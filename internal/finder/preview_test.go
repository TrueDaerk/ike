package finder

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/codepreview"
	"ike/internal/locations"
	"ike/internal/search"
	"ike/internal/ui"
)

// resultBlock returns the rendered rows of the results block — the fixed
// height region between the blank line after the Exclude row and the blank
// line before the status row.
func resultBlock(t *testing.T, m *Model) []string {
	t.Helper()
	lines := strings.Split(m.View(), "\n")
	if m.lay.listTop < 0 {
		t.Fatal("no results block in the view")
	}
	// +1 for the top border row the box adds around the content rows.
	from := m.lay.listTop + 1
	return lines[from : from+m.lay.listRows]
}

// sample writes a file with n numbered lines and returns its path.
func sample(t *testing.T, n int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sample.go")
	var b strings.Builder
	for i := 1; i <= n; i++ {
		b.WriteString("row " + strconv.Itoa(i) + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// openFinder returns an open finder sized to a roomy terminal.
func openFinder(t *testing.T, w, h int) *Model {
	t.Helper()
	m := New(search.New(nil))
	m.SetSize(w, h)
	m.Open("")
	m.query = "row"
	return m
}

// TestResultsBlockMinHeight is the min-11 criterion (#2047): with no matches
// at all the block still occupies ui.MinResultRows rows.
func TestResultsBlockMinHeight(t *testing.T) {
	m := openFinder(t, 160, 20) // 20/2-9 = 1, well under the floor
	if got := len(resultBlock(t, m)); got != ui.MinResultRows {
		t.Fatalf("empty results block is %d rows, want %d", got, ui.MinResultRows)
	}
	m.list.Append([]locations.Item{
		{Path: "a.go", Line: 1, Text: "row 1"},
		{Path: "a.go", Line: 2, Text: "row 2"},
	})
	if got := len(resultBlock(t, m)); got != ui.MinResultRows {
		t.Fatalf("two-match block is %d rows, want %d", got, ui.MinResultRows)
	}
}

// TestResultsBlockMaxHeight is the max-40 criterion: a tall terminal and a
// long result list stop the block at ui.MaxResultRows, and the list scrolls
// instead of growing.
func TestResultsBlockMaxHeight(t *testing.T) {
	m := openFinder(t, 200, 200) // 200/2-9 = 91, well over the ceiling
	items := make([]locations.Item, 0, 200)
	for i := 1; i <= 200; i++ {
		items = append(items, locations.Item{Path: "a.go", Line: i, Text: "row " + strconv.Itoa(i)})
	}
	m.list.Append(items)
	rows := resultBlock(t, m)
	if len(rows) != ui.MaxResultRows {
		t.Fatalf("block is %d rows, want %d", len(rows), ui.MaxResultRows)
	}
	// Scrolling: stepping to the last match must bring it into the window.
	m.list.SetCursor(m.list.Total() - 1)
	if !strings.Contains(ansi.Strip(strings.Join(resultBlock(t, m), "\n")), "row 200") {
		t.Fatal("the last match did not scroll into view")
	}
}

// TestPreviewFollowsCursor is the preview-update criterion: the code column
// beside the list re-renders around the newly selected match.
func TestPreviewFollowsCursor(t *testing.T) {
	path := sample(t, 120)
	m := openFinder(t, 200, 60)
	m.list.Append([]locations.Item{
		{Path: path, Line: 10, Text: "row 10"},
		{Path: path, Line: 100, Text: "row 100"},
	})
	listW, previewW := codepreview.Split(m.boxWidth() - 6)
	if previewW <= 0 {
		t.Fatalf("no preview column at width %d", m.width)
	}
	// Content starts two cells in (box border + padding); JoinColumns puts
	// the rule one cell past the list column and the preview two past it.
	const contentX = 2
	right := func() string {
		var b strings.Builder
		for _, ln := range resultBlock(t, m) {
			r := []rune(ansi.Strip(ln))
			if len(r) <= contentX+listW+1 {
				t.Fatalf("results row is too short for the split: %q", string(r))
			}
			if got := r[contentX+listW+1]; got != '│' {
				t.Fatalf("no vertical rule at the column split, got %q in %q", got, string(r))
			}
			b.WriteString(strings.TrimRight(string(r[contentX+listW+3:]), " │") + "\n")
		}
		return b.String()
	}
	first := right()
	if !strings.Contains(first, "row 12") {
		t.Fatalf("preview of the first match lacks its surroundings:\n%s", first)
	}
	m.list.Step(1)
	second := right()
	if !strings.Contains(second, "row 102") {
		t.Fatalf("preview did not follow the cursor:\n%s", second)
	}
}

// TestPreviewMissingFile covers the no-crash criterion for deleted targets.
func TestPreviewMissingFile(t *testing.T) {
	m := openFinder(t, 200, 60)
	m.list.Append([]locations.Item{{Path: filepath.Join(t.TempDir(), "gone.go"), Line: 3, Text: "row 3"}})
	if !strings.Contains(ansi.Strip(strings.Join(resultBlock(t, m), "\n")), "preview unavailable") {
		t.Fatal("a deleted target should render the unavailable notice")
	}
}

// TestNarrowOverlayDropsPreview keeps the single-column layout on terminals
// too narrow to split.
func TestNarrowOverlayDropsPreview(t *testing.T) {
	if _, previewW := codepreview.Split(codepreview.MinSplitWidth - 1); previewW != 0 {
		t.Fatalf("narrow overlay kept a %d-cell preview", previewW)
	}
	listW, previewW := codepreview.Split(94)
	if previewW == 0 || listW+previewW+3 != 94 {
		t.Fatalf("split of 94 = %d + %d, does not add up", listW, previewW)
	}
}
