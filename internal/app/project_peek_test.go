package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/project"
)

// project_peek_test.go covers the quick-peek switch (#2136): enter, one-key
// return with unload, keep-escalation, the busy guard, and history hygiene.

// peekFixture: two (or three) sibling project dirs, cwd in the first.
func peekFixture(t *testing.T, names ...string) []string {
	t.Helper()
	base := t.TempDir()
	var roots []string
	for _, n := range names {
		d := filepath.Join(base, n)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		roots = append(roots, d)
	}
	t.Chdir(roots[0])
	return roots
}

// runPeekCmds executes the command tree with a per-leaf timeout (the switch batch
// carries sleepers — idle timers, blink ticks — that must not stall the test)
// and returns every produced message without feeding them back into Update.
func runPeekCmds(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	var out []tea.Msg
	pending := []tea.Cmd{cmd}
	for len(pending) > 0 {
		c := pending[0]
		pending = pending[1:]
		if c == nil {
			continue
		}
		ch := make(chan tea.Msg, 1)
		go func(c tea.Cmd) { ch <- c() }(c)
		var msg tea.Msg
		select {
		case msg = <-ch:
		case <-time.After(500 * time.Millisecond):
			continue // a sleeper: not peek material
		}
		if b, ok := msg.(tea.BatchMsg); ok {
			pending = append(pending, b...)
			continue
		}
		if msg != nil {
			out = append(out, msg)
		}
	}
	return out
}

// recordedRoots filters the RecordedMsg roots out of a message list.
func recordedRoots(msgs []tea.Msg) []string {
	var roots []string
	for _, msg := range msgs {
		if r, ok := msg.(project.RecordedMsg); ok {
			roots = append(roots, r.Root)
		}
	}
	return roots
}

// historyPaths reads the persisted recent-projects list, most recent first.
func historyPaths() []string {
	cfg, _ := config.Load(config.Discover("."))
	var out []string
	for _, e := range project.History(cfg) {
		out = append(out, e.Path)
	}
	return out
}

// enterPeek performs the peek switch into root and runs the follow-up batch.
func enterPeek(t *testing.T, m Model, root string) (Model, []tea.Msg) {
	t.Helper()
	out, cmd := m.Update(project.PeekProjectMsg{Root: root})
	return out.(Model), runPeekCmds(t, cmd)
}

// TestPeekEnterMarksAndSkipsHistory (#2136): the peek switch re-roots like a
// normal switch but marks the model, shows the indicator, and records nothing.
func TestPeekEnterMarksAndSkipsHistory(t *testing.T) {
	roots := peekFixture(t, "origin", "peeked")
	m := switchModel(t)

	m, msgs := enterPeek(t, m, roots[1])
	if !sameDir(t, cwd(t), roots[1]) {
		t.Fatalf("peek must re-root, cwd = %s", cwd(t))
	}
	if m.peek == nil || !sameDir(t, m.peek.origin, roots[0]) {
		t.Fatalf("peek marker wrong: %+v", m.peek)
	}
	if got := recordedRoots(msgs); len(got) != 0 {
		t.Errorf("peek-enter must not record history, recorded %v", got)
	}
	if seg := peekSegment(m, nil); !strings.Contains(seg, "origin") {
		t.Errorf("indicator should name the origin, got %q", seg)
	}
}

// TestPeekReturnRestoresAndUnloads (#2136): one action back — the origin
// resumes, the peeked workspace is dropped, and an untouched peek leaves no
// .ike residue in the peeked project.
func TestPeekReturnRestoresAndUnloads(t *testing.T) {
	roots := peekFixture(t, "origin", "peeked")
	m := switchModel(t)
	m, _ = enterPeek(t, m, roots[1])

	out, cmd := m.Update(project.PeekReturnMsg{})
	m = out.(Model)
	if !sameDir(t, cwd(t), roots[0]) {
		t.Fatalf("return must restore the origin, cwd = %s", cwd(t))
	}
	if m.peek != nil {
		t.Error("peek marker must clear on return")
	}
	if m.ws.Peek(roots[1]) != nil || len(m.ws.Background()) != 0 {
		t.Errorf("peeked workspace must be dropped, background = %v", m.ws.Background())
	}
	runPeekCmds(t, cmd)
	if _, err := os.Stat(filepath.Join(roots[1], ".ike")); !os.IsNotExist(err) {
		t.Errorf("an untouched peek must not plant .ike in the peeked project: %v", err)
	}
}

// TestPeekRoundTripKeepsHistory (#2136): after enter + return, the persisted
// project.history — and with it the startup restore head — is unchanged: the
// origin stays in front, the peeked project never appears.
func TestPeekRoundTripKeepsHistory(t *testing.T) {
	roots := peekFixture(t, "origin", "peeked")
	m := switchModel(t)
	// A private user layer (set after switchModel, which resets the env):
	// other tests' leaked record goroutines must not write into this test's
	// history between the two snapshots.
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	if err := project.RecordOpen(config.Discover("."), roots[0], time.Now()); err != nil {
		t.Fatal(err)
	}
	before := historyPaths()

	m, _ = enterPeek(t, m, roots[1])
	out, cmd := m.Update(project.PeekReturnMsg{})
	m = out.(Model)
	runPeekCmds(t, cmd)

	after := historyPaths()
	if len(after) != len(before) {
		t.Fatalf("history changed: before %v, after %v", before, after)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("history order changed: before %v, after %v", before, after)
		}
	}
	if want, _ := project.Validate(roots[0]); after[0] != want {
		t.Errorf("restore head must stay the origin, got %q", after[0])
	}
}

// TestPeekKeepConverts (#2136): project.peek.keep clears the marker and
// records the peeked project as a normal open — it becomes the restore head.
func TestPeekKeepConverts(t *testing.T) {
	roots := peekFixture(t, "origin", "peeked")
	m := switchModel(t)
	t.Setenv("IKE_CONFIG_DIR", t.TempDir()) // private user layer, see above
	m, _ = enterPeek(t, m, roots[1])

	out, cmd := m.Update(project.PeekKeepMsg{})
	m = out.(Model)
	if m.peek != nil {
		t.Error("keep must clear the peek marker")
	}
	if got := recordedRoots(runPeekCmds(t, cmd)); len(got) != 1 || !sameDir(t, got[0], roots[1]) {
		t.Fatalf("keep must record the peeked root, recorded %v", got)
	}
	if head := historyPaths()[0]; !sameDir(t, head, roots[1]) {
		t.Errorf("kept project should lead the history, got %q", head)
	}
}

// TestPeekEscalatesOnNormalSwitch (#2136): a plain switch elsewhere from
// within a peek converts it — the peeked root is recorded (before the target)
// and the marker clears.
func TestPeekEscalatesOnNormalSwitch(t *testing.T) {
	roots := peekFixture(t, "origin", "peeked", "third")
	m := switchModel(t)
	t.Setenv("IKE_CONFIG_DIR", t.TempDir()) // private user layer, see above
	m, _ = enterPeek(t, m, roots[1])

	out, cmd := m.Update(project.SwitchProjectMsg{Root: roots[2]})
	m = out.(Model)
	if m.peek != nil {
		t.Error("a normal switch away must clear the peek marker")
	}
	runPeekCmds(t, cmd)
	paths := historyPaths()
	if len(paths) < 2 {
		t.Fatalf("escalation must record peeked root and target, got %v", paths)
	}
	if !sameDir(t, paths[0], roots[2]) || !sameDir(t, paths[1], roots[1]) {
		t.Errorf("want [third, peeked, ...], got %v", paths)
	}
}

// TestPeekReturnBusyGuard (#2136): a dirty buffer in the peek gates the
// return behind the existing guard — esc keeps the peek, d discards and
// returns.
func TestPeekReturnBusyGuard(t *testing.T) {
	roots := peekFixture(t, "origin", "peeked")
	file := filepath.Join(roots[1], "f.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := switchModel(t)
	m, _ = enterPeek(t, m, roots[1])
	m = openDirty(t, m, file)

	out, _ := m.Update(project.PeekReturnMsg{})
	m = out.(Model)
	if !m.peekReturnPromptOpen() {
		t.Fatal("a dirty peek must open the return guard")
	}
	// esc: stay in the peek, workspace untouched.
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.peekReturnPromptOpen() || m.peek == nil || !sameDir(t, cwd(t), roots[1]) {
		t.Fatal("esc must keep the peek untouched")
	}
	// d: discard and return.
	out, _ = m.Update(project.PeekReturnMsg{})
	m = out.(Model)
	out, _ = m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	m = out.(Model)
	if !sameDir(t, cwd(t), roots[0]) {
		t.Fatalf("d must return to the origin, cwd = %s", cwd(t))
	}
	if m.peek != nil || m.ws.Peek(roots[1]) != nil {
		t.Error("d must drop the peeked workspace and clear the marker")
	}
}

// TestPeekReturnWithoutPeek (#2136): project.peek.return outside a peek is a
// friendly no-op.
func TestPeekReturnWithoutPeek(t *testing.T) {
	peekFixture(t, "only")
	m := switchModel(t)
	out, _ := m.Update(project.PeekReturnMsg{})
	m = out.(Model)
	if m.peek != nil || m.peekReturnPromptOpen() {
		t.Error("return without a peek must be a no-op")
	}
}
