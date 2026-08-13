package app

import (
	"strconv"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// archiveApp opens an archive holding n numbered members plus cmd/main.go and
// returns the model together with the pane's content origin.
func archiveApp(t *testing.T, n int) (Model, string, int, int, string) {
	t.Helper()
	files := map[string]string{"cmd/main.go": "package main\n"}
	for i := 0; i < n; i++ {
		files["f"+strconv.Itoa(i)+".txt"] = "x\n"
	}
	p := writeTestArchive(t, "src.tar", files)
	m := newSized()
	tm, _ := m.Update(OpenArchiveMsg{Path: p})
	m = tm.(Model)
	keys := archiveKeys(m)
	if len(keys) != 1 {
		t.Fatalf("expected one archive pane, got %v", keys)
	}
	r, ok := m.lay.Panes[keys[0]]
	if !ok {
		t.Fatalf("no layout rect for %q", keys[0])
	}
	return m, keys[0], r.X + paneContentX, r.Y + paneContentY, p
}

// TestArchiveWheelScrolls: the wheel over the archive pane scrolls its entry
// list and clamps at the top.
func TestArchiveWheelScrolls(t *testing.T) {
	m, key, x, y, _ := archiveApp(t, 40)
	av := m.activeWS().Panes.Get(key).Archive()
	if av.Top() != 0 {
		t.Fatalf("the list starts scrolled to %d", av.Top())
	}
	m = step(m, tea.MouseWheelMsg{X: x + 2, Y: y + 3, Button: tea.MouseWheelDown})
	if av.Top() == 0 {
		t.Fatal("the wheel did not scroll the entry list")
	}
	// Scrolling down clamps at the last page, up at the first row.
	for i := 0; i < 30; i++ {
		m = step(m, tea.MouseWheelMsg{X: x + 2, Y: y + 3, Button: tea.MouseWheelDown})
	}
	if got, maxTop := av.Top(), av.Rows()-1; got >= maxTop {
		t.Fatalf("scrolling down ran past the last page: top = %d of %d rows", got, av.Rows())
	}
	if av.Cursor() < av.Top() {
		t.Fatalf("cursor %d left the window at top %d", av.Cursor(), av.Top())
	}
	for i := 0; i < 30; i++ {
		m = step(m, tea.MouseWheelMsg{X: x + 2, Y: y + 3, Button: tea.MouseWheelUp})
	}
	if av.Top() != 0 {
		t.Fatalf("scrolling up clamps at top = %d, want 0", av.Top())
	}
}

// TestArchiveClickSelectsRow: a left click focuses the pane and selects the
// row under the pointer.
func TestArchiveClickSelectsRow(t *testing.T) {
	m, key, x, y, _ := archiveApp(t, 0)
	// rows: cmd, cmd/main.go — y+0 is the header line, rows start at y+1.
	av := m.activeWS().Panes.Get(key).Archive()
	m = step(m, tea.MouseClickMsg{X: x + 10, Y: y + 2, Button: tea.MouseLeft})
	if m.activeWS().Panes.Focused() != key {
		t.Fatalf("the click did not focus the pane: %q", m.activeWS().Panes.Focused())
	}
	if got := av.RowName(av.Cursor()); got != "cmd/main.go" {
		t.Fatalf("the click selected %q", got)
	}
}

// TestArchiveDoubleClickOpensEntry: two quick clicks on a file row open it
// read-only, the same end state enter produces.
func TestArchiveDoubleClickOpensEntry(t *testing.T) {
	m, key, x, y, p := archiveApp(t, 0)
	_ = key
	out, _ := m.Update(tea.MouseClickMsg{X: x + 10, Y: y + 2, Button: tea.MouseLeft})
	m = out.(Model)
	out, cmd := m.Update(tea.MouseClickMsg{X: x + 10, Y: y + 2, Button: tea.MouseLeft})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("the double click emitted no command")
	}
	for _, msg := range dispatched(cmd) {
		out, c := m.Update(msg)
		m = drainCmd(out.(Model), c)
	}
	vpath := archiveEntryPath(p, "cmd/main.go")
	ed := readOnlyEditor(m, vpath)
	if ed == nil {
		t.Fatal("the double click must open the entry in an editor tab")
	}
	if !ed.ReadOnly() {
		t.Fatal("a double-clicked archive entry opens read-only")
	}
	// Editing is refused, like the enter path (E45).
	*ed, _ = ed.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if ed.Text() != "package main" {
		t.Fatalf("the buffer was edited: %q", ed.Text())
	}
}

// TestArchiveFoldGlyphClickToggles: a single click on a directory's fold
// glyph collapses it.
func TestArchiveFoldGlyphClickToggles(t *testing.T) {
	m, key, x, y, _ := archiveApp(t, 0)
	av := m.activeWS().Panes.Get(key).Archive()
	if av.Rows() != 2 {
		t.Fatalf("rows = %d, want 2", av.Rows())
	}
	m = step(m, tea.MouseClickMsg{X: x + 1, Y: y + 1, Button: tea.MouseLeft})
	if av.Rows() != 1 {
		t.Fatalf("the glyph click did not collapse: rows = %d", av.Rows())
	}
}

// TestArchiveKeyboardNavigationAndOpen: with the pane focused, j/k and the
// page keys move the cursor and enter opens the selected file read-only —
// the end-to-end keyboard path (#1852).
func TestArchiveKeyboardNavigationAndOpen(t *testing.T) {
	m, key, _, _, p := archiveApp(t, 40)
	if m.activeWS().Panes.Focused() != key {
		t.Fatalf("the archive pane must open focused, got %q", m.activeWS().Panes.Focused())
	}
	av := m.activeWS().Panes.Get(key).Archive()
	if got := av.RowName(av.Cursor()); got != "cmd" {
		t.Fatalf("cursor starts on %q", got)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := av.RowName(av.Cursor()); got != "cmd/main.go" {
		t.Fatalf("j landed on %q", got)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: 'k', Text: "k"})
	if got := av.RowName(av.Cursor()); got != "cmd" {
		t.Fatalf("k landed on %q", got)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyPgDown})
	down := av.Cursor()
	if down <= 1 {
		t.Fatalf("pgdown moved the cursor to %d", down)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyPgUp})
	if av.Cursor() >= down {
		t.Fatalf("pgup left the cursor at %d", av.Cursor())
	}
	// Back to the file row and open it.
	m = drainKey(m, tea.KeyPressMsg{Code: 'g', Text: "g"})
	m = drainKey(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := av.RowName(av.Cursor()); got != "cmd/main.go" {
		t.Fatalf("cursor on %q before enter", got)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	vpath := archiveEntryPath(p, "cmd/main.go")
	if ed := readOnlyEditor(m, vpath); ed == nil {
		t.Fatalf("enter did not open %q read-only", vpath)
	}
}
