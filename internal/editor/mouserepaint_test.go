package editor

import (
	"strings"
	"testing"
)

// mouseModel is a focused, sized buffer with a warm render cache when warm is
// true — the state a long-running pane is in.
func mouseModel(t *testing.T, warm bool) Model {
	t.Helper()
	m := New()
	m.RestoreText(strings.Repeat("some line of code here = value(x, y)\n", 40))
	m.SetSize(60, 20)
	m.SetFocused(true)
	if warm {
		_ = m.View() // fill the line cache (#614) at the current caret state
	}
	return m
}

// TestMouseClickRepaintsCaret is the regression test for #1327: the render
// caches were keyed on the render epoch alone, which only Update bumps, so a
// mouse-driven caret move left the cached line bodies in place and the view
// kept drawing the caret at its old position. A warm render must equal a cold
// one for the same state.
func TestMouseClickRepaintsCaret(t *testing.T) {
	warm := mouseModel(t, true)
	before := warm.View()
	warm.MouseClick(10, 5)

	cold := mouseModel(t, false)
	cold.MouseClick(10, 5)

	got, want := warm.View(), cold.View()
	if got != want {
		t.Fatalf("a warm cache served a stale frame after a click\n--- cached ---\n%s\n--- fresh ---\n%s", got, want)
	}
	if got == before {
		t.Fatal("the click must change what the view draws (the caret moved)")
	}
	if warm.RenderVersion() == mouseModel(t, false).RenderVersion() {
		t.Fatal("RenderVersion must change with the caret, or the pane cache reuses its frame")
	}
}

// TestMouseDragRepaintsSelection is the same guarantee for a drag: the
// selection must be highlighted while it grows, not only be logically there.
func TestMouseDragRepaintsSelection(t *testing.T) {
	warm := mouseModel(t, true)
	warm.MouseClick(10, 5)
	_ = warm.View() // the caret move alone is now on screen
	warm.MouseDrag(24, 7)

	cold := mouseModel(t, false)
	cold.MouseClick(10, 5)
	cold.MouseDrag(24, 7)

	if !warm.ModeName().IsVisual() {
		t.Fatal("precondition: the drag must have started a visual selection")
	}
	if got, want := warm.View(), cold.View(); got != want {
		t.Fatalf("a warm cache served a stale frame during a drag\n--- cached ---\n%s\n--- fresh ---\n%s", got, want)
	}
}

// TestAltClickRepaintsCarets covers the secondary-caret entry point (#145),
// which mutates through the same mouse path.
func TestAltClickRepaintsCarets(t *testing.T) {
	warm := mouseModel(t, true)
	warm.AltClick(12, 6)

	cold := mouseModel(t, false)
	cold.AltClick(12, 6)

	if got, want := warm.View(), cold.View(); got != want {
		t.Fatalf("a warm cache served a stale frame after alt+click\n--- cached ---\n%s\n--- fresh ---\n%s", got, want)
	}
}

// TestScrollKeepsTheLineCacheWarm guards the 0400 gain the fix must not undo
// (#614): a vertical scroll changes no line body, so the cache survives it.
func TestScrollKeepsTheLineCacheWarm(t *testing.T) {
	m := mouseModel(t, true)
	entries := len(m.lineCache.entries)
	if entries == 0 {
		t.Fatal("precondition: the first render must populate the cache")
	}
	m.ScrollBy(6)
	_ = m.View()
	if m.lineCache.entries[lineKey{line: 0, from: 0, to: -1, width: m.view.TextWidth(m.buf.LineCount())}] == "" {
		t.Fatal("a scroll must not drop the cached bodies of lines it scrolled past")
	}
}
