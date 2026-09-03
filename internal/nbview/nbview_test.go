package nbview

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/theme"
)

// press feeds one key to the model and returns the command it emitted.
func press(m *Model, key string) tea.Cmd {
	return m.Update(tea.KeyPressMsg{Code: keyCode(key), Text: keyText(key)})
}

// keyCode maps the single-character keys the tests press.
func keyCode(key string) rune {
	switch key {
	case "enter":
		return tea.KeyEnter
	case "esc":
		return tea.KeyEscape
	}
	return rune(key[0])
}

func keyText(key string) string {
	switch key {
	case "enter", "esc":
		return ""
	}
	return key
}

// typeQuery presses "/" and types a query, then enter.
func typeQuery(m *Model, q string) {
	press(m, "/")
	for _, r := range q {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	press(m, "enter")
}

// body returns the rendered rows joined, plain.
func body(m *Model) string { return strings.Join(m.Rows(), "\n") }

// TestRendersEveryCellAndOutputKind (#2425): markdown, code, stream,
// execute_result, degraded HTML, image metadata, error and the empty cell all
// reach the rendered rows.
func TestRendersEveryCellAndOutputKind(t *testing.T) {
	m := newModel(t, writeFixture(t))
	if m.Err() != nil {
		t.Fatalf("Err = %v", m.Err())
	}
	out := body(&m)
	for _, want := range []string{
		"Title",                     // markdown cell, rendered
		"Some prose about the data", // glamour drops the emphasis markers
		"import sys",                // code cell source
		"stdout", "hello",           // stream output with its channel label
		"stderr", "a warning",
		"Out[2]", "42", // execute_result with its execution count
		"text/html as text", "name", "alpha", // the degraded HTML table
		"image/png · 8×4 px",                  // the image output's metadata label
		"ZeroDivisionError: division by zero", // the error output
		"Traceback (most recent call last):",
		"(empty code cell)", // the empty cell still shows
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered rows missing %q:\n%s", want, out)
		}
	}
	// The traceback's ANSI colouring is stripped, not passed through.
	if strings.Contains(out, "0;31m") {
		t.Fatalf("traceback kept its ANSI escapes:\n%s", out)
	}
	// The gutter labels every cell with its index, type and execution count —
	// a never-run cell with an empty count rather than [0].
	for _, want := range []string{"1 md", "2 code [1]"} {
		if !strings.Contains(stripANSI(m.View()), want) {
			t.Fatalf("gutter missing %q", want)
		}
	}
	press(&m, "G")
	if !strings.Contains(stripANSI(m.View()), "6 code [ ]") {
		t.Fatalf("gutter of the last cell:\n%s", stripANSI(m.View()))
	}
}

// TestCellNavigation (#2425): j/k step cells, g/G go to the ends.
func TestCellNavigation(t *testing.T) {
	m := newModel(t, writeFixture(t))
	if m.Cursor() != 0 {
		t.Fatalf("initial cursor = %d", m.Cursor())
	}
	press(&m, "j")
	press(&m, "j")
	if m.Cursor() != 2 {
		t.Fatalf("after jj cursor = %d, want 2", m.Cursor())
	}
	press(&m, "k")
	if m.Cursor() != 1 {
		t.Fatalf("after k cursor = %d, want 1", m.Cursor())
	}
	press(&m, "G")
	if m.Cursor() != len(m.Cells())-1 {
		t.Fatalf("after G cursor = %d, want %d", m.Cursor(), len(m.Cells())-1)
	}
	press(&m, "g")
	if m.Cursor() != 0 {
		t.Fatalf("after g cursor = %d, want 0", m.Cursor())
	}
	// k on the first cell stays put rather than wrapping or panicking.
	press(&m, "k")
	if m.Cursor() != 0 {
		t.Fatalf("k at top cursor = %d", m.Cursor())
	}
}

// TestFoldOutputs (#2425): enter collapses the cursor cell's outputs to one
// marker row and expands them again.
func TestFoldOutputs(t *testing.T) {
	m := newModel(t, writeFixture(t))
	press(&m, "j") // cell 2: the stream outputs
	before := body(&m)
	if !strings.Contains(before, "a warning") {
		t.Fatal("outputs not shown before folding")
	}
	press(&m, "enter")
	if !m.Folded(1) {
		t.Fatal("cell not folded")
	}
	folded := body(&m)
	if strings.Contains(folded, "a warning") {
		t.Fatalf("folded cell still shows its output:\n%s", folded)
	}
	if !strings.Contains(folded, "2 outputs folded") {
		t.Fatalf("no fold marker:\n%s", folded)
	}
	press(&m, "enter")
	if m.Folded(1) || !strings.Contains(body(&m), "a warning") {
		t.Fatal("unfold did not restore the outputs")
	}
	// A cell without outputs has nothing to fold and says so by doing nothing.
	press(&m, "g")
	press(&m, "enter")
	if m.Folded(0) {
		t.Fatal("markdown cell folded despite having no outputs")
	}
}

// TestSearchAcrossCellSources (#2425): "/" matches cell sources — not
// outputs — and n/N step the matches, moving the cell cursor with them.
func TestSearchAcrossCellSources(t *testing.T) {
	m := newModel(t, writeFixture(t))
	typeQuery(&m, "print")
	if len(m.Matches()) != 1 {
		t.Fatalf("matches = %d, want 1", len(m.Matches()))
	}
	if m.Cursor() != 1 {
		t.Fatalf("cursor = %d, want the matching cell 1", m.Cursor())
	}
	if m.Searching() {
		t.Fatal("enter left the search line open")
	}
	// "hello" appears in the source *and* in the stream output; only the
	// source counts.
	m2 := newModel(t, writeFixture(t))
	typeQuery(&m2, "hello")
	if len(m2.Matches()) != 1 {
		t.Fatalf("output text matched: %d matches", len(m2.Matches()))
	}
	// Several matches step and wrap.
	m3 := newModel(t, writeFixture(t))
	typeQuery(&m3, "o")
	if len(m3.Matches()) < 2 {
		t.Fatalf("matches = %d, want at least 2", len(m3.Matches()))
	}
	first := m3.Cursor()
	press(&m3, "n")
	if m3.Cursor() == first && len(m3.Matches()) > 1 {
		t.Fatal("n did not move to the next match")
	}
	press(&m3, "N")
	if m3.Cursor() != first {
		t.Fatalf("N did not return to the first match: %d != %d", m3.Cursor(), first)
	}
	// esc drops the match set.
	press(&m3, "esc")
	if len(m3.Matches()) != 0 {
		t.Fatal("esc kept the matches")
	}
}

// TestSearchIsCaseInsensitive (#2425).
// TestSearchIsSmartcase: the in-pane search's one matching rule (#2461) — an
// all-lowercase query folds case, an uppercase rune makes it exact.
func TestSearchIsSmartcase(t *testing.T) {
	m := newModel(t, writeFixture(t))
	typeQuery(&m, "import")
	if len(m.Matches()) != 1 {
		t.Fatalf("matches = %d, want 1", len(m.Matches()))
	}
	m2 := newModel(t, writeFixture(t))
	typeQuery(&m2, "IMPORT")
	if len(m2.Matches()) != 0 {
		t.Fatalf("an uppercase query is exact: matches = %d, want 0", len(m2.Matches()))
	}
}

// TestSearchNarrowsLiveWithACaret (#2461): the match set follows every
// keystroke, the line renders a caret and the shared counter, and esc while
// the line is open drops the search.
func TestSearchNarrowsLiveWithACaret(t *testing.T) {
	m := newModel(t, writeFixture(t))
	press(&m, "/")
	for _, r := range "imp" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if !m.Searching() || len(m.Matches()) != 1 {
		t.Fatalf("typing must narrow live: open=%v matches=%d", m.Searching(), len(m.Matches()))
	}
	if v := m.View(); !strings.Contains(v, "/imp") || !strings.Contains(v, "1/1") {
		t.Fatalf("the line must show the query and the counter:\n%s", v)
	}
	m.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
	if len(m.Matches()) != 0 || !strings.Contains(m.View(), "no matches") {
		t.Fatalf("a miss must show live: matches=%d\n%s", len(m.Matches()), m.View())
	}
	press(&m, "esc")
	if m.Searching() || len(m.Matches()) != 0 {
		t.Fatal("esc while the line is open must drop the search")
	}
}

// TestCopyAndScratchCarryTheCellSource (#2425): y copies the cursor cell's
// source, e asks for a scratch in the notebook's language.
func TestCopyAndScratchCarryTheCellSource(t *testing.T) {
	m := newModel(t, writeFixture(t))
	press(&m, "j") // the code cell

	cmd := press(&m, "y")
	if cmd == nil {
		t.Fatal("y produced no command")
	}
	cp, ok := cmd().(CopyMsg)
	if !ok {
		t.Fatalf("y produced %T", cmd())
	}
	if cp.Text != "import sys\nprint(\"hello\")" {
		t.Fatalf("copied %q", cp.Text)
	}
	if !strings.Contains(cp.What, "cell 2") {
		t.Fatalf("copy label = %q", cp.What)
	}

	cmd = press(&m, "e")
	if cmd == nil {
		t.Fatal("e produced no command")
	}
	sc, ok := cmd().(ScratchMsg)
	if !ok {
		t.Fatalf("e produced %T", cmd())
	}
	if sc.Ext != "py" {
		t.Fatalf("scratch ext = %q, want py", sc.Ext)
	}
	if !strings.HasPrefix(sc.Content, "import sys") {
		t.Fatalf("scratch content = %q", sc.Content)
	}

	// A markdown cell opens as markdown whatever the kernel is.
	press(&m, "g")
	sc = press(&m, "e")().(ScratchMsg)
	if sc.Ext != "md" {
		t.Fatalf("markdown scratch ext = %q, want md", sc.Ext)
	}

	// An empty cell has nothing to copy or open.
	press(&m, "G")
	if cmd := press(&m, "y"); cmd != nil {
		t.Fatal("y on the empty cell produced a command")
	}
	if cmd := press(&m, "e"); cmd != nil {
		t.Fatal("e on the empty cell produced a command")
	}
}

// TestSaveImageOutput (#2425): o on a cell with an image output asks the app
// to write it next to the notebook, with the decoded bytes.
func TestSaveImageOutput(t *testing.T) {
	path := writeFixture(t)
	m := newModel(t, path)
	press(&m, "G")
	press(&m, "k")
	press(&m, "k") // cell 4, the image output
	cmd := press(&m, "o")
	if cmd == nil {
		t.Fatal("o produced no command")
	}
	msg, ok := cmd().(SaveImageMsg)
	if !ok {
		t.Fatalf("o produced %T", cmd())
	}
	if !strings.HasSuffix(msg.Path, "fixture-cell4.png") {
		t.Fatalf("suggested path = %q", msg.Path)
	}
	if !strings.HasPrefix(string(msg.Data), "\x89PNG") {
		t.Fatal("saved bytes are not the decoded PNG")
	}
	// A cell without an image output has nothing to save.
	press(&m, "g")
	if cmd := press(&m, "o"); cmd != nil {
		t.Fatal("o on an image-free cell produced a command")
	}
}

// TestMalformedNotebookExplainsItself (#2425): the parse error and the "open
// as text" hint are the pane's whole body, and keys do nothing.
func TestMalformedNotebookExplainsItself(t *testing.T) {
	m := newModel(t, writeNotebook(t, `{"cells": [ oops }`))
	if m.Err() == nil {
		t.Fatal("malformed notebook parsed")
	}
	view := stripANSI(m.View())
	if !strings.Contains(view, "invalid character") {
		t.Fatalf("view does not show the parse error:\n%s", view)
	}
	if !strings.Contains(view, "Open File As…") {
		t.Fatalf("view does not offer the open-as-text way out:\n%s", view)
	}
	if cmd := press(&m, "y"); cmd != nil {
		t.Fatal("a key acted on an unparsed notebook")
	}
}

// TestReloadPicksUpExternalChanges (#2425): the watcher's re-read shows the
// new cells and keeps the cursor inside the document.
func TestReloadPicksUpExternalChanges(t *testing.T) {
	path := writeFixture(t)
	m := newModel(t, path)
	press(&m, "G")
	if !strings.Contains(body(&m), "import sys") {
		t.Fatal("fixture did not render")
	}
	replacement := `{"cells": [{"cell_type": "code", "execution_count": 9,
		"source": "after = 1", "outputs": []}],
		"metadata": {"language_info": {"name": "python", "file_extension": ".py"}},
		"nbformat": 4, "nbformat_minor": 5}`
	if err := os.WriteFile(path, []byte(replacement), 0o644); err != nil {
		t.Fatal(err)
	}
	m.Reload()
	out := body(&m)
	if strings.Contains(out, "import sys") || !strings.Contains(out, "after = 1") {
		t.Fatalf("reload did not re-render:\n%s", out)
	}
	if m.Cursor() != 0 {
		t.Fatalf("cursor = %d, want it clamped into the shorter notebook", m.Cursor())
	}
	// A file that turns malformed becomes the error pane rather than stale rows.
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.Reload()
	if m.Err() == nil {
		t.Fatal("reload of a broken notebook kept the old content")
	}
}

// TestImagesDegradeWithoutGraphics (#2425): with no Kitty support the image
// output is its metadata label and places nothing; enabling support adds the
// placement the app's reconcile transmits.
func TestImagesDegradeWithoutGraphics(t *testing.T) {
	m := newModel(t, writeFixture(t))
	if !m.HasImages() {
		t.Fatal("HasImages = false for a notebook with an image output")
	}
	if ids := m.ImageIDs(); len(ids) != 0 {
		t.Fatalf("placed %d images without graphics support", len(ids))
	}
	m.SetGraphics(true)
	if ids := m.ImageIDs(); len(ids) != 1 {
		t.Fatalf("placed %d images with graphics support, want 1", len(ids))
	}
	if seqs := m.SyncSeqs(); len(seqs) != 1 {
		t.Fatalf("SyncSeqs = %d, want one transmit", len(seqs))
	}
	if len(m.TransmittedIDs()) != 1 {
		t.Fatal("transmit was not recorded")
	}
	if seqs := m.SyncSeqs(); len(seqs) != 0 {
		t.Fatal("a second sync re-transmitted an unchanged placement")
	}
	// Folding the image cell's outputs releases the placement, so the app's
	// reconcile deletes it instead of leaving a ghost.
	m.gotoCell(3)
	press(&m, "enter")
	if ids := m.ImageIDs(); len(ids) != 0 {
		t.Fatalf("folded image still placed: %v", ids)
	}
	m.ResetImages()
	if len(m.TransmittedIDs()) != 0 {
		t.Fatal("ResetImages kept the transmitted state")
	}
}

// TestEmptyNotebookRenders (#2425): a notebook with no cells at all is a
// valid document, not an error.
func TestEmptyNotebookRenders(t *testing.T) {
	m := newModel(t, writeNotebook(t, `{"cells": [], "nbformat": 4, "nbformat_minor": 5, "metadata": {}}`))
	if m.Err() != nil {
		t.Fatalf("Err = %v", m.Err())
	}
	if len(m.Rows()) != 0 {
		t.Fatalf("rows = %d, want 0", len(m.Rows()))
	}
	// The footer still renders, and neither navigation nor search panics.
	press(&m, "j")
	press(&m, "/")
	if m.Searching() {
		t.Fatal("search opened over an empty notebook")
	}
	if m.View() == "" {
		t.Fatal("empty notebook rendered nothing at all")
	}
}

// TestUnreadableFileExplainsItself (#2425).
func TestUnreadableFileExplainsItself(t *testing.T) {
	m := New("notebook", "/nonexistent/does-not-exist.ipynb", theme.DefaultPalette())
	m.SetSize(60, 10)
	if m.Err() == nil {
		t.Fatal("missing file opened cleanly")
	}
	if !strings.Contains(stripANSI(m.View()), "does-not-exist.ipynb") {
		t.Fatal("view does not name the file")
	}
}
