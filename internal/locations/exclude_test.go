package locations

// exclude_test.go covers selective apply and the replace preview (#2154):
// exclusion toggles, the include-only removal batches, and the Rewrite hook's
// rendering (struck match + replacement, excluded rows dim with a ✗ marker).

import (
	"strings"
	"testing"

	"ike/internal/theme"
)

func TestToggleExcludedFlipsCurrent(t *testing.T) {
	var l List
	l.Append(items())
	if !l.ToggleExcluded() {
		t.Fatal("toggle must report ok with items present")
	}
	if it, _ := l.Current(); !it.Excluded {
		t.Fatal("current item must be excluded")
	}
	if l.ExcludedCount() != 1 || l.IncludedCount() != 2 {
		t.Fatalf("counts wrong: %d excluded, %d included", l.ExcludedCount(), l.IncludedCount())
	}
	l.ToggleExcluded()
	if l.ExcludedCount() != 0 {
		t.Fatal("second toggle must re-include")
	}
	var empty List
	if empty.ToggleExcluded() {
		t.Fatal("toggle on an empty list must report false")
	}
}

func TestToggleExcludedGroupFlipsWholeFile(t *testing.T) {
	var l List
	l.Append(items()) // a.go ×2, b.go ×1; cursor on a.go:1
	l.ToggleExcludedGroup()
	if l.ExcludedCount() != 2 {
		t.Fatalf("both a.go items must be excluded, got %d", l.ExcludedCount())
	}
	// A partially included group excludes the rest; a fully excluded one
	// re-includes everything.
	l.ToggleExcluded() // re-include a.go:1 → mixed
	l.ToggleExcludedGroup()
	if l.ExcludedCount() != 2 {
		t.Fatalf("mixed group must become fully excluded, got %d", l.ExcludedCount())
	}
	l.ToggleExcludedGroup()
	if l.ExcludedCount() != 0 {
		t.Fatal("fully excluded group must re-include")
	}
}

func TestRemoveIncludedKeepsExcludedRows(t *testing.T) {
	var l List
	l.Append(items())
	l.SetCursor(1)
	l.ToggleExcluded() // a.go:9 stays behind
	out := l.RemoveIncluded()
	if len(out) != 2 || out[0].Line != 1 || out[1].Path != "b.go" {
		t.Fatalf("batch must hold the included items in order: %+v", out)
	}
	if l.Total() != 1 || l.Files() != 1 {
		t.Fatalf("excluded row must stay listed: %d in %d files", l.Total(), l.Files())
	}
	if it, _ := l.Current(); it.Line != 9 || !it.Excluded {
		t.Fatalf("cursor must land on the surviving row, got %+v", it)
	}
}

func TestRemoveIncludedGroupLeavesOtherFiles(t *testing.T) {
	var l List
	l.Append(items()) // cursor on a.go:1
	out := l.RemoveIncludedGroup()
	if len(out) != 2 || out[0].Path != "a.go" || out[1].Path != "a.go" {
		t.Fatalf("batch must hold the cursor file's items: %+v", out)
	}
	if l.Total() != 1 || l.Files() != 1 {
		t.Fatalf("other files must survive: %d in %d files", l.Total(), l.Files())
	}
	if it, _ := l.Current(); it.Path != "b.go" {
		t.Fatalf("cursor must move to the survivor, got %+v", it)
	}
}

func TestRenderPreviewShowsReplacement(t *testing.T) {
	var l List
	l.Append(items())
	l.Rewrite = func(it Item, r Range) (string, bool) { return "REPL", true }
	out := l.Render(60, 10, theme.DefaultPalette(), nil)
	if !strings.Contains(out, "REPL") {
		t.Fatalf("preview text missing:\n%s", out)
	}
	if !strings.Contains(out, "foo") {
		t.Fatalf("matched text must still render (struck through):\n%s", out)
	}
}

func TestRenderExcludedRowIsMarkedAndSkipsPreview(t *testing.T) {
	var l List
	l.Append(items())
	l.ToggleExcluded() // exclude a.go:1 (the cursor row)
	l.Rewrite = func(it Item, r Range) (string, bool) { return "REPL", true }
	out := l.Render(60, 10, theme.DefaultPalette(), nil)
	if !strings.Contains(out, "✗") {
		t.Fatalf("excluded row must carry the ✗ marker:\n%s", out)
	}
	// Only the two included rows preview; the excluded one keeps its text.
	if got := strings.Count(out, "REPL"); got != 2 {
		t.Fatalf("excluded row must not preview: %d previews\n%s", got, out)
	}
}
