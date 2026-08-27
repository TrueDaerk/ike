package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/project"
)

// switchFixture builds two project directories with one file in the first,
// chdirs there and returns the roots plus the file path.
func switchFixture(t *testing.T) (src, dst, file string) {
	t.Helper()
	base := t.TempDir()
	src, dst = filepath.Join(base, "src"), filepath.Join(base, "dst")
	for _, d := range []string{src, dst} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	file = filepath.Join(src, "a.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(src)
	return src, dst, file
}

// TestAutoSaveOnSwitchWritesDirtyBuffer (#2186): with the setting on, an
// orderly switch writes the departing project's dirty buffers and proceeds
// without any prompt.
func TestAutoSaveOnSwitchWritesDirtyBuffer(t *testing.T) {
	_, dst, file := switchFixture(t)
	m := autoSaveSwitchModel(t)
	m = openDirty(t, m, file)

	out, cmd := m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("the switch must return its follow-up batch")
	}
	if m.switchBlockedPromptOpen() || m.switchPromptOpen() {
		t.Fatal("a writable dirty buffer must not prompt")
	}
	if !sameDir(t, cwd(t), dst) {
		t.Fatalf("the switch must proceed, cwd = %s", cwd(t))
	}
	if data, _ := os.ReadFile(file); string(data) == "one\n" {
		t.Fatalf("the switch must write the dirty buffer, file = %q", data)
	}
}

// TestAutoSaveOnSwitchOffParksDirty (#2186): with the setting off the switch
// behaves as before — the buffer parks unsaved with its workspace.
func TestAutoSaveOnSwitchOffParksDirty(t *testing.T) {
	_, dst, file := switchFixture(t)
	m := switchModel(t) // auto_save_on_switch = false
	m = openDirty(t, m, file)

	out, _ := m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	if !sameDir(t, cwd(t), dst) {
		t.Fatalf("the switch must proceed, cwd = %s", cwd(t))
	}
	if data, _ := os.ReadFile(file); string(data) != "one\n" {
		t.Fatalf("with the gate off nothing may be written, file = %q", data)
	}
}

// TestAutoSaveOnSwitchAggregatesUnsaveable (#2186): a buffer that cannot be
// written — here an untitled one — holds the switch in one aggregated dialog
// naming it and its reason, while the writable buffer is already saved.
func TestAutoSaveOnSwitchAggregatesUnsaveable(t *testing.T) {
	src, dst, _ := switchFixture(t)
	m := autoSaveSwitchModel(t)
	m = dirtyUntitled(t, m)

	out, _ := m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	if !m.switchBlockedPromptOpen() {
		t.Fatal("an unsaveable buffer must open the aggregated dialog")
	}
	body := guardBody(m)
	for _, want := range []string{"untitled", "no file path", "[s/enter]", "[d]", "[esc]"} {
		if !strings.Contains(body, want) {
			t.Errorf("dialog body must mention %q, got %q", want, body)
		}
	}
	if !sameDir(t, cwd(t), src) {
		t.Fatalf("the switch must wait for the answer, cwd = %s", cwd(t))
	}
}

// TestAutoSaveSwitchCancelAborts (#2186): esc on the dialog cancels the switch
// and loses nothing — the project stays, the buffer stays dirty.
func TestAutoSaveSwitchCancelAborts(t *testing.T) {
	src, dst, _ := switchFixture(t)
	m := autoSaveSwitchModel(t)
	m = dirtyUntitled(t, m)
	key := m.activeEditorKey()

	out, _ := m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)

	if m.switchBlockedPromptOpen() {
		t.Fatal("esc must close the dialog")
	}
	if !sameDir(t, cwd(t), src) {
		t.Fatalf("esc must abort the switch, cwd = %s", cwd(t))
	}
	if ed := m.activeWS().Panes.Get(key).Editor(); ed == nil || !ed.Dirty() {
		t.Fatal("the cancelled switch must leave the buffer untouched")
	}
}

// TestAutoSaveSwitchDiscardProceeds (#2186): d switches anyway; the unsaveable
// buffer parks with its workspace, still dirty and still unwritten.
func TestAutoSaveSwitchDiscardProceeds(t *testing.T) {
	_, dst, _ := switchFixture(t)
	m := autoSaveSwitchModel(t)
	m = dirtyUntitled(t, m)

	out, _ := m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	out, _ = m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	m = out.(Model)

	if m.switchBlockedPromptOpen() {
		t.Fatal("d must close the dialog")
	}
	if !sameDir(t, cwd(t), dst) {
		t.Fatalf("d must perform the switch, cwd = %s", cwd(t))
	}
}

// TestAutoSaveSwitchSaveAsThenSwitches (#2186): s names the untitled buffer
// through the save-as prompt; the accepted name removes the last blocker, so
// the switch runs right after.
func TestAutoSaveSwitchSaveAsThenSwitches(t *testing.T) {
	src, dst, _ := switchFixture(t)
	m := autoSaveSwitchModel(t)
	m = dirtyUntitled(t, m)

	out, _ := m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	out, cmd := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	m = drainCmd(out.(Model), cmd)
	if !m.saveAsOpen() {
		t.Fatal("s must open the save-as prompt for the untitled buffer")
	}

	m = typeInto(m, "named.txt")
	out, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drainCmd(out.(Model), cmd)

	if data, err := os.ReadFile(filepath.Join(src, "named.txt")); err != nil {
		t.Fatalf("the named buffer must be written: %v", err)
	} else if !strings.HasPrefix(string(data), "hello") {
		t.Fatalf("file content = %q", data)
	}
	if !sameDir(t, cwd(t), dst) {
		t.Fatalf("naming the last blocker must resume the switch, cwd = %s", cwd(t))
	}
}

// TestAutoSaveSwitchSaveAsCancelReopensDialog (#2186): aborting the save-as
// prompt the dialog opened brings the dialog back — the switch never
// dead-ends on a cancelled sub-prompt.
func TestAutoSaveSwitchSaveAsCancelReopensDialog(t *testing.T) {
	src, dst, _ := switchFixture(t)
	m := autoSaveSwitchModel(t)
	m = dirtyUntitled(t, m)

	out, _ := m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	out, cmd := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	m = drainCmd(out.(Model), cmd)
	out, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = drainCmd(out.(Model), cmd)

	if !m.switchBlockedPromptOpen() {
		t.Fatal("a cancelled save-as must bring the dialog back")
	}
	if !sameDir(t, cwd(t), src) {
		t.Fatalf("nothing may have switched yet, cwd = %s", cwd(t))
	}
}

// TestAutoSaveSwitchStaleBufferIsNotClobbered (#2186): a buffer whose file
// changed on disk since the edits is never written by the switch — that
// decision belongs to the conflict guard — so it lands in the dialog.
func TestAutoSaveSwitchStaleBufferIsNotClobbered(t *testing.T) {
	_, dst, file := switchFixture(t)
	m := autoSaveSwitchModel(t)
	m = openDirty(t, m, file)
	if err := os.WriteFile(file, []byte("external\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The reconcile pass is what marks a dirty buffer stale when its file
	// provably changed (#1515) — the same state an external edit leaves.
	m.reconcileEditors()
	for _, ed := range m.editorViewsForPath(file) {
		if !ed.Stale() {
			t.Fatal("fixture: the buffer must be stale")
		}
	}

	out, _ := m.Update(project.SwitchProjectMsg{Root: dst})
	m = out.(Model)
	if !m.switchBlockedPromptOpen() {
		t.Fatal("a stale buffer must reach the dialog instead of being written")
	}
	if body := guardBody(m); !strings.Contains(body, "changed on disk") {
		t.Errorf("dialog must name the reason, got %q", body)
	}
	if data, _ := os.ReadFile(file); string(data) != "external\n" {
		t.Fatalf("the external change must survive, file = %q", data)
	}
}
