package palette

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/ui"
)

// preview_test.go covers the find-usages popup's layout (#2047): the code
// column beside the result list, and the [11, 40] bound on the result rows.

// usagesMode is a stand-in for the app's references mode: a locked
// PreviewMode whose rows point at lines of one file.
type usagesMode struct {
	path string
	n    int
}

func (u *usagesMode) Prefix() rune        { return '&' }
func (u *usagesMode) Placeholder() string { return "usages…" }
func (u *usagesMode) CodePreview() bool   { return true }

func (u *usagesMode) Results(query string, _ Context) []Item {
	var out []Item
	for i := 1; i <= u.n; i++ {
		title := fmt.Sprintf("sample.go:%d", i)
		if query != "" && !strings.Contains(title, query) {
			continue
		}
		out = append(out, Item{Title: title, Msg: RunCommandMsg{ID: title}, Preview: PreviewTarget{Path: u.path, Line: i}})
	}
	return out
}

// sampleFile writes a file whose n-th line reads "row <n>".
func sampleFile(t *testing.T, n int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sample.go")
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "row %d\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// openUsages returns a palette locked to a usages mode over n references.
func openUsages(t *testing.T, refs, fileLines int) (*Palette, *usagesMode) {
	t.Helper()
	mode := &usagesMode{path: sampleFile(t, fileLines), n: refs}
	p := New(Config{MaxResults: 2}, mode)
	p.SetSize(160, 50)
	p.OpenLocked(Context{Root: "."}, '&')
	return p, mode
}

// bodyRows returns the rendered result-block rows: everything below the
// prompt and separator, without the box's border rows.
func bodyRows(t *testing.T, p *Palette) []string {
	t.Helper()
	lines := strings.Split(p.View(), "\n")
	if len(lines) < 5 {
		t.Fatalf("view is only %d lines:\n%s", len(lines), p.View())
	}
	// border, prompt, separator … result rows … border
	return lines[3 : len(lines)-1]
}

// TestUsagesPopupMinHeight is the min-11 criterion (#2047): even two usages
// keep the popup at ui.MinResultRows content rows.
func TestUsagesPopupMinHeight(t *testing.T) {
	p, _ := openUsages(t, 2, 40)
	if got := len(bodyRows(t, p)); got != ui.MinResultRows {
		t.Fatalf("two-usage popup has %d result rows, want %d", got, ui.MinResultRows)
	}
	// Zero results (a filter that matches nothing) keeps the height too.
	p.query = "zzz"
	p.recompute()
	if len(p.items) != 0 {
		t.Fatalf("expected no items, got %d", len(p.items))
	}
	if got := len(bodyRows(t, p)); got != ui.MinResultRows {
		t.Fatalf("empty popup has %d result rows, want %d", got, ui.MinResultRows)
	}
}

// TestUsagesPopupMaxHeight is the max-40 criterion: a big resize delta cannot
// push the window past ui.MaxResultRows, and the list scrolls instead.
func TestUsagesPopupMaxHeight(t *testing.T) {
	p, _ := openUsages(t, 300, 400)
	var sizes ui.WinSizes
	p.SetSizeStore(&sizes)
	sizes.Nudge(winKind, 0, 80) // ask for 92 rows
	if got := p.visibleRows(); got != ui.MaxResultRows {
		t.Fatalf("visibleRows = %d, want %d", got, ui.MaxResultRows)
	}
	if got := len(bodyRows(t, p)); got != ui.MaxResultRows {
		t.Fatalf("popup has %d result rows, want %d", got, ui.MaxResultRows)
	}
	// Still scrollable: walking to the end brings the last usage into view.
	for range 299 {
		p.move(1)
	}
	if !strings.Contains(ansi.Strip(strings.Join(bodyRows(t, p), "\n")), "sample.go:300") {
		t.Fatal("the last usage did not scroll into view")
	}
}

// listWidth is the result column's width for the current box — read through
// the popup's own geometry, so the test follows the adaptive preview width
// (#2327) exactly as the renderer does.
func listWidth(p *Palette) (listW, previewW int) {
	return p.previewSplit(p.boxWidth() - 4)
}

// previewColumn returns the text right of the vertical rule, asserting the
// rule sits in the same column on every row.
func previewColumn(t *testing.T, p *Palette) string {
	t.Helper()
	listW, previewW := listWidth(p)
	if previewW <= 0 {
		t.Fatalf("no preview column at box width %d", p.boxWidth())
	}
	const contentX = 2 // box border + padding
	var b strings.Builder
	for _, ln := range bodyRows(t, p) {
		r := []rune(ansi.Strip(ln))
		if len(r) <= contentX+listW+1 {
			t.Fatalf("row too short for the split: %q", string(r))
		}
		if got := r[contentX+listW+1]; got != '│' {
			t.Fatalf("no vertical rule at the split, got %q in %q", got, string(r))
		}
		b.WriteString(strings.TrimRight(string(r[contentX+listW+3:]), " │") + "\n")
	}
	return b.String()
}

// TestUsagesPreviewFollowsCursor is the preview-update criterion: moving the
// selection re-renders the code column around the new target.
func TestUsagesPreviewFollowsCursor(t *testing.T) {
	p, _ := openUsages(t, 200, 400)
	first := previewColumn(t, p)
	if !strings.Contains(first, "row 1") {
		t.Fatalf("preview of the first usage lacks its target:\n%s", first)
	}
	for range 99 {
		p.Update(downKey())
	}
	second := previewColumn(t, p)
	if !strings.Contains(second, "row 100") || !strings.Contains(second, "row 103") {
		t.Fatalf("preview did not follow the cursor to usage 100:\n%s", second)
	}
}

// TestUsagesPreviewMissingFile covers the no-crash criterion.
func TestUsagesPreviewMissingFile(t *testing.T) {
	p, mode := openUsages(t, 3, 10)
	if err := os.Remove(mode.path); err != nil {
		t.Fatal(err)
	}
	p.prev.Reset() // the file was read before it vanished
	if !strings.Contains(previewColumn(t, p), "preview unavailable") {
		t.Fatal("a deleted target should render the unavailable notice")
	}
}

// TestPreviewClickIgnoresCodeColumn keeps presses on the excerpt from
// activating the row behind it (#2047); since #2327 such a press focuses the
// column instead, and a press back in the list blurs it.
func TestPreviewClickIgnoresCodeColumn(t *testing.T) {
	p, _ := openUsages(t, 20, 40)
	listW, _ := listWidth(p)
	if cmd := p.Click(2+listW+4, 1+2); cmd != nil {
		t.Fatal("a press in the preview column activated a row")
	}
	if !p.prev.Focused() {
		t.Fatal("a press in the preview column must focus it")
	}
	if p.Click(2+1, 1+2) == nil {
		t.Fatal("a press in the result column should activate its row")
	}
	if p.prev.Focused() {
		t.Fatal("a press back in the result column must blur the preview")
	}
}

// focusKey is the chord that hands the excerpt column the keyboard (#2327).
func focusKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'p', Mod: tea.ModAlt} }

// TestPreviewFocusScrolls is the scroll criterion in the popup: focused, the
// editor motions walk the excerpt while the result selection stays put, and
// esc hands the query field the keyboard back instead of closing.
func TestPreviewFocusScrolls(t *testing.T) {
	p, _ := openUsages(t, 20, 400)
	for range 99 {
		p.Update(downKey())
	}
	before := previewColumn(t, p)
	p.Update(focusKey())
	if !p.prev.Focused() {
		t.Fatal("alt+p must focus the preview column")
	}
	sel := p.selected
	p.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	p.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if scrolled := previewColumn(t, p); scrolled == before {
		t.Fatalf("j did not scroll the excerpt:\n%s", scrolled)
	}
	if p.selected != sel {
		t.Fatalf("the selection moved to %d while the preview had focus", p.selected)
	}
	if p.query != "" {
		t.Fatalf("the motion leaked into the query: %q", p.query)
	}
	p.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
	if again := previewColumn(t, p); again != before {
		t.Fatalf("z did not return to the target:\n%s", again)
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if p.prev.Focused() || !p.IsOpen() {
		t.Fatal("esc must blur the preview and keep the popup open")
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if p.IsOpen() {
		t.Fatal("a second esc must close the popup")
	}
}

// TestPlainModeKeepsSingleColumn makes sure the split stays opt-in and
// locked-only: the default centered palette — even when its file mode is a
// PreviewMode (#2053) — renders exactly as before.
func TestPlainModeKeepsSingleColumn(t *testing.T) {
	p := New(Config{MaxResults: 5}, fileMode(numberedFiles(3)...))
	p.SetSize(160, 50)
	p.Open(Context{Root: "."})
	if p.previewing() {
		t.Fatal("an unlocked open should not preview")
	}
	if got := p.visibleRows(); got != 5 {
		t.Fatalf("visibleRows = %d, want the configured 5", got)
	}
	if strings.Contains(ansi.Strip(p.View()), "row 1") {
		t.Fatal("a plain mode rendered a code preview")
	}
}

// filePickerDir writes n numbered files whose first line names them, and
// returns the directory plus a palette locked to an "@" file picker over it.
func filePickerDir(t *testing.T, n int) (*Palette, string) {
	t.Helper()
	dir := t.TempDir()
	names := make([]string, n)
	for i := range n {
		names[i] = fmt.Sprintf("file%02d.go", i)
		body := fmt.Sprintf("// head of %s\n", names[i]) + strings.Repeat("filler\n", 30)
		if err := os.WriteFile(filepath.Join(dir, names[i]), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	p := New(Config{MaxResults: 5}, fileMode(names...))
	p.SetSize(160, 50)
	p.OpenLocked(Context{Root: dir}, '@')
	return p, dir
}

// TestFilePickerPreviewsSelectedFile is the file-picker half of #2053: the
// locked "@" picker shows the head of the highlighted file, and the excerpt
// follows the cursor down the list.
func TestFilePickerPreviewsSelectedFile(t *testing.T) {
	p, _ := filePickerDir(t, 3)
	if !p.previewing() {
		t.Fatal("the locked file picker must render the code column")
	}
	if col := previewColumn(t, p); !strings.Contains(col, "head of file00.go") {
		t.Fatalf("preview column = %q, want the first file's head", col)
	}
	p.move(1)
	if col := previewColumn(t, p); !strings.Contains(col, "head of file01.go") {
		t.Fatalf("preview column after moving = %q, want the second file's head", col)
	}
}

// TestFilePickerRowsCarryTargets: every row that opens a file points the
// preview at it; a directory row (which only descends) carries no target, so
// its column stays blank rather than claiming the file is unreadable.
func TestFilePickerRowsCarryTargets(t *testing.T) {
	_, dir := filePickerDir(t, 2)
	f := fileMode("file00.go", "file01.go")
	for _, it := range f.Results("", Context{Root: dir}) {
		open, ok := it.Msg.(OpenFileMsg)
		if !ok {
			continue
		}
		if it.Preview.Path != open.Path || it.Preview.Line != 1 {
			t.Fatalf("row %q preview = %+v, want %s:1", it.Title, it.Preview, open.Path)
		}
	}
	sub := filepath.Join(dir, "pkg")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, it := range f.Results("pkg", Context{Root: dir}) {
		if _, ok := it.Msg.(OpenPathDescendMsg); ok && it.Preview.Path != "" {
			t.Fatalf("directory row %q carries a preview target %+v", it.Title, it.Preview)
		}
	}
}

// TestAnchoredFilePickerKeepsSingleColumn: the "@" finder floated over an
// editor pane is too small for two columns and stays one, as before (#2053).
func TestAnchoredFilePickerKeepsSingleColumn(t *testing.T) {
	p, dir := filePickerDir(t, 3)
	p.OpenAnchored(Context{Root: dir}, '@', 4, 2, 60)
	if p.previewing() {
		t.Fatal("an anchored open must not split into two columns")
	}
	if strings.Contains(ansi.Strip(p.View()), "head of file00.go") {
		t.Fatal("the anchored finder rendered a code preview")
	}
}
