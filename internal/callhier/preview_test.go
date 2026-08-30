package callhier

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/codepreview"
	ilsp "ike/internal/lsp"
)

// preview_test.go covers the call-hierarchy overlay's code column (#2053):
// the excerpt of the selected entry's file beside the tree, and its behaviour
// on cursor moves and unreadable targets.

// sampleFile writes a file whose n-th line reads "row <n>".
func sampleFile(t *testing.T, n int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "calls.go")
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "row %d\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// previewColumn returns the text right of the vertical rule, asserting the
// rule sits in the same column on every content row.
func previewColumn(t *testing.T, m *Model) string {
	t.Helper()
	innerW := m.width - 12 - 4
	listW, previewW := m.previewSplit(innerW)
	if previewW <= 0 {
		t.Fatalf("no preview column at width %d", m.width)
	}
	var out []string
	for _, row := range strings.Split(ansi.Strip(m.body(innerW, m.theme())), "\n") {
		runes := []rune(row)
		if len(runes) <= listW+1 {
			t.Fatalf("row %q is shorter than the rule column %d", row, listW+1)
		}
		if runes[listW+1] != '│' {
			t.Fatalf("row %q has no rule at column %d", row, listW+1)
		}
		out = append(out, strings.TrimRight(string(runes[listW+3:]), " "))
	}
	return strings.Join(out, "\n")
}

// openPreviewModel opens the overlay on two roots in one file, both expanded
// to leaves so the cursor can walk them.
func openPreviewModel(t *testing.T) (*Model, string) {
	t.Helper()
	path := sampleFile(t, 60)
	m := New()
	m.SetSize(160, 50)
	rec := &fetchRecorder{}
	m.Open(ilsp.CallHierarchyMsg{
		Path: path,
		Roots: []ilsp.CallHierarchyEntry{
			entry("First", path, 9),   // line 10, 1-based
			entry("Second", path, 39), // line 40
		},
		Fetch: rec.fetch,
	})
	m.Apply(ilsp.CallHierarchyCallsMsg{ReqID: 1, Incoming: true})
	return m, path
}

// TestCallHierarchyPreviewShowsSelection: the column shows the selected
// entry's file around its line.
func TestCallHierarchyPreviewShowsSelection(t *testing.T) {
	m, _ := openPreviewModel(t)
	if col := previewColumn(t, m); !strings.Contains(col, "row 10") {
		t.Fatalf("preview column = %q, want the first root's line", col)
	}
}

// TestCallHierarchyPreviewFollowsCursor is the acceptance criterion that the
// excerpt tracks the cursor.
func TestCallHierarchyPreviewFollowsCursor(t *testing.T) {
	m, _ := openPreviewModel(t)
	first := previewColumn(t, m)
	m.Update(key("down"))
	second := previewColumn(t, m)
	if first == second {
		t.Fatal("the preview did not follow the cursor")
	}
	if !strings.Contains(second, "row 40") {
		t.Fatalf("preview column = %q, want the second root's line", second)
	}
}

// TestCallHierarchyPreviewMissingFile: an unreadable target degrades to the
// dim notice instead of crashing the frame.
func TestCallHierarchyPreviewMissingFile(t *testing.T) {
	m := New()
	m.SetSize(160, 50)
	rec := &fetchRecorder{}
	m.Open(ilsp.CallHierarchyMsg{
		Path:  "/no/such/file.go",
		Roots: []ilsp.CallHierarchyEntry{entry("Gone", "/no/such/file.go", 3)},
		Fetch: rec.fetch,
	})
	if col := previewColumn(t, m); !strings.Contains(col, codepreview.Unavailable) {
		t.Fatalf("preview column = %q, want the %q notice", col, codepreview.Unavailable)
	}
}

// TestCallHierarchyNarrowBoxKeepsOneColumn: a terminal too narrow to split
// renders the tree alone, with no stray rule (the box's own border aside).
func TestCallHierarchyNarrowBoxKeepsOneColumn(t *testing.T) {
	m, _ := openPreviewModel(t)
	m.SetSize(50, 40)
	body := ansi.Strip(m.body(40-4, m.theme()))
	if strings.Contains(body, "│") {
		t.Fatalf("a narrow overlay must not render the preview rule: %q", body)
	}
	if !strings.Contains(body, "First") {
		t.Fatalf("the tree is missing from the narrow body: %q", body)
	}
}

// focusKey is the chord that hands the excerpt column the keyboard (#2327).
func focusKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'p', Mod: tea.ModAlt} }

// TestCallHierarchyPreviewScrolls is the scroll criterion in this overlay:
// focused, the editor motions walk the excerpt while the tree cursor stays
// put, and esc gives the tree the keyboard back instead of closing.
func TestCallHierarchyPreviewScrolls(t *testing.T) {
	m, _ := openPreviewModel(t)
	before := previewColumn(t, m)
	m.Update(focusKey())
	if !m.prev.Focused() {
		t.Fatal("alt+p must focus the preview column")
	}
	m.Update(key("j"))
	m.Update(key("j"))
	scrolled := previewColumn(t, m)
	if scrolled == before {
		t.Fatalf("j did not scroll the excerpt:\n%s", scrolled)
	}
	if m.cursor != 0 {
		t.Fatalf("the tree cursor moved to %d while the preview had focus", m.cursor)
	}
	m.Update(key("z"))
	if again := previewColumn(t, m); again != before {
		t.Fatalf("z did not return to the entry:\n%s", again)
	}
	m.Update(key("esc"))
	if m.prev.Focused() || !m.IsOpen() {
		t.Fatal("esc must blur the preview and keep the overlay open")
	}
	m.Update(key("esc"))
	if m.IsOpen() {
		t.Fatal("a second esc must close the overlay")
	}
}
