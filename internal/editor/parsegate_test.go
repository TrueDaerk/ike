package editor

import (
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/highlight"
	"ike/internal/host"
)

// The parse gate (#2770): a keystroke burst's commands parse the newest
// snapshot once, the superseded ones yield nothing, and the gate is open
// again as soon as the worker retires.

func gateEditor(t *testing.T) Model {
	t.Helper()
	m := New()
	m.Configure(host.MapConfig{})
	m.RestoreText(strings.Repeat("<p>hi</p>\n", 20))
	m.path = "/tmp/gate.html"
	m.SetSize(80, 20)
	m.SetFocused(true)
	return send(m, key('i'))
}

func TestParseGateBurstParsesNewestSnapshotOnce(t *testing.T) {
	m := gateEditor(t)
	var cmds []tea.Cmd
	for i := 0; i < 6; i++ {
		var cmd tea.Cmd
		m, cmd = m.Update(key('x'))
		if cmd == nil {
			t.Fatalf("keystroke %d scheduled nothing", i+1)
		}
		cmds = append(cmds, cmd)
	}
	// The first command to run becomes the worker and parses the newest
	// snapshot — the document as it stands after the whole burst.
	msg, ok := runCmdMsg(cmds[0]).(highlight.SpansMsg)
	if !ok {
		t.Fatal("the worker produced no SpansMsg")
	}
	if msg.Version != m.docVersion {
		t.Fatalf("worker parsed version %d, document is at %d: the newest snapshot must win", msg.Version, m.docVersion)
	}
	if m.parseGate.pending() {
		t.Fatal("a snapshot is still pending after the worker retired")
	}
	// The superseded commands have nothing left to do.
	for i, cmd := range cmds[1:] {
		if got := runCmdMsg(cmd); got != nil {
			t.Fatalf("superseded command %d yielded %T, want nil", i+2, got)
		}
	}
	m, _ = m.Update(msg)
	if m.SyntaxCapture(1, 1) == "" {
		t.Fatal("the result must be applied")
	}
	// The gate is open: the next edit's command parses on its own.
	var next tea.Cmd
	m, next = m.Update(key('y'))
	fresh, ok := runCmdMsg(next).(highlight.SpansMsg)
	if !ok || fresh.Version != m.docVersion {
		t.Fatalf("after the worker retired the next command must parse the current version, got %#v", fresh)
	}
}

// Commands that run concurrently still parse once: exactly one becomes the
// worker, the others yield nil.
func TestParseGateConcurrentCommandsParseOnce(t *testing.T) {
	m := gateEditor(t)
	var cmds []tea.Cmd
	for i := 0; i < 8; i++ {
		var cmd tea.Cmd
		m, cmd = m.Update(key('x'))
		cmds = append(cmds, cmd)
	}
	results := make([]tea.Msg, len(cmds))
	var wg sync.WaitGroup
	for i, cmd := range cmds {
		wg.Add(1)
		go func(i int, cmd tea.Cmd) {
			defer wg.Done()
			results[i] = runCmdMsg(cmd)
		}(i, cmd)
	}
	wg.Wait()
	got := 0
	for _, r := range results {
		if sp, ok := r.(highlight.SpansMsg); ok {
			got++
			if sp.Version != m.docVersion {
				t.Fatalf("the worker parsed version %d, document is at %d", sp.Version, m.docVersion)
			}
		}
	}
	if got != 1 {
		t.Fatalf("%d commands parsed, want exactly 1", got)
	}
}

// A view without a gate (the zero Model) still parses: schedule degrades to
// the plain command.
func TestParseGateNilParses(t *testing.T) {
	var g *parseGate
	cmd := g.schedule(parseSnapshot{key: "k", version: 3}, func(s parseSnapshot) tea.Msg {
		return highlight.SpansMsg{Path: s.key, Version: s.version}
	})
	if sp, ok := cmd().(highlight.SpansMsg); !ok || sp.Version != 3 {
		t.Fatalf("nil gate command yielded %#v", sp)
	}
}
