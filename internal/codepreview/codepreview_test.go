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
	return writeFile(t, "sample.txt", lines(n, func(i int) string { return "line " + strconv.Itoa(i) }))
}

// lines builds n lines from a generator.
func lines(n int, gen func(i int) string) []string {
	out := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, gen(i))
	}
	return out
}

// writeFile writes rows as a file named name in a fresh temp dir.
func writeFile(t *testing.T, name string, rows []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(strings.Join(rows, "\n")+"\n"), 0o600); err != nil {
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

// at renders the preview of one target and returns its stripped rows.
func at(c *Cache, path string, line, width, height int) []string {
	return plain(c.Render(Target{Path: path, Line: line}, width, height, nil))
}

// TestRenderCentersTarget checks the excerpt is centered on the target line
// and always fills exactly the requested height (#2047).
func TestRenderCentersTarget(t *testing.T) {
	path := writeLines(t, 100)
	var c Cache
	rows := at(&c, path, 50, 40, 11)
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
	rows := at(&c, path, 2, 40, 11)
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
	first := at(&c, path, 10, 40, 11)
	second := at(&c, path, 60, 40, 11)
	if strings.Join(first, "\n") == strings.Join(second, "\n") {
		t.Fatal("preview did not follow the cursor")
	}
	if !strings.HasSuffix(second[5], "line 60") {
		t.Fatalf("row 5 = %q, want line 60", second[5])
	}
	// Re-rendering the first target must reproduce it exactly (cache hit).
	if again := at(&c, path, 10, 40, 11); strings.Join(again, "\n") != strings.Join(first, "\n") {
		t.Fatal("re-render of the same target differs")
	}
}

// TestRenderMissingFile covers the acceptance criterion that deleted or
// unreadable targets degrade to a notice instead of crashing.
func TestRenderMissingFile(t *testing.T) {
	var c Cache
	rows := at(&c, filepath.Join(t.TempDir(), "gone.txt"), 3, 40, 11)
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
	if rows := at(&c, t.TempDir(), 1, 40, 5); rows[0] != Unavailable {
		t.Fatalf("directory row 0 = %q, want %q", rows[0], Unavailable)
	}
	var empty Cache
	for i, r := range at(&empty, "", 1, 40, 5) {
		if r != "" {
			t.Fatalf("empty path row %d = %q, want blank", i, r)
		}
	}
}

// TestRenderFitsWidth guards the hard clipping: no rendered row may exceed
// the column width, or the two-column popup layout breaks.
func TestRenderFitsWidth(t *testing.T) {
	path := writeFile(t, "long.txt", []string{strings.Repeat("x", 400)})
	var c Cache
	for i, r := range c.Render(Target{Path: path, Line: 1}, 30, 4, nil) {
		if w := ansi.StringWidth(r); w > 30 {
			t.Fatalf("row %d width %d > 30", i, w)
		}
	}
}
