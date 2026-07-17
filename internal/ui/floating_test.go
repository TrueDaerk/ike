package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
	// The underlined title styles every rune, so match on stripped text.
	v := ansi.Strip(f.View())
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
	if !f.Update(tea.KeyPressMsg{Code: tea.KeyTab}) {
		t.Fatal("open shell should consume all keys")
	}
	if !f.IsOpen() {
		t.Fatal("non-dismiss key should not close the shell")
	}
	// esc dismisses.
	if !f.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) {
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

	for _, key := range []tea.KeyPressMsg{
		{Text: "q", Code: 'q'},
		{Code: tea.KeyEscape},
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
	if f.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) {
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

func TestFloatingViewReflectsInPlaceContentChange(t *testing.T) {
	// #409: content that mutates its state after opening (a modal moving its
	// cursor) must show the change on the very next View, without the host
	// forcing a relayout via SetSize/SetContent.
	body := "cursor at 0"
	f := New(Config{})
	f.SetContent(ModelContent{Heading: "T", Body: func() string { return body }})
	f.SetSize(80, 24)
	f.Open()
	if v := ansi.Strip(f.View()); !strings.Contains(v, "cursor at 0") {
		t.Fatalf("initial body missing: %q", v)
	}
	body = "cursor at 1"
	if v := ansi.Strip(f.View()); !strings.Contains(v, "cursor at 1") {
		t.Fatalf("view served stale body after in-place change: %q", v)
	}
}

func TestFloatingViewRefreshKeepsScrollOffset(t *testing.T) {
	// Re-rendering the body every View must not reset an active scroll.
	tall := strings.TrimRight(strings.Repeat("line\n", 200), "\n")
	f := New(Config{})
	f.SetContent(&stubContent{heading: "T", body: tall})
	f.SetSize(80, 24)
	f.Open()
	f.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	off := f.scroll.vp.YOffset()
	if off == 0 {
		t.Fatal("setup: scroll should have moved")
	}
	_ = f.View()
	if got := f.scroll.vp.YOffset(); got != off {
		t.Fatalf("View reset scroll offset: got %d, want %d", got, off)
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

// keyedContent is a KeyHandler stub consuming a configurable key set and
// recording every key offered to it.
type keyedContent struct {
	ModelContent
	consume map[string]bool
	seen    []string
}

func (k *keyedContent) HandleKey(key string) bool {
	k.seen = append(k.seen, key)
	return k.consume[key]
}

func TestKeyHandlerConsumeAndFallThrough(t *testing.T) {
	tall := strings.TrimRight(strings.Repeat("line\n", 200), "\n")
	kc := &keyedContent{
		ModelContent: ModelContent{Heading: "T", Body: func() string { return tall }},
		consume:      map[string]bool{"tab": true},
	}
	f := New(Config{})
	f.SetContent(kc)
	f.SetSize(80, 24)
	f.Open()

	// tab is consumed by the content; the shell stays open, scroll untouched.
	if !f.Update(tea.KeyPressMsg{Code: tea.KeyTab}) {
		t.Fatal("open shell should consume tab")
	}
	if len(kc.seen) != 1 || kc.seen[0] != "tab" {
		t.Fatalf("content should have been offered tab, saw %v", kc.seen)
	}
	if off := f.scroll.vp.YOffset(); off != 0 {
		t.Fatalf("consumed key must not scroll, offset = %d", off)
	}

	// An unconsumed key falls through to the scroller.
	f.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if kc.seen[len(kc.seen)-1] != "down" {
		t.Fatalf("content should see unconsumed keys first, saw %v", kc.seen)
	}
	if off := f.scroll.vp.YOffset(); off == 0 {
		t.Fatal("unconsumed key should fall through to scroll")
	}

	// Dismiss keys never reach the content.
	f.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if f.IsOpen() {
		t.Fatal("esc should still dismiss with a KeyHandler installed")
	}
	for _, k := range kc.seen {
		if k == "esc" {
			t.Fatal("dismiss key must not be offered to the content")
		}
	}
}

// filterKeyedContent is both Filterable and a KeyHandler, like the help pane.
type filterKeyedContent struct {
	filterableContent
	consume map[string]bool
	seen    []string
}

func (c *filterKeyedContent) HandleKey(key string) bool {
	c.seen = append(c.seen, key)
	return c.consume[key]
}

func TestKeyHandlerAfterFilter(t *testing.T) {
	// Printable keys feed the filter and are never offered as handler keys;
	// tab still reaches HandleKey.
	c := &filterKeyedContent{
		filterableContent: filterableContent{ModelContent: ModelContent{Heading: "T", Body: func() string { return "b" }}},
		consume:           map[string]bool{"tab": true},
	}
	f := New(Config{})
	f.SetContent(c)
	f.SetSize(80, 24)
	f.Open()

	f.Update(tea.KeyPressMsg{Text: "a", Code: 'a'})
	if c.filter != "a" {
		t.Fatalf("printable key should feed the filter, filter = %q", c.filter)
	}
	if len(c.seen) != 0 {
		t.Fatalf("filtered key must not reach HandleKey, saw %v", c.seen)
	}
	if !f.Update(tea.KeyPressMsg{Code: tea.KeyTab}) || len(c.seen) != 1 || c.seen[0] != "tab" {
		t.Fatalf("tab should reach HandleKey even while filtering, saw %v", c.seen)
	}
}

// filterableContent is a Filterable stub recording its filter.
type filterableContent struct {
	ModelContent
	filter string
}

func (f *filterableContent) SetFilter(s string) { f.filter = s }
func (f *filterableContent) Filter() string     { return f.filter }

func TestFilterableTypingAndDismissRules(t *testing.T) {
	f := New(Config{DismissKeys: []string{"esc", "q"}})
	fc := &filterableContent{ModelContent: ModelContent{Heading: "T", Body: func() string { return "b" }}}
	f.SetSize(80, 24)
	f.SetContent(fc)
	f.Open()

	key := func(s string) tea.Msg { return tea.KeyPressMsg{Text: s, Code: rune(s[0])} }

	// Typing builds the filter; q with an active filter is a letter.
	f.Update(key("s"))
	f.Update(key("q"))
	if fc.filter != "sq" {
		t.Fatalf("filter = %q, want sq (q is a letter mid-filter)", fc.filter)
	}

	// Backspace shortens; esc clears the filter but keeps the shell open.
	f.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if fc.filter != "s" {
		t.Fatalf("backspace: filter = %q, want s", fc.filter)
	}
	f.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if fc.filter != "" || !f.IsOpen() {
		t.Fatalf("esc must clear the filter and keep the shell open (filter=%q open=%v)", fc.filter, f.IsOpen())
	}

	// With an empty filter, q dismisses; esc would too.
	f.Update(key("q"))
	if f.IsOpen() {
		t.Fatal("q on an empty filter must dismiss the shell")
	}
}
