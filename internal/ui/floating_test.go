package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// stubContent is a fixed-body Content used to drive the shell in tests. body is
// rendered verbatim, ignoring the width budget unless wide is set, in which case
// it returns lastWidth markers so width-plumbing can be asserted.
type stubContent struct {
	heading   string
	body      string
	lastWidth int
}

func (s *stubContent) Title() string { return s.heading }
func (s *stubContent) Render(width int) string {
	s.lastWidth = width
	return s.body
}

func TestFloatingOpenCloseAndView(t *testing.T) {
	f := New(Config{})
	c := &stubContent{heading: "TITLE", body: "hello world"}
	f.SetContent(c)
	f.SetSize(80, 24)

	if f.IsOpen() {
		t.Fatal("shell should start closed")
	}
	if f.View() != "" {
		t.Fatal("closed shell should render empty")
	}
	f.Open()
	if !f.IsOpen() {
		t.Fatal("shell should be open after Open")
	}
	v := f.View()
	if !strings.Contains(v, "TITLE") || !strings.Contains(v, "hello world") {
		t.Fatalf("view missing title or body: %q", v)
	}
	// Default dismiss key is esc; the hint reflects it.
	if !strings.Contains(v, "esc to close") {
		t.Fatalf("view missing default dismiss hint: %q", v)
	}
}

func TestFloatingDefaultDismissAndSwallow(t *testing.T) {
	f := New(Config{})
	f.SetContent(&stubContent{heading: "T", body: "b"})
	f.SetSize(80, 24)
	f.Open()

	// A non-dismiss key is swallowed (consumed) but does not close.
	if !f.Update(tea.KeyMsg{Type: tea.KeyTab}) {
		t.Fatal("open shell should consume all keys")
	}
	if !f.IsOpen() {
		t.Fatal("non-dismiss key should not close the shell")
	}
	// esc dismisses.
	if !f.Update(tea.KeyMsg{Type: tea.KeyEsc}) {
		t.Fatal("esc should be consumed")
	}
	if f.IsOpen() {
		t.Fatal("esc should dismiss the shell")
	}
}

func TestFloatingConfigurableDismissKeys(t *testing.T) {
	f := New(Config{DismissKeys: []string{"esc", "q"}})
	f.SetContent(&stubContent{heading: "T", body: "b"})
	f.SetSize(80, 24)

	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("q")},
		{Type: tea.KeyEsc},
	} {
		f.Open()
		f.Update(key)
		if f.IsOpen() {
			t.Fatalf("key %v should dismiss", key)
		}
	}
	// esc/q hint is shown.
	f.Open()
	if !strings.Contains(f.View(), "esc/q to close") {
		t.Fatalf("hint should list configured keys: %q", f.View())
	}
}

func TestFloatingUpdateIgnoredWhenClosed(t *testing.T) {
	f := New(Config{})
	if f.Update(tea.KeyMsg{Type: tea.KeyEsc}) {
		t.Fatal("closed shell should not consume keys")
	}
}

func TestFloatingFitsWithinTerminal(t *testing.T) {
	f := New(Config{})
	f.SetContent(&stubContent{heading: "HELP", body: "a short body"})
	f.SetSize(80, 24)
	f.Open()
	v := f.View()
	if w, h := lipgloss.Width(v), lipgloss.Height(v); w > 80 || h > 24 {
		t.Fatalf("pane %dx%d overflows 80x24 terminal", w, h)
	}
}

func TestFloatingContentWidthBudget(t *testing.T) {
	c := &stubContent{heading: "T", body: "x"}
	f := New(Config{})
	f.SetContent(c)
	f.SetSize(80, 24)
	f.Open()
	// 80 - 2*margin(2) - frameH(8) = 68.
	if c.lastWidth != 80-2*defaultMargin-frameH {
		t.Fatalf("content width budget = %d, want %d", c.lastWidth, 80-2*defaultMargin-frameH)
	}
}

func TestFloatingMaxWidthFractionClamps(t *testing.T) {
	c := &stubContent{heading: "T", body: "x"}
	f := New(Config{MaxWidthFrac: 0.5})
	f.SetContent(c)
	f.SetSize(100, 40)
	f.Open()
	// Clamp: int(100*0.5) - frameH = 50 - 8 = 42, tighter than the margin budget
	// (100 - 4 - 8 = 88), so the fraction wins.
	if c.lastWidth != 42 {
		t.Fatalf("clamped content width = %d, want 42", c.lastWidth)
	}
}

func TestFloatingScrollsOverflowingContent(t *testing.T) {
	tall := strings.TrimRight(strings.Repeat("line\n", 200), "\n")
	c := &stubContent{heading: "T", body: tall}
	f := New(Config{})
	f.SetContent(c)
	f.SetSize(80, 24)
	f.Open()
	// Overflowing content scrolls rather than expanding the pane past the
	// terminal; the position indicator appears.
	if h := lipgloss.Height(f.View()); h > 24 {
		t.Fatalf("overflowing pane height %d should stay within 24", h)
	}
	if !f.scroll.scrollable() {
		t.Fatal("tall content should be scrollable")
	}
}

func TestModelContentAdapter(t *testing.T) {
	mc := ModelContent{Heading: "PANE", Body: func() string { return "rendered" }}
	if mc.Title() != "PANE" {
		t.Fatalf("title = %q", mc.Title())
	}
	if mc.Render(10) != "rendered" {
		t.Fatalf("render = %q", mc.Render(10))
	}
	// Nil body degrades to empty, never panics.
	if (ModelContent{}).Render(5) != "" {
		t.Fatal("nil body should render empty")
	}
}
