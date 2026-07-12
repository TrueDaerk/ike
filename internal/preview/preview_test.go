package preview

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/theme"
)

const doc = `# Alpha

intro text

## Beta

middle text

## Gamma

closing text
`

// newSized returns a preview with a computed size, ready to render.
func newSized() Model {
	m := New("preview", "doc.md", theme.DefaultPalette())
	m.SetSize(60, 10)
	return m
}

func TestSetSourceImmediateRenders(t *testing.T) {
	m := newSized()
	m.SetSourceImmediate(doc)
	v := m.View()
	if !strings.Contains(v, "Alpha") {
		t.Fatalf("rendered view should contain the heading text, got:\n%s", v)
	}
	if got := strings.Count(v, "\n"); got != 9 {
		t.Fatalf("view must fill exactly the pane height: %d newlines, want 9", got)
	}
}

func TestAnchorsMapHeadingsInOrder(t *testing.T) {
	m := newSized()
	m.SetSourceImmediate(doc)
	if len(m.anchors) != 3 {
		t.Fatalf("anchors = %d, want 3", len(m.anchors))
	}
	prev := -1
	for _, a := range m.anchors {
		if a.rendered < 0 {
			t.Fatalf("heading at source line %d not located in the rendered output", a.src)
		}
		if a.rendered <= prev {
			t.Fatalf("anchors must be strictly ordered, got rendered lines %v", m.anchors)
		}
		prev = a.rendered
	}
}

func TestFencedHeadingIsNotAnchored(t *testing.T) {
	m := newSized()
	m.SetSourceImmediate("# Real\n\n```\n# fake heading in code\n```\n")
	if len(m.anchors) != 1 {
		t.Fatalf("anchors = %d, want 1 (fenced heading excluded)", len(m.anchors))
	}
}

func TestDebounceDropsStaleTick(t *testing.T) {
	m := newSized()
	m.SetSourceImmediate("# One\n")
	if cmd := m.SetSource("# Two\n"); cmd == nil {
		t.Fatal("SetSource must arm the debounce tick")
	}
	staleSeq := m.seq
	_ = m.SetSource("# Three\n")
	// The orphaned first tick must not render the middle revision.
	m.Update(RenderTickMsg{Key: m.key, Seq: staleSeq})
	if v := m.View(); strings.Contains(v, "Two") {
		t.Fatal("stale tick rendered an outdated source revision")
	}
	// The newest tick renders the latest revision.
	m.Update(RenderTickMsg{Key: m.key, Seq: m.seq})
	if v := m.View(); !strings.Contains(v, "Three") {
		t.Fatalf("latest tick should render the newest source, got:\n%s", m.View())
	}
}

func TestCursorFollowScrolls(t *testing.T) {
	m := newSized()
	var b strings.Builder
	b.WriteString("# Top\n\n")
	for i := 0; i < 40; i++ {
		b.WriteString("filler line\n")
	}
	b.WriteString("\n# Bottom\n\nend\n")
	src := b.String()
	m.SetSourceImmediate(src)
	m.SetCursorLine(0)
	if m.top != 0 {
		t.Fatalf("cursor at top should scroll to 0, got %d", m.top)
	}
	m.SetCursorLine(strings.Count(src, "\n") - 1)
	if m.top == 0 {
		t.Fatal("cursor at the bottom heading should scroll the view down")
	}
}

func TestScrollKeysClamp(t *testing.T) {
	m := newSized()
	m.SetSourceImmediate(strings.Repeat("line\n\n", 30))
	m.SetFocused(true)
	m.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	if m.top != m.maxTop() {
		t.Fatalf("G should scroll to the end: top=%d maxTop=%d", m.top, m.maxTop())
	}
	m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.top != m.maxTop() {
		t.Fatal("scrolling past the end must clamp")
	}
	m.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	if m.top != 0 {
		t.Fatalf("g should scroll to the top, got %d", m.top)
	}
	m.ScrollBy(-1)
	if m.top != 0 {
		t.Fatal("scrolling above the top must clamp")
	}
}

func TestResizeRerenders(t *testing.T) {
	m := New("preview", "doc.md", theme.DefaultPalette())
	m.SetSize(60, 10)
	m.SetSourceImmediate(doc)
	wide := len(m.lines)
	m.SetSize(24, 10)
	if len(m.lines) == 0 {
		t.Fatal("resize must re-render")
	}
	if len(m.lines) < wide {
		t.Fatalf("narrower width should wrap to at least as many lines: %d -> %d", wide, len(m.lines))
	}
}
