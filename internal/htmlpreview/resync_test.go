package htmlpreview

import (
	"strings"
	"testing"
)

// TestResync: a preview shown again (#2766, an editor tab back in Preview
// view) keeps its page and scroll when nothing changed, re-renders a changed
// text or a debounce tick it never received, and follows a moved caret.
func TestResync(t *testing.T) {
	src := longDoc(80)
	m := sized(t, src)
	m.ScrollBy(25)
	top := m.Top()
	m.Resync(src, 0)
	if m.Pending() || m.Top() != top {
		t.Fatalf("an unchanged source must keep the page at top %d (got %d, pending %v)", top, m.Top(), m.Pending())
	}
	// A debounced edit whose tick never reached the hidden pane.
	edited := strings.Replace(src, "para000", "edited", 1)
	_ = m.SetSource(edited)
	m.Resync(edited, 0)
	if !m.Pending() {
		t.Fatal("a source whose tick was missed must owe a render")
	}
	m.Flush()
	if !strings.Contains(strings.Join(m.Lines(), "\n"), "edited") {
		t.Fatal("the render must show the edited text")
	}
	// A changed text arriving directly.
	m.Resync(src, 0)
	if !m.Pending() {
		t.Fatal("a changed text must owe a render")
	}
	m.Flush()
	// A moved caret re-syncs the scroll.
	m.Resync(src, 60)
	if m.Pending() || m.Top() == 0 {
		t.Fatalf("a moved caret must scroll the page (top %d)", m.Top())
	}
}
