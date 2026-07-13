package editor

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestDependencyDirClassifier(t *testing.T) {
	deps := []string{
		"/home/u/proj/.venv/lib/python3.12/site-packages/fastapi/__init__.py",
		"/home/u/proj/node_modules/react/index.js",
		"proj/vendor/pkg/file.go",
		"/x/site-packages/y.py",
	}
	for _, p := range deps {
		if !dependencyDir(p) {
			t.Errorf("dependencyDir(%q) = false, want true", p)
		}
	}
	notDeps := []string{"/home/u/proj/main.py", "src/app/index.js", "venvironment/x.py", ""}
	for _, p := range notDeps {
		if dependencyDir(p) {
			t.Errorf("dependencyDir(%q) = true, want false", p)
		}
	}
}

// loadedDep writes content into a file under a .venv directory and loads it.
func loadedDep(t *testing.T, content string) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	sub := filepath.Join(dir, ".venv", "site-packages", "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "mod.py")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(80, 20)
	m.SetFocused(true)
	return m, path
}

// updateGetMsg runs one key and resolves the returned Cmd to its message.
func updateGetMsg(m Model, k tea.KeyPressMsg) (Model, tea.Msg) {
	m, cmd := m.Update(k)
	if cmd == nil {
		return m, nil
	}
	return m, cmd()
}

func blockedByFirstEdit(t *testing.T, k tea.KeyPressMsg) {
	t.Helper()
	m, _ := loadedDep(t, "import os\nx = 1\n")
	before := m.buf.String()
	m, msg := updateGetMsg(m, k)
	if m.buf.String() != before {
		t.Fatalf("edit %v mutated a locked dependency file: %q", k, m.buf.String())
	}
	if _, ok := msg.(DepEditBlockedMsg); !ok {
		// The signal may be batched; fall back to the flag-independent check.
		if !m.blockDep() {
			t.Fatalf("edit %v did not block (msg=%T)", k, msg)
		}
	}
}

func TestDepEditBlocksEachEditClass(t *testing.T) {
	blockedByFirstEdit(t, key('x')) // delete char
	blockedByFirstEdit(t, key('i')) // insert entry
	blockedByFirstEdit(t, key('o')) // open line
	blockedByFirstEdit(t, key('p')) // paste
	blockedByFirstEdit(t, key('J')) // join
	blockedByFirstEdit(t, key('~')) // toggle case
}

func TestDepEditTypingBlockedUntilConfirmed(t *testing.T) {
	m, _ := loadedDep(t, "x = 1\n")
	before := m.buf.String()
	// Enter insert (blocked) then attempt to type — nothing lands.
	m, msg := updateGetMsg(m, key('i'))
	if _, ok := msg.(DepEditBlockedMsg); !ok {
		t.Fatalf("i did not emit DepEditBlockedMsg, got %T", msg)
	}
	m = send(m, keys("abc")...)
	if m.buf.String() != before {
		t.Fatalf("typing landed on a locked file: %q", m.buf.String())
	}
}

func TestDepEditConfirmReplaysEdit(t *testing.T) {
	m, _ := loadedDep(t, "abc\n")
	// x on 'a' is blocked...
	m, _ = updateGetMsg(m, key('x'))
	if m.buf.Line(0) != "abc" {
		t.Fatalf("x should be blocked, got %q", m.buf.Line(0))
	}
	// ...confirm replays it.
	m, _ = m.Update(ConfirmDepEditMsg{})
	if m.buf.Line(0) != "bc" {
		t.Fatalf("confirm did not replay x: %q", m.buf.Line(0))
	}
	if m.blockDep() {
		t.Fatal("buffer still locked after confirm")
	}
	// A second edit is no longer prompted.
	m, msg := updateGetMsg(m, key('x'))
	if _, ok := msg.(DepEditBlockedMsg); ok {
		t.Fatal("second edit re-prompted after confirm")
	}
	if m.buf.Line(0) != "c" {
		t.Fatalf("second x not applied: %q", m.buf.Line(0))
	}
}

func TestDepEditCancelLeavesFileUnchanged(t *testing.T) {
	m, _ := loadedDep(t, "abc\n")
	m, _ = updateGetMsg(m, key('x'))
	m.CancelDepEdit()
	if m.buf.Line(0) != "abc" {
		t.Fatalf("cancel changed the file: %q", m.buf.Line(0))
	}
	if !m.blockDep() {
		t.Fatal("buffer should stay locked after cancel")
	}
}

func TestNonDependencyFileEditsFreely(t *testing.T) {
	m, _ := loaded(t, "abc\n")
	if m.IsDependencyFile() {
		t.Fatal("plain file wrongly classified as dependency")
	}
	m, msg := updateGetMsg(m, key('x'))
	if _, ok := msg.(DepEditBlockedMsg); ok {
		t.Fatal("plain file edit was blocked")
	}
	if m.buf.Line(0) != "bc" {
		t.Fatalf("plain file edit not applied: %q", m.buf.Line(0))
	}
}
