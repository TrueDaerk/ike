package terminal

// copymode_test.go — copy mode over the VT emulator model (#2162), driven
// through pipe sessions (#1370): deterministic content, no PTY needed.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// copyModel builds a pipe-backed model with numbered lines filling the
// scrollback plus one distinctive word line, and waits until the feed loop
// has them all.
func copyModel(t *testing.T) *Model {
	t.Helper()
	m := NewPipe("copy-test", 40, 6, nil)
	t.Cleanup(m.Close)
	var b strings.Builder
	for i := 1; i <= 20; i++ {
		b.WriteString("line" + pad2(i) + "\n")
	}
	b.WriteString("foo bar baz\n")
	m.FeedText(b.String())
	waitFor(t, "content fed", func() bool {
		return m.sess.ScrollbackLen() > 10 && findVirtual(&m, "foo bar baz") >= 0
	})
	return &m
}

func pad2(i int) string {
	if i < 10 {
		return "0" + string(rune('0'+i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

// findVirtual scans the virtual buffer for the first line containing want.
func findVirtual(m *Model, want string) int {
	for v := 0; v < m.copyTotal(); v++ {
		if strings.Contains(m.sess.LineText(v), want) {
			return v
		}
	}
	return -1
}

func ctrl(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }

// TestCopyModeEnterExit: the chord entry opens the mode with its indicator,
// esc and q leave it, and the view snaps back to live on exit.
func TestCopyModeEnterExit(t *testing.T) {
	m := copyModel(t)
	m.ScrollBy(5)
	if !m.StartCopyMode() {
		t.Fatal("StartCopyMode must open on a plain session")
	}
	if !m.Copying() {
		t.Fatal("Copying must report the open mode")
	}
	if !strings.Contains(m.View(), "COPY") {
		t.Fatal("the view must carry the COPY indicator")
	}
	press(m, tea.KeyEscape, "")
	if m.Copying() || m.Scroll() != 0 {
		t.Fatalf("esc must exit and snap to live, copying=%v scroll=%d", m.Copying(), m.Scroll())
	}
	m.StartCopyMode()
	press(m, 'q', "q")
	if m.Copying() {
		t.Fatal("q must exit copy mode")
	}
}

// TestCopyModeConsumesKeys: while the mode is open no key reaches the
// session — the occupied marker (set on every forwarded key) stays false.
func TestCopyModeConsumesKeys(t *testing.T) {
	m := copyModel(t)
	m.StartCopyMode()
	for _, r := range "xyz-15/q?" {
		press(m, r, string(r))
	}
	press(m, tea.KeyEscape, "")
	press(m, tea.KeyEnter, "")
	if m.occupied {
		t.Fatal("copy-mode keys must never reach the session")
	}
}

// TestCopyModeAltScreenGuard: a live alt-screen child keeps its keys — the
// mode refuses to open, like the scrollback search.
func TestCopyModeAltScreenGuard(t *testing.T) {
	m := copyModel(t)
	m.FeedText("\x1b[?1049h")
	waitFor(t, "alt screen", func() bool { return m.sess.AltScreen() })
	if m.StartCopyMode() {
		t.Fatal("copy mode must stay closed under an alt-screen child")
	}
}

// TestCopyModeMotions: hjkl, 0/$, gg/G and ctrl+u/d move the cursor over the
// virtual buffer, clamped to its bounds.
func TestCopyModeMotions(t *testing.T) {
	m := copyModel(t)
	m.StartCopyMode()
	c := m.copy

	press(m, 'g', "g")
	press(m, 'g', "g")
	if c.cur != (vpos{line: 0, col: 0}) {
		t.Fatalf("gg must go to the buffer top, got %+v", c.cur)
	}
	press(m, 'j', "j")
	press(m, 'j', "j")
	press(m, 'k', "k")
	if c.cur.line != 1 {
		t.Fatalf("jjk must land on line 1, got %d", c.cur.line)
	}
	press(m, 'l', "l")
	press(m, 'l', "l")
	press(m, 'h', "h")
	if c.cur.col != 1 {
		t.Fatalf("llh must land on col 1, got %d", c.cur.col)
	}
	press(m, '$', "$")
	if want := m.copyLineEnd(1); c.cur.col != want {
		t.Fatalf("$ must go to the line end %d, got %d", want, c.cur.col)
	}
	press(m, '0', "0")
	if c.cur.col != 0 {
		t.Fatalf("0 must go to col 0, got %d", c.cur.col)
	}
	press(m, 'G', "G")
	if c.cur.line != m.copyTotal()-1 {
		t.Fatalf("G must go to the last line %d, got %d", m.copyTotal()-1, c.cur.line)
	}
	m.Update(ctrl('u'))
	if want := m.copyTotal() - 1 - m.pageSize(); c.cur.line != want {
		t.Fatalf("ctrl+u must go up half a page to %d, got %d", want, c.cur.line)
	}
	m.Update(ctrl('d'))
	if c.cur.line != m.copyTotal()-1 {
		t.Fatalf("ctrl+d must go back down, got %d", c.cur.line)
	}
	// Clamps: k at the top, j at the bottom stay put.
	press(m, 'j', "j")
	if c.cur.line != m.copyTotal()-1 {
		t.Fatal("j at the last line must clamp")
	}
	press(m, 'g', "g")
	press(m, 'g', "g")
	press(m, 'k', "k")
	if c.cur.line != 0 {
		t.Fatal("k at the first line must clamp")
	}
}

// TestCopyModeWordMotions: w/b step whitespace-separated words, crossing
// line boundaries.
func TestCopyModeWordMotions(t *testing.T) {
	m := copyModel(t)
	m.StartCopyMode()
	c := m.copy
	v := findVirtual(m, "foo bar baz")
	c.cur = vpos{line: v, col: 0}
	press(m, 'w', "w")
	if c.cur != (vpos{line: v, col: 4}) {
		t.Fatalf("w must land on bar (col 4), got %+v", c.cur)
	}
	press(m, 'w', "w")
	if c.cur != (vpos{line: v, col: 8}) {
		t.Fatalf("w must land on baz (col 8), got %+v", c.cur)
	}
	press(m, 'b', "b")
	if c.cur != (vpos{line: v, col: 4}) {
		t.Fatalf("b must land back on bar, got %+v", c.cur)
	}
	press(m, 'b', "b")
	press(m, 'b', "b")
	// Off the line start, b crosses to the previous line's last word.
	if c.cur.line != v-1 {
		t.Fatalf("b past the line start must cross to line %d, got %+v", v-1, c.cur)
	}
	c.cur = vpos{line: v - 1, col: m.copyLineEnd(v - 1)}
	press(m, 'w', "w")
	if c.cur != (vpos{line: v, col: 0}) {
		t.Fatalf("w past the line end must cross to the next line's first word, got %+v", c.cur)
	}
}

// TestCopyModeSelectionYank: v anchors, motions extend, y hands the
// inclusive span to the clipboard funnel and exits back to live.
func TestCopyModeSelectionYank(t *testing.T) {
	m := copyModel(t)
	m.ScrollBy(3)
	m.StartCopyMode()
	c := m.copy
	v := findVirtual(m, "foo bar baz")
	c.cur = vpos{line: v, col: 4}
	press(m, 'v', "v")
	if !m.HasSelection() {
		t.Fatal("v must anchor a selection")
	}
	press(m, 'w', "w")
	press(m, '$', "$")
	cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd == nil {
		t.Fatal("y over a selection must return the yank command")
	}
	msg, ok := cmd().(CopiedMsg)
	if !ok {
		t.Fatalf("yank command must yield CopiedMsg, got %T", cmd())
	}
	if msg.Text != "bar baz" {
		t.Fatalf("yank = %q, want %q", msg.Text, "bar baz")
	}
	if m.Copying() || m.Scroll() != 0 || m.HasSelection() {
		t.Fatalf("y must exit to live without a selection, copying=%v scroll=%d sel=%v",
			m.Copying(), m.Scroll(), m.HasSelection())
	}
}

// TestCopyModeSelectionBackwards: a selection dragged toward older lines
// yanks the same inclusive span.
func TestCopyModeSelectionBackwards(t *testing.T) {
	m := copyModel(t)
	m.StartCopyMode()
	c := m.copy
	v := findVirtual(m, "foo bar baz")
	c.cur = vpos{line: v, col: m.copyLineEnd(v)}
	press(m, 'v', "v")
	press(m, '0', "0")
	cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd == nil {
		t.Fatal("y must yank the backwards selection")
	}
	if msg := cmd().(CopiedMsg); msg.Text != "foo bar baz" {
		t.Fatalf("backwards yank = %q, want %q", msg.Text, "foo bar baz")
	}
}

// TestCopyModeEscCancelsSelection: esc drops an active selection but stays
// in the mode; a second esc leaves it.
func TestCopyModeEscCancelsSelection(t *testing.T) {
	m := copyModel(t)
	m.StartCopyMode()
	press(m, 'v', "v")
	press(m, 'j', "j")
	press(m, tea.KeyEscape, "")
	if !m.Copying() || m.HasSelection() {
		t.Fatalf("esc must only cancel the selection, copying=%v sel=%v", m.Copying(), m.HasSelection())
	}
	press(m, tea.KeyEscape, "")
	if m.Copying() {
		t.Fatal("the second esc must exit the mode")
	}
}

// TestCopyModeSearch: `/` jumps forward to the match's column, n/N step with
// wrap-around, `?` searches backward, and a miss reports `no matches`.
func TestCopyModeSearch(t *testing.T) {
	m := copyModel(t)
	m.StartCopyMode()
	c := m.copy
	press(m, 'g', "g")
	press(m, 'g', "g")

	press(m, '/', "/")
	typeQuery(m, "bar")
	press(m, tea.KeyEnter, "")
	v := findVirtual(m, "foo bar baz")
	if c.cur != (vpos{line: v, col: 4}) {
		t.Fatalf("/bar must land on the match column, got %+v want line %d col 4", c.cur, v)
	}
	// The single match: n wraps back onto it.
	press(m, 'n', "n")
	if c.cur.line != v {
		t.Fatalf("n must wrap to the only match, got line %d", c.cur.line)
	}
	press(m, 'N', "N")
	if c.cur.line != v {
		t.Fatalf("N must wrap to the only match, got line %d", c.cur.line)
	}

	// Several matches: numbered lines share the "line1" prefix.
	press(m, '/', "/")
	typeQuery(m, "line1")
	press(m, tea.KeyEnter, "")
	first := c.cur.line
	press(m, 'n', "n")
	if c.cur.line <= first {
		t.Fatalf("n must step to a newer match, %d -> %d", first, c.cur.line)
	}
	press(m, 'N', "N")
	if c.cur.line != first {
		t.Fatalf("N must step back to %d, got %d", first, c.cur.line)
	}

	// Backward search from the bottom.
	press(m, 'G', "G")
	press(m, '?', "?")
	typeQuery(m, "line02")
	press(m, tea.KeyEnter, "")
	if got := m.sess.LineText(c.cur.line); !strings.Contains(got, "line02") {
		t.Fatalf("? must land on line02, got %q", got)
	}

	// Miss: reported, cursor unmoved.
	at := c.cur
	press(m, '/', "/")
	typeQuery(m, "nosuchtext")
	press(m, tea.KeyEnter, "")
	if !c.miss || c.cur != at {
		t.Fatalf("a miss must report and keep the cursor, miss=%v cur=%+v", c.miss, c.cur)
	}
	if !strings.Contains(m.View(), "no matches") {
		t.Fatal("the status row must report the miss")
	}
}

// TestCopyModeSearchHighlights: visible occurrences of the accepted query
// render reverse-video.
func TestCopyModeSearchHighlights(t *testing.T) {
	m := copyModel(t)
	m.StartCopyMode()
	press(m, '/', "/")
	typeQuery(m, "bar")
	press(m, tea.KeyEnter, "")
	if !strings.Contains(m.View(), "\x1b[7m") {
		t.Fatal("the match must be reverse-video highlighted")
	}
}

// TestCopyModeOutputKeepsFlowing: output arriving while the mode is open
// lands in the buffer without moving the cursor off its line, and the live
// view shows it after exit.
func TestCopyModeOutputKeepsFlowing(t *testing.T) {
	m := copyModel(t)
	m.StartCopyMode()
	c := m.copy
	v := findVirtual(m, "foo bar baz")
	c.cur = vpos{line: v, col: 0}
	m.copyAfterMove()
	top := c.top

	m.FeedText("late arrival\n")
	waitFor(t, "late output fed", func() bool { return findVirtual(m, "late arrival") >= 0 })
	if !m.Copying() {
		t.Fatal("output must not close the mode")
	}
	if got := m.sess.LineText(c.cur.line); !strings.Contains(got, "foo bar baz") {
		t.Fatalf("the cursor must stay on its line, now on %q", got)
	}
	if c.top != top {
		t.Fatalf("the window must stay anchored, top %d -> %d", top, c.top)
	}
	press(m, 'q', "q")
	waitFor(t, "live view shows the arrival", func() bool {
		return strings.Contains(m.View(), "late arrival")
	})
}

// TestCopyModeCursorFollowsWindow: motions past the window edges re-anchor
// the view so the cursor stays visible.
func TestCopyModeCursorFollowsWindow(t *testing.T) {
	m := copyModel(t)
	m.StartCopyMode()
	c := m.copy
	press(m, 'g', "g")
	press(m, 'g', "g")
	if c.top != 0 {
		t.Fatalf("gg must scroll the window to the top, top=%d", c.top)
	}
	press(m, 'G', "G")
	if c.cur.line < c.top || c.cur.line > c.top+m.copyRows()-1 {
		t.Fatalf("G must keep the cursor visible, cur=%d window=[%d,%d]",
			c.cur.line, c.top, c.top+m.copyRows()-1)
	}
}

// TestCopyModeClearDrops: terminal.clear invalidates the buffer the mode
// cursors over, so it closes.
func TestCopyModeClearDrops(t *testing.T) {
	m := copyModel(t)
	m.StartCopyMode()
	m.Clear()
	if m.Copying() {
		t.Fatal("Clear must drop copy mode with the history")
	}
}

// TestCopyModeResizeDrops: a resize reflows the history (#935) and shifts
// every virtual index, so the mode closes rather than pointing at
// reshuffled lines.
func TestCopyModeResizeDrops(t *testing.T) {
	m := copyModel(t)
	m.StartCopyMode()
	m.SetSize(50, 8)
	if m.Copying() {
		t.Fatal("SetSize must drop copy mode")
	}
	if m.Scroll() != 0 {
		t.Fatalf("dropping the mode must snap to live, scroll=%d", m.Scroll())
	}
}

// TestCopyModeStatusLinePosition: the status row carries the cursor's
// 1-based line position over the virtual total.
func TestCopyModeStatusLinePosition(t *testing.T) {
	m := copyModel(t)
	m.StartCopyMode()
	press(m, 'g', "g")
	press(m, 'g', "g")
	if view := m.View(); !strings.Contains(view, "1/") {
		t.Fatalf("status row must show the position, view tail: %q",
			view[strings.LastIndex(view, "\n")+1:])
	}
}

// TestCopyModeSearchInputConsumed: while the query line is open, editing
// keys stay in the field; esc cancels the input but keeps the mode.
func TestCopyModeSearchInputConsumed(t *testing.T) {
	m := copyModel(t)
	m.StartCopyMode()
	press(m, '/', "/")
	typeQuery(m, "abc")
	if m.copy.query != "abc" {
		t.Fatalf("typed query = %q", m.copy.query)
	}
	press(m, tea.KeyEscape, "")
	if !m.Copying() || m.copy.input {
		t.Fatalf("esc must cancel the input, copying=%v input=%v", m.Copying(), m.copy.input)
	}
	if m.occupied {
		t.Fatal("query keys must never reach the session")
	}
}
