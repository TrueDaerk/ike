package htmlpreview

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// links_test.go mirrors the markdown preview's link tests
// (internal/preview/links_test.go) for the HTML preview (#2741).

const linkDoc = `<html>
<body>
<p>First <a href="notes.html">the notes</a> and <a href="https://example.com/x">remote</a>.</p>
<p>Then <a href="#deep">jump down</a>.</p>
</body>
</html>
`

// key sends one key press and returns the command it produced.
func key(m *Model, k tea.KeyPressMsg) tea.Cmd { return m.Update(k) }

var (
	tabKey      = tea.KeyPressMsg{Code: tea.KeyTab}
	shiftTabKey = tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	enterKey    = tea.KeyPressMsg{Code: tea.KeyEnter}
)

// TestTabSelectsLinksAndWraps: tab walks the links in reading order and wraps
// at the end, shift+tab walks back and wraps at the start.
func TestTabSelectsLinksAndWraps(t *testing.T) {
	m := sized(t, linkDoc)
	if !m.HasLinks() {
		t.Fatal("the document has links")
	}
	if _, ok := m.SelectedTarget(); ok {
		t.Fatal("nothing is selected before the first tab")
	}
	want := []string{"notes.html", "https://example.com/x", "#deep", "notes.html"}
	for i, w := range want {
		key(&m, tabKey)
		if got, _ := m.SelectedTarget(); got != w {
			t.Fatalf("tab %d selected %q, want %q", i+1, got, w)
		}
	}
	key(&m, shiftTabKey)
	if got, _ := m.SelectedTarget(); got != "#deep" {
		t.Fatalf("shift+tab should wrap back to the last link, got %q", got)
	}
	var fresh Model
	fresh = sized(t, linkDoc)
	key(&fresh, shiftTabKey)
	if got, _ := fresh.SelectedTarget(); got != "#deep" {
		t.Fatalf("shift+tab from no selection picks the last link, got %q", got)
	}
}

// TestNoLinksLeavesTabInert: a link-free document selects nothing.
func TestNoLinksLeavesTabInert(t *testing.T) {
	m := sized(t, longDoc(3))
	if m.HasLinks() {
		t.Fatal("no links expected")
	}
	key(&m, tabKey)
	if _, ok := m.SelectedTarget(); ok {
		t.Fatal("tab must not select anything in a link-free document")
	}
}

// TestSelectedLinkIsHighlighted: the selected label is drawn in reverse video
// and only it.
func TestSelectedLinkIsHighlighted(t *testing.T) {
	m := sized(t, linkDoc)
	if strings.Contains(m.View(), "\x1b[7m") {
		t.Fatal("nothing is highlighted before a selection")
	}
	key(&m, tabKey)
	v := m.View()
	i := strings.Index(v, "\x1b[7m")
	if i < 0 {
		t.Fatalf("the selected link is not highlighted:\n%q", v)
	}
	if rest := v[i+len("\x1b[7m"):]; !strings.HasPrefix(rest, "the notes") {
		t.Fatalf("the highlight should wrap the label, got %q", rest[:min(len(rest), 20)])
	}
	if !strings.Contains(ansi.Strip(v), "First the notes and remote.") {
		t.Fatalf("highlighting must not change the text:\n%s", ansi.Strip(v))
	}
}

// TestWrappedLinkHighlightsEveryPiece: a label wrapped over two lines is
// highlighted on both.
func TestWrappedLinkHighlightsEveryPiece(t *testing.T) {
	m := New("htmlpreview", "/tmp/page.html", nil)
	m.SetSize(20, 10)
	m.SetSourceImmediate(`<p>go <a href="x.html">a label long enough to wrap twice</a></p>`)
	key(&m, tabKey)
	if n := strings.Count(m.View(), "\x1b[7m"); n < 2 {
		t.Fatalf("a wrapped label needs a highlight per line, got %d:\n%s", n, ansi.Strip(m.View()))
	}
}

// TestEnterEmitsLinkMsg: enter on a selection yields a LinkMsg naming the
// preview, the source path and the destination; y the same with Copy.
func TestEnterEmitsLinkMsg(t *testing.T) {
	m := sized(t, linkDoc)
	key(&m, tabKey)
	cmd := key(&m, enterKey)
	if cmd == nil {
		t.Fatal("enter on a selected link must emit a command")
	}
	got, ok := cmd().(LinkMsg)
	want := LinkMsg{Key: "htmlpreview", Path: "/tmp/page.html", Target: "notes.html"}
	if !ok || got != want {
		t.Fatalf("enter emitted %#v, want %#v", got, want)
	}
	cmd = key(&m, tea.KeyPressMsg{Code: 'y', Text: "y"})
	if got, ok := cmd().(LinkMsg); !ok || !got.Copy || got.Target != "notes.html" {
		t.Fatalf("y emitted %#v, want a copy of the selected link", got)
	}
}

// TestEnterWithoutSelectionSyncsBack: enter with no link selected is the
// reverse cursor sync — the source line of the rendered line at the sync
// row, the row a forward sync puts the caret's line on, so the two
// round-trip.
func TestEnterWithoutSelectionSyncsBack(t *testing.T) {
	m := sized(t, longDoc(60))
	m.SetCursorLine(32) // para030
	cmd := key(&m, enterKey)
	if cmd == nil {
		t.Fatal("enter without a selection must emit the reverse sync")
	}
	got, ok := cmd().(SourceLineMsg)
	if !ok || got != (SourceLineMsg{Path: "/tmp/page.html", Line: 32}) {
		t.Fatalf("enter emitted %#v, want source line 32", got)
	}
	m.ScrollBy(5)
	if got := key(&m, enterKey)().(SourceLineMsg); got.Line <= 32 {
		t.Fatalf("after scrolling down the sync row maps further down, got %d", got.Line)
	}
}

// TestEscClearsSelection: esc drops the selection, so enter syncs back again.
func TestEscClearsSelection(t *testing.T) {
	m := sized(t, linkDoc)
	key(&m, tabKey)
	key(&m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if _, ok := m.SelectedTarget(); ok {
		t.Fatal("esc must clear the link selection")
	}
	if _, ok := key(&m, enterKey)().(SourceLineMsg); !ok {
		t.Fatal("enter after esc is the reverse sync")
	}
}

// TestClickFollowsLink: a click on a label selects and follows it; a click
// beside it or on a plain line does nothing.
func TestClickFollowsLink(t *testing.T) {
	m := sized(t, linkDoc)
	plainLine := strings.Split(plain(m), "\n")[0]
	x := strings.Index(plainLine, "remote")
	if x < 0 {
		t.Fatalf("no remote link on the first line: %q", plainLine)
	}
	cmd := m.Click(x+2, 0)
	if cmd == nil {
		t.Fatal("a click on a link must follow it")
	}
	if got, ok := cmd().(LinkMsg); !ok || got.Target != "https://example.com/x" {
		t.Fatalf("click emitted %#v", got)
	}
	if got, _ := m.SelectedTarget(); got != "https://example.com/x" {
		t.Fatalf("the clicked link becomes the selection, got %q", got)
	}
	if cmd := m.Click(1, 0); cmd != nil {
		t.Fatal("a click on plain text must do nothing")
	}
	if cmd := m.Click(0, 8); cmd != nil {
		t.Fatal("a click below the document must do nothing")
	}
}

// TestScrollToAnchorFindsElement: an id (or a name) scrolls there; an unknown
// one reports false and leaves the view alone.
func TestScrollToAnchorFindsElement(t *testing.T) {
	var b strings.Builder
	b.WriteString("<p><a href=\"#deep\">down</a></p>\n")
	for i := 0; i < 40; i++ {
		b.WriteString("<p>filler</p>\n")
	}
	b.WriteString("<h2 id=\"deep\">The Deep End</h2>\n<p><a name=\"caf\u00e9\">named</a></p>\n")
	m := sized(t, b.String())
	if !m.ScrollToAnchor("deep") {
		t.Fatal("the id anchor exists")
	}
	if !strings.Contains(plain(m), "The Deep End") {
		t.Fatalf("the anchor should scroll its element into view:\n%s", plain(m))
	}
	m.ScrollBy(-100)
	if !m.ScrollToAnchor("caf%C3%A9") {
		t.Fatal("a percent-encoded fragment matches its decoded name")
	}
	top := m.Top()
	if m.ScrollToAnchor("nowhere") || m.Top() != top {
		t.Fatal("an unknown anchor reports false and does not scroll")
	}
}

// TestSelectionSurvivesRerender: an edit that drops links clamps the
// selection instead of pointing past the index.
func TestSelectionSurvivesRerender(t *testing.T) {
	m := sized(t, linkDoc)
	for range 3 {
		key(&m, tabKey)
	}
	m.SetSourceImmediate(`<p><a href="only.html">only</a></p>`)
	if got, _ := m.SelectedTarget(); got != "only.html" {
		t.Fatalf("selection should clamp to the remaining link, got %q", got)
	}
}
