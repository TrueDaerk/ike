package app

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/pane"
)

func tmpFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestFileOpenSkipsTerminalTabHost guards #998: with a terminal-only tab
// host focused (a cmd+t-converted terminal pane, #983), opening a file must
// not add a file tab there — it lands in a real editor pane and focuses it,
// the terminal host stays untouched.
func TestFileOpenSkipsTerminalTabHost(t *testing.T) {
	m, key := openTestTerminal(t)
	handled, out, _ := m.terminalReservedKey("cmd+t")
	if !handled {
		t.Fatal("setup: cmd+t must convert the terminal pane")
	}
	m = out.(Model)
	inst := m.activeWS().Panes.Get(key)
	t.Cleanup(inst.CloseTerminalTabs)
	tabs := inst.TabCount()

	path := tmpFile(t, "a.txt", "hello\n")
	out2, _ := m.openPath(path, false)
	m = out2.(Model)

	fkey := m.activeWS().Panes.Focused()
	if fkey == key {
		t.Fatal("file must not open in the terminal tab host")
	}
	finst := m.activeWS().Panes.Get(fkey)
	if finst == nil || finst.Kind() != pane.KindEditor || finst.Editor() == nil || finst.Editor().Path() != path {
		t.Fatalf("file must land in an editor pane, focused %q", fkey)
	}
	if inst.TabCount() != tabs || len(inst.Editors()) != 0 {
		t.Fatal("the terminal host's tabs must stay terminal-only")
	}
}

// TestOpenPathOutsideWorkspace guards #999: an absolute path outside the
// project root opens as a fully functional editor tab; a missing path fails
// with an error toast instead of silently doing nothing.
func TestOpenPathOutsideWorkspace(t *testing.T) {
	m := sized(t, 100, 40)
	outside := tmpFile(t, "outside.txt", "external\n")

	out, _ := m.openPath(outside, false)
	m = out.(Model)
	inst := m.activeWS().Panes.Get(m.activeWS().Panes.Focused())
	if inst == nil || inst.Editor() == nil || inst.Editor().Path() != outside {
		t.Fatal("outside-root file must open as a regular editor tab")
	}

	missing := filepath.Join(t.TempDir(), "missing.txt")
	out, _ = m.openPath(missing, false)
	m = out.(Model)
	if inst2 := m.activeWS().Panes.Get(m.activeWS().Panes.Focused()); inst2 != nil && inst2.Editor() != nil && inst2.Editor().Path() == missing {
		t.Fatal("missing path must not produce a tab")
	}
}

// TestFileOpenSkipsDedicatedTerminal: with a dedicated terminal pane focused,
// a file open lands in an editor pane and the terminal pane survives as-is.
func TestFileOpenSkipsDedicatedTerminal(t *testing.T) {
	m, key := openTestTerminal(t)
	path := tmpFile(t, "b.txt", "world\n")
	out, _ := m.openPath(path, false)
	m = out.(Model)

	fkey := m.activeWS().Panes.Focused()
	finst := m.activeWS().Panes.Get(fkey)
	if fkey == key || finst == nil || finst.Kind() != pane.KindEditor {
		t.Fatalf("file must land in an editor pane, focused %q", fkey)
	}
	if tp := m.activeWS().Panes.Get(key); tp == nil || tp.Kind() != pane.KindTerminal {
		t.Fatal("the terminal pane must survive unchanged")
	}
}
