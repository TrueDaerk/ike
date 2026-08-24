package codepreview

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// writeLines writes a file whose n-th line reads "line <n>".
func writeLines(t *testing.T, n int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sample.txt")
	var b strings.Builder
	for i := 1; i <= n; i++ {
		b.WriteString("line " + strconv.Itoa(i) + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// plain strips styling so assertions compare the text, not the SGR sequences.
func plain(rows []string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = strings.TrimRight(ansi.Strip(r), " ")
	}
	return out
}

// TestRenderCentersTarget checks the excerpt is centered on the target line
// and always fills exactly the requested height (#2047).
func TestRenderCentersTarget(t *testing.T) {
	path := writeLines(t, 100)
	var c Cache
	rows := plain(c.Render(path, 50, 40, 11, nil))
	if len(rows) != 11 {
		t.Fatalf("got %d rows, want 11", len(rows))
	}
	// height 11 centers the target on the sixth row (index 5).
	if !strings.HasSuffix(rows[5], "line 50") {
		t.Fatalf("row 5 = %q, want the target line", rows[5])
	}
	if !strings.HasSuffix(rows[0], "line 45") {
		t.Fatalf("row 0 = %q, want line 45", rows[0])
	}
	if !strings.HasSuffix(rows[10], "line 55") {
		t.Fatalf("row 10 = %q, want line 55", rows[10])
	}
}

// TestRenderNearFileStartAndEnd guards the clamping: a match near the top
// shows the file's head, and a short tail pads with blank rows instead of
// shrinking the block.
func TestRenderNearFileStartAndEnd(t *testing.T) {
	path := writeLines(t, 8)
	var c Cache
	rows := plain(c.Render(path, 2, 40, 11, nil))
	if len(rows) != 11 {
		t.Fatalf("got %d rows, want 11", len(rows))
	}
	if !strings.HasSuffix(rows[0], "line 1") {
		t.Fatalf("row 0 = %q, want the file head", rows[0])
	}
	if rows[8] != "" || rows[10] != "" {
		t.Fatalf("tail rows %q/%q, want blank padding", rows[8], rows[10])
	}
}

// TestRenderFollowsCursor is the preview-update criterion: rendering a
// different target line yields a different excerpt (#2047).
func TestRenderFollowsCursor(t *testing.T) {
	path := writeLines(t, 100)
	var c Cache
	first := plain(c.Render(path, 10, 40, 11, nil))
	second := plain(c.Render(path, 60, 40, 11, nil))
	if strings.Join(first, "\n") == strings.Join(second, "\n") {
		t.Fatal("preview did not follow the cursor")
	}
	if !strings.HasSuffix(second[5], "line 60") {
		t.Fatalf("row 5 = %q, want line 60", second[5])
	}
	// Re-rendering the first target must reproduce it exactly (cache hit).
	if again := plain(c.Render(path, 10, 40, 11, nil)); strings.Join(again, "\n") != strings.Join(first, "\n") {
		t.Fatal("re-render of the same target differs")
	}
}

// TestRenderMissingFile covers the acceptance criterion that deleted or
// unreadable targets degrade to a notice instead of crashing.
func TestRenderMissingFile(t *testing.T) {
	var c Cache
	rows := plain(c.Render(filepath.Join(t.TempDir(), "gone.txt"), 3, 40, 11, nil))
	if len(rows) != 11 {
		t.Fatalf("got %d rows, want 11", len(rows))
	}
	if rows[0] != Unavailable {
		t.Fatalf("row 0 = %q, want %q", rows[0], Unavailable)
	}
}

// TestRenderDirectoryAndEmptyPath keeps the other degenerate targets silent.
func TestRenderDirectoryAndEmptyPath(t *testing.T) {
	var c Cache
	if rows := plain(c.Render(t.TempDir(), 1, 40, 5, nil)); rows[0] != Unavailable {
		t.Fatalf("directory row 0 = %q, want %q", rows[0], Unavailable)
	}
	var empty Cache
	for i, r := range plain(empty.Render("", 1, 40, 5, nil)) {
		if r != "" {
			t.Fatalf("empty path row %d = %q, want blank", i, r)
		}
	}
}

// TestRenderFitsWidth guards the hard truncation: no rendered row may exceed
// the column width, or the two-column popup layout breaks.
func TestRenderFitsWidth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "long.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 400)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var c Cache
	for i, r := range c.Render(path, 1, 30, 4, nil) {
		if w := ansi.StringWidth(r); w > 30 {
			t.Fatalf("row %d width %d > 30", i, w)
		}
	}
}
