package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/largefile"
	"ike/internal/registry"
)

// asyncFixture writes a plain-text file just above the editor's async
// threshold (4 MB, #2996).
func asyncFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "big.log")
	line := strings.Repeat("x", 99) + "\n"
	if err := os.WriteFile(path, []byte(strings.Repeat(line, (4<<20)/100+100)), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestAsyncOpenShowsLoadingBadgeThenLands guards the app side of #2996: a
// big file opens as a tab at once, the status line's large-file slot says
// `loading…`, the open hands back the read command, and routing its LoadMsg
// fills the buffer and clears the badge.
func TestAsyncOpenShowsLoadingBadgeThenLands(t *testing.T) {
	largefile.Reset()
	t.Cleanup(largefile.Reset)
	m := sizedWith(t, registry.New(), 200, 40)
	path := asyncFixture(t)
	out, cmd := m.openPath(path, false)
	m = out.(Model)
	ed := m.activeEditor()
	if ed == nil || ed.Path() != path {
		t.Fatal("the tab must open under the path at once")
	}
	if !ed.Loading() {
		t.Fatal("a file above the threshold must be loading")
	}
	if frame := m.render(); !strings.Contains(frame, "loading…") {
		t.Fatal("the status line must show the pending load in the large-file slot")
	}
	if cmd == nil {
		t.Fatal("the open must hand back the read command")
	}
	msg := findLoadMsg(t, cmd)
	if msg.Err != nil {
		t.Fatalf("the read must succeed: %v", msg.Err)
	}
	// A key during the load is handled: the explorer toggle moves focus.
	m = step(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = step(m, msg)
	ed = m.editorForPath(path)
	if ed == nil || ed.Loading() {
		t.Fatal("the routed landing must settle the load")
	}
	if ed.LineCount() < (4<<20)/100 {
		t.Fatalf("the landed buffer must hold the file, got %d lines", ed.LineCount())
	}
	if frame := m.render(); strings.Contains(frame, "loading…") {
		t.Fatal("the badge must clear once the landing arrived")
	}
}

// TestAsyncOpenCloseCancelsLoad: closing the tab before the read lands
// cancels it — the command yields nil, and nothing is installed anywhere.
func TestAsyncOpenCloseCancelsLoad(t *testing.T) {
	largefile.Reset()
	t.Cleanup(largefile.Reset)
	m := sizedWith(t, registry.New(), 200, 40)
	small := filepath.Join(t.TempDir(), "small.txt")
	if err := os.WriteFile(small, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.openPath(small, false)
	m = out.(Model)
	path := asyncFixture(t)
	out, cmd := m.openPath(path, false)
	m = out.(Model)
	key := m.activeEditorKey()
	inst := m.activeWS().Panes.Get(key)
	if !inst.CloseTab(inst.ActiveTab()) {
		t.Fatal("setup: the big file's tab must close")
	}
	if msg := asyncLoadRunCmds(cmd); msg != nil {
		if lm, ok := msg.(editor.LoadMsg); ok {
			t.Fatalf("a cancelled read must yield nil, got %#v", lm.Path)
		}
	}
}

// findLoadMsg runs cmd (or a batch's commands) until one yields a LoadMsg.
func findLoadMsg(t *testing.T, cmd tea.Cmd) editor.LoadMsg {
	t.Helper()
	if msg, ok := asyncLoadRunCmds(cmd).(editor.LoadMsg); ok {
		return msg
	}
	t.Fatal("no LoadMsg among the open's commands")
	return editor.LoadMsg{}
}

// runCmds runs cmd, descending into batches, and returns the first LoadMsg
// or nil.
func asyncLoadRunCmds(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case editor.LoadMsg:
		return msg
	case tea.BatchMsg:
		for _, c := range msg {
			if r := asyncLoadRunCmds(c); r != nil {
				return r
			}
		}
	}
	return nil
}
