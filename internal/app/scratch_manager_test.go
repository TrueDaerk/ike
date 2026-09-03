package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/palette"
	"ike/internal/scratch"
)

// scratch_manager_test.go covers the scratch manager (#2256): the listing with
// its metadata, opening, the validated rename (including a scratch open in an
// editor, which must follow the new name), the confirmed delete and the
// language change.

// smKey feeds one key press through the manager's update path.
func smKey(m Model, k tea.Key) Model { return drainKey(m, tea.KeyPressMsg(k)) }

// smType feeds one printable key with its text, so the type-ahead sees it.
func smType(m Model, r rune) Model {
	return drainKey(m, tea.KeyPressMsg{Code: r, Text: string(r)})
}

// smTypeAll types a whole string into the open step.
func smTypeAll(m Model, s string) Model {
	for _, r := range s {
		m = smType(m, r)
	}
	return m
}

// openManager opens the manager through the real command path.
func openManager(t *testing.T, m Model) Model {
	t.Helper()
	m = drainCmd(m, m.RunCommand(scratchManageCommandID))
	if !m.scratchManagerOpen() {
		t.Fatal("scratch.manage must open the manager")
	}
	return m
}

// smSeed creates the scratches with the given extensions in the model's
// store, newest last, and returns their paths.
func smSeed(t *testing.T, exts ...string) []string {
	t.Helper()
	var out []string
	for _, ext := range exts {
		path, err := scratch.Create(ext)
		if err != nil {
			t.Fatalf("seeding scratch: %v", err)
		}
		out = append(out, path)
	}
	return out
}

// smSelect moves the cursor onto the row naming base.
func smSelect(t *testing.T, m Model, base string) Model {
	t.Helper()
	for i, e := range m.scratchMgr.visible() {
		if e.name == base {
			m.scratchMgr.pick = i
			return m
		}
	}
	t.Fatalf("no row for %s in %v", base, m.scratchMgr.visible())
	return m
}

// smClick presses the left button at the hit region the last render recorded
// for (kind, arg) — the coordinates a real click there carries.
func smClick(t *testing.T, m Model, kind smHitKind, arg int) Model {
	t.Helper()
	v := m.shell.View()
	bx, by := (m.width-lipgloss.Width(v))/2, (m.height-lipgloss.Height(v))/2
	ox, oy := m.shell.ContentOrigin()
	for _, h := range m.scratchMgr.hits {
		if h.kind != kind || h.arg != arg {
			continue
		}
		x, y := bx+ox+h.x0, by+oy+h.y-m.shell.ScrollOffset()
		out, cmd := m.handleMouse(mouseEvent{Mouse: tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}, action: mousePress})
		return drainCmd(out.(Model), cmd)
	}
	t.Fatalf("no hit region for kind %d arg %d", kind, arg)
	return m
}

func TestScratchManagerCommandRegistered(t *testing.T) {
	m := newSized()
	if _, ok := m.reg.Command(scratchManageCommandID); !ok {
		t.Fatal("scratch.manage must be registered")
	}
}

// TestScratchManagerListsMetadata is the listing criterion: every scratch with
// its name, language, size and age.
func TestScratchManagerListsMetadata(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	paths := smSeed(t, "txt", "sct")
	if err := os.WriteFile(paths[1], []byte("hello scratch"), 0o644); err != nil {
		t.Fatalf("writing scratch: %v", err)
	}
	m = openManager(t, m)

	if got := len(m.scratchMgr.entries); got != 2 {
		t.Fatalf("entries = %d, want 2", got)
	}
	view := plainView(m)
	for _, want := range []string{"scratch-1.txt", "scratch-1.sct", "Plain Text", "Sctest", "13 B", "Language", "Modified"} {
		if !strings.Contains(view, want) {
			t.Fatalf("manager view misses %q:\n%s", want, view)
		}
	}
}

// TestScratchManagerSpeedSearchNarrows covers the speed search: typing filters
// the rows and esc clears the query before it closes the dialog.
func TestScratchManagerSpeedSearchNarrows(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	smSeed(t, "txt", "sct")
	m = openManager(t, m)

	m = smTypeAll(m, "sct")
	if got := len(m.scratchMgr.visible()); got != 1 {
		t.Fatalf("filtered rows = %d, want only the .sct scratch", got)
	}
	m = smKey(m, tea.Key{Code: tea.KeyEscape})
	if !m.scratchManagerOpen() {
		t.Fatal("the first esc must clear the query, not close the manager")
	}
	if got := len(m.scratchMgr.visible()); got != 2 {
		t.Fatalf("rows after clearing = %d, want 2", got)
	}
	m = smKey(m, tea.Key{Code: tea.KeyEscape})
	if m.scratchManagerOpen() {
		t.Fatal("the second esc must close the manager")
	}
}

// TestScratchManagerOpensSelection covers enter: the scratch lands as the
// focused buffer and the manager closes.
func TestScratchManagerOpensSelection(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	paths := smSeed(t, "txt")
	m = openManager(t, m)

	m = smKey(m, tea.Key{Code: tea.KeyEnter})
	if m.scratchManagerOpen() {
		t.Fatal("opening a scratch must close the manager")
	}
	if got := m.activeWS().Panes.FocusedInstance().Editor().Path(); got != paths[0] {
		t.Fatalf("focused buffer = %q, want %q", got, paths[0])
	}
}

// TestScratchManagerRenameFollowsOpenBuffer is the open-buffer criterion: a
// scratch renamed while it is open in an editor keeps the buffer, which
// re-points at the new path (so the tab title follows it).
func TestScratchManagerRenameFollowsOpenBuffer(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := dispatch(t, newSized(), NewScratchMsg{Ext: "sct"})
	old := m.activeWS().Panes.FocusedInstance().Editor().Path()

	m = openManager(t, m)
	m = smSelect(t, m, filepath.Base(old))
	m = smKey(m, tea.Key{Code: 'r', Mod: tea.ModCtrl})
	if m.scratchMgr.step != smStepRename {
		t.Fatalf("step = %d, want the rename step", m.scratchMgr.step)
	}
	if m.scratchMgr.renameInput.Text != filepath.Base(old) {
		t.Fatalf("rename prefill = %q, want the current name", m.scratchMgr.renameInput.Text)
	}
	// Replace the prefilled name with notes.sct.
	for range m.scratchMgr.renameInput.Text {
		m = smKey(m, tea.Key{Code: tea.KeyBackspace})
	}
	m = smTypeAll(m, "notes.sct")
	m = smKey(m, tea.Key{Code: tea.KeyEnter})

	want := filepath.Join(filepath.Dir(old), "notes.sct")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("renamed scratch must exist: %v", err)
	}
	if _, err := os.Stat(old); err == nil {
		t.Fatal("the old scratch name must be gone")
	}
	ed := m.activeWS().Panes.FocusedInstance().Editor()
	if ed.Path() != want {
		t.Fatalf("open buffer path = %q, want %q", ed.Path(), want)
	}
	if m.scratchMgr.step != smStepList {
		t.Fatalf("step = %d, want back on the list", m.scratchMgr.step)
	}
	if got := m.scratchMgr.entries[m.scratchMgr.pick].name; got != "notes.sct" {
		t.Fatalf("selection = %q, want the renamed scratch", got)
	}
}

// TestScratchManagerRenameCollisionRejected is the collision criterion: the
// rename is refused with a message and nothing on disk moves.
func TestScratchManagerRenameCollisionRejected(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	paths := smSeed(t, "txt", "sct")
	m = openManager(t, m)
	m = smSelect(t, m, "scratch-1.sct")
	m = smKey(m, tea.Key{Code: 'r', Mod: tea.ModCtrl})
	for range m.scratchMgr.renameInput.Text {
		m = smKey(m, tea.Key{Code: tea.KeyBackspace})
	}
	m = smTypeAll(m, "scratch-1.txt")
	m = smKey(m, tea.Key{Code: tea.KeyEnter})

	if m.scratchMgr.step != smStepRename {
		t.Fatalf("step = %d, want the rename prompt to stay open", m.scratchMgr.step)
	}
	if !strings.Contains(m.scratchMgr.err, "already exists") {
		t.Fatalf("err = %q, want an already-exists message", m.scratchMgr.err)
	}
	if !strings.Contains(plainView(m), "already exists") {
		t.Fatalf("the message must be visible:\n%s", plainView(m))
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s must be untouched: %v", p, err)
		}
	}
}

// TestScratchManagerRenameRejectsPathyName covers the store's guard reaching
// the dialog: a name with a separator is refused, not acted on.
func TestScratchManagerRenameRejectsPathyName(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	smSeed(t, "txt")
	m = openManager(t, m)
	m = smKey(m, tea.Key{Code: 'r', Mod: tea.ModCtrl})
	for range m.scratchMgr.renameInput.Text {
		m = smKey(m, tea.Key{Code: tea.KeyBackspace})
	}
	m = smTypeAll(m, "../escaped.txt")
	m = smKey(m, tea.Key{Code: tea.KeyEnter})

	if m.scratchMgr.err == "" {
		t.Fatal("a pathy name must be refused with a message")
	}
	if m.scratchMgr.step != smStepRename {
		t.Fatalf("step = %d, want the rename prompt to stay open", m.scratchMgr.step)
	}
}

// TestScratchManagerDeleteConfirms is the delete criterion: `ctrl+d` asks
// first, `n` keeps the file and `y` removes it.
func TestScratchManagerDeleteConfirms(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	paths := smSeed(t, "txt")
	m = openManager(t, m)

	m = smKey(m, tea.Key{Code: 'd', Mod: tea.ModCtrl})
	if m.scratchMgr.step != smStepDelete {
		t.Fatalf("step = %d, want the confirmation", m.scratchMgr.step)
	}
	if !strings.Contains(plainView(m), "Delete scratch-1.txt?") {
		t.Fatalf("the confirmation must name the file:\n%s", plainView(m))
	}
	m = smType(m, 'n')
	if _, err := os.Stat(paths[0]); err != nil {
		t.Fatalf("a cancelled delete must keep the file: %v", err)
	}

	m = smKey(m, tea.Key{Code: 'd', Mod: tea.ModCtrl})
	m = smType(m, 'y')
	if _, err := os.Stat(paths[0]); err == nil {
		t.Fatal("a confirmed delete must remove the file")
	}
	if len(m.scratchMgr.entries) != 0 {
		t.Fatalf("entries = %d, want the list refreshed to empty", len(m.scratchMgr.entries))
	}
}

// TestScratchManagerDeleteClosesOpenBuffer covers the open-buffer half of the
// delete: the editor showing the removed scratch closes with it.
func TestScratchManagerDeleteClosesOpenBuffer(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := dispatch(t, newSized(), NewScratchMsg{Ext: "sct"})
	path := m.activeWS().Panes.FocusedInstance().Editor().Path()

	m = openManager(t, m)
	m = smKey(m, tea.Key{Code: 'd', Mod: tea.ModCtrl})
	m = smType(m, 'y')

	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil {
			continue
		}
		for _, ed := range inst.Editors() {
			if ed.HasFile() && ed.Path() == path {
				t.Fatal("the editor showing the deleted scratch must close")
			}
		}
	}
}

// TestScratchManagerChangesLanguage is the language criterion: the picked
// language swaps the extension, the open buffer follows the new path — which
// is what re-languages its highlighting — and the row's language column
// follows too.
func TestScratchManagerChangesLanguage(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := dispatch(t, newSized(), NewScratchMsg{Ext: "txt"})
	old := m.activeWS().Panes.FocusedInstance().Editor().Path()

	m = openManager(t, m)
	m = smKey(m, tea.Key{Code: 'l', Mod: tea.ModCtrl})
	if m.scratchMgr.step != smStepLang {
		t.Fatalf("step = %d, want the language picker", m.scratchMgr.step)
	}
	m = smTypeAll(m, "sctest")
	// The test language plus the "Custom…" row, which no filter hides (#2340).
	if got := m.scratchMgr.filteredLangs(); len(got) != 2 || got[0].title != "Sctest" || !got[1].custom {
		t.Fatalf("filtered languages = %+v, want the test language and the custom row", got)
	}
	m = smKey(m, tea.Key{Code: tea.KeyEnter})

	want := filepath.Join(filepath.Dir(old), "scratch-1.sct")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("re-languaged scratch must exist: %v", err)
	}
	ed := m.activeWS().Panes.FocusedInstance().Editor()
	if ed.Path() != want {
		t.Fatalf("open buffer path = %q, want %q", ed.Path(), want)
	}
	if got := m.scratchMgr.entries[m.scratchMgr.pick]; got.name != "scratch-1.sct" || got.lang != "Sctest" {
		t.Fatalf("row = %+v, want the .sct scratch listed as Sctest", got)
	}
	if m.scratchMgr.step != smStepList {
		t.Fatalf("step = %d, want back on the list", m.scratchMgr.step)
	}
}

// TestScratchManagerReachableFromCreateFlow covers the entry point of #2256:
// the scratch.new language picker offers "open existing".
func TestScratchManagerReachableFromCreateFlow(t *testing.T) {
	items := scratchNewMode{}.Results("", palette.Context{})
	for _, it := range items {
		if strings.HasPrefix(it.Title, "Open existing scratch") {
			if got, ok := it.Msg.(palette.RunCommandMsg); !ok || got.ID != scratchManageCommandID {
				t.Fatalf("row msg = %#v, want RunCommandMsg for %s", it.Msg, scratchManageCommandID)
			}
			return
		}
	}
	t.Fatalf("the scratch.new picker must offer the manager: %v", items)
}

// TestScratchManagerClickOpensRow covers the mouse path: a click selects the
// row, a second one opens it.
func TestScratchManagerClickOpensRow(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	paths := smSeed(t, "txt", "sct")
	m = openManager(t, m)

	// Row 1 is the older .txt scratch (the list is newest-first).
	want := paths[0]
	if m.scratchMgr.visible()[1].path != want {
		t.Fatalf("row 1 = %q, want %q", m.scratchMgr.visible()[1].path, want)
	}
	m = smClick(t, m, smHitRow, 1)
	if m.scratchMgr.pick != 1 {
		t.Fatalf("pick = %d, want the clicked row", m.scratchMgr.pick)
	}
	m = smClick(t, m, smHitRow, 1)
	if m.scratchManagerOpen() {
		t.Fatal("the second click must open the scratch and close the manager")
	}
	if got := m.activeWS().Panes.FocusedInstance().Editor().Path(); got != want {
		t.Fatalf("focused buffer = %q, want %q", got, want)
	}
}
