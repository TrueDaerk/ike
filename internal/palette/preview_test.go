package palette

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// listWidth is the result column's width for the current box.
func listWidth(p *Palette) (listW, previewW int) {
	return previewSplit(p.boxWidth() - 4)
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

// TestPreviewClickIgnoresCodeColumn keeps presses on the excerpt inert — the
// row behind it must not activate (#2047).
func TestPreviewClickIgnoresCodeColumn(t *testing.T) {
	p, _ := openUsages(t, 20, 40)
	listW, _ := listWidth(p)
	if cmd := p.Click(2+listW+4, 1+2); cmd != nil {
		t.Fatal("a press in the preview column activated a row")
	}
	if p.Click(2+1, 1+2) == nil {
		t.Fatal("a press in the result column should activate its row")
	}
}

// TestPlainModeKeepsSingleColumn makes sure the split is opt-in: a mode that
// is not a PreviewMode renders exactly as before.
func TestPlainModeKeepsSingleColumn(t *testing.T) {
	p := New(Config{MaxResults: 5}, fileMode(numberedFiles(3)...))
	p.SetSize(160, 50)
	p.Open(Context{Root: "."})
	if p.previewing() {
		t.Fatal("a plain mode should not preview")
	}
	if got := p.visibleRows(); got != 5 {
		t.Fatalf("visibleRows = %d, want the configured 5", got)
	}
	if strings.Contains(ansi.Strip(p.View()), "row 1") {
		t.Fatal("a plain mode rendered a code preview")
	}
}
