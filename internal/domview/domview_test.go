package domview

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/net/html"

	"ike/internal/htmldom"
)

const fixture = `<div id="root">
  <ul class="list">
    <li class="item">one</li>
    <li class="item">two</li>
  </ul>
  <p>tail</p>
</div>`

func loaded(t *testing.T) Model {
	t.Helper()
	m := New(nil)
	m.SetSize(60, 12)
	m.SetDoc("/f/page.html", 1, htmldom.Parse(fixture))
	return m
}

func press(m *Model, keys ...string) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		var kp tea.KeyPressMsg
		if len([]rune(k)) == 1 {
			r := []rune(k)[0]
			kp = tea.KeyPressMsg{Code: r, Text: k}
			if r >= 'A' && r <= 'Z' {
				kp.Mod = tea.ModShift
			}
		} else {
			switch k {
			case "enter":
				kp = tea.KeyPressMsg{Code: tea.KeyEnter}
			case "esc":
				kp = tea.KeyPressMsg{Code: tea.KeyEscape}
			case "space":
				kp = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
			case "backspace":
				kp = tea.KeyPressMsg{Code: tea.KeyBackspace}
			default:
				panic("unknown key " + k)
			}
		}
		cmd = m.Update(kp)
	}
	return cmd
}

func rowLabels(m Model) []string {
	var out []string
	for _, r := range m.Rows() {
		out = append(out, nodeLabel(r.Node))
	}
	return out
}

func TestSetDocFlattensTree(t *testing.T) {
	m := loaded(t)
	labels := rowLabels(m)
	want := []string{"<div#root>", "<ul.list>", "<li.item>", "“one”", "<li.item>", "“two”", "<p>", "“tail”"}
	if len(labels) != len(want) {
		t.Fatalf("rows = %v, want %v", labels, want)
	}
	for i := range want {
		if labels[i] != want[i] {
			t.Fatalf("rows = %v, want %v", labels, want)
		}
	}
	if m.Rows()[1].Depth != 1 || m.Rows()[3].Depth != 3 {
		t.Fatalf("depths wrong: %+v", m.Rows())
	}
}

func TestFoldCollapsesSubtree(t *testing.T) {
	m := loaded(t)
	press(&m, "j") // onto <ul.list>
	press(&m, "space")
	labels := rowLabels(m)
	for _, l := range labels {
		if l == "<li.item>" {
			t.Fatalf("folded ul still shows children: %v", labels)
		}
	}
	press(&m, "l") // expand again
	if len(rowLabels(m)) != 8 {
		t.Fatalf("expand did not restore rows: %v", rowLabels(m))
	}
}

func TestEnterNavigatesToNodePosition(t *testing.T) {
	m := loaded(t)
	press(&m, "j", "j") // first <li.item>, line 2
	cmd := press(&m, "enter")
	if cmd == nil {
		t.Fatal("enter produced no command")
	}
	nav, ok := cmd().(NavigateMsg)
	if !ok {
		t.Fatalf("message = %T, want NavigateMsg", cmd())
	}
	if nav.Path != "/f/page.html" || nav.Line != 2 || nav.Col != 4 {
		t.Fatalf("navigate = %+v, want line 2 col 4", nav)
	}
}

func TestFollowCursorSelectsEnclosingNode(t *testing.T) {
	m := loaded(t)
	m.SetFocused(false)
	m.FollowCursor(3, 22) // inside "two"'s li
	if m.Current() < 0 || nodeLabel(m.Rows()[m.Current()].Node) != "“two”" {
		t.Fatalf("current = %d (%v)", m.Current(), rowLabels(m))
	}
	// Inside a folded subtree the visible ancestor is highlighted.
	m.SetFocused(true)
	press(&m, "g", "j") // top, then onto <ul.list>
	press(&m, "space") // fold <ul.list> — cursor moved to it first
	m.SetFocused(false)
	m.FollowCursor(3, 22)
	if m.Current() < 0 || nodeLabel(m.Rows()[m.Current()].Node) != "<ul.list>" {
		t.Fatalf("current inside fold = %d (%v)", m.Current(), rowLabels(m))
	}
}

func TestSelectorMatchesAndCount(t *testing.T) {
	m := loaded(t)
	for _, k := range []string{"/", "l", "i", ".", "i", "t", "e", "m"} {
		press(&m, k)
	}
	if !m.Editing() {
		t.Fatal("/ should start selector editing")
	}
	if m.Selector() != "li.item" || len(m.Matches()) != 2 {
		t.Fatalf("selector %q matches %d", m.Selector(), len(m.Matches()))
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "1/2 matches") {
		t.Fatalf("view lacks match count:\n%s", view)
	}
	press(&m, "enter")
	if m.Editing() {
		t.Fatal("enter should leave selector editing")
	}
}

func TestInvalidSelectorShowsError(t *testing.T) {
	m := loaded(t)
	for _, k := range []string{"/", "l", "i", "["} {
		press(&m, k)
	}
	if m.SelectorError() == "" {
		t.Fatal("invalid selector should set an error")
	}
	if len(m.Matches()) != 0 {
		t.Fatal("invalid selector should clear matches")
	}
	if !strings.Contains(ansi.Strip(m.View()), "✗") {
		t.Fatal("view lacks the error marker")
	}
}

func TestMatchNavigationWraps(t *testing.T) {
	m := loaded(t)
	for _, k := range []string{"/", "l", "i"} {
		press(&m, k)
	}
	press(&m, "enter")
	rev := m.MatchesRev()
	cmd := press(&m, "n")
	if m.MatchesRev() == rev {
		t.Fatal("n should bump the match revision")
	}
	if cmd == nil {
		t.Fatal("n should jump the editor")
	}
	if _, idx := m.MatchRanges(); idx != 1 {
		t.Fatalf("match index = %d, want 1", idx)
	}
	press(&m, "n") // wraps back to 0
	if _, idx := m.MatchRanges(); idx != 0 {
		t.Fatalf("match index after wrap = %d, want 0", idx)
	}
	press(&m, "N")
	if _, idx := m.MatchRanges(); idx != 1 {
		t.Fatalf("match index after N = %d, want 1", idx)
	}
}

func TestMatchRangesCoverOpeningTags(t *testing.T) {
	m := loaded(t)
	for _, k := range []string{"/", "l", "i"} {
		press(&m, k)
	}
	ranges, _ := m.MatchRanges()
	if len(ranges) != 2 {
		t.Fatalf("ranges = %v", ranges)
	}
	r := ranges[0]
	if r.StartLine != 2 || r.StartCol != 4 || r.EndLine != 2 || r.EndCol != 21 {
		t.Fatalf("first range = %+v", r)
	}
}

func TestCopySelectorAndOuterHTML(t *testing.T) {
	m := loaded(t)
	press(&m, "j", "j", "j") // the text node “one”
	cmd := press(&m, "c")
	if cmd == nil {
		t.Fatal("c produced no command")
	}
	cp := cmd().(CopyMsg)
	if cp.What != "CSS selector" {
		t.Fatalf("copy = %+v", cp)
	}
	// The text row copies its enclosing element's selector; it must re-find
	// exactly the first li.
	doc := htmldom.Parse(fixture)
	g, err := htmldom.Compile(cp.Text)
	if err != nil {
		t.Fatalf("copied selector %q: %v", cp.Text, err)
	}
	got := doc.Select(g)
	if len(got) != 1 || got[0].Data != "li" {
		t.Fatalf("copied selector %q matches %d nodes", cp.Text, len(got))
	}

	press(&m, "k") // back on first <li.item>
	cmd = press(&m, "Y")
	if cmd == nil {
		t.Fatal("Y produced no command")
	}
	cp = cmd().(CopyMsg)
	if cp.Text != `<li class="item">one</li>` {
		t.Fatalf("outer HTML = %q", cp.Text)
	}
}

func TestSetNotHTMLShowsNotice(t *testing.T) {
	m := loaded(t)
	m.SetNotHTML("/f/main.go")
	if len(m.Rows()) != 0 {
		t.Fatal("non-HTML buffer should clear the tree")
	}
	if !strings.Contains(ansi.Strip(m.View()), "not an HTML file") {
		t.Fatal("view lacks the not-HTML notice")
	}
}

func TestDoubleClickNavigates(t *testing.T) {
	m := loaded(t)
	at := time.Unix(100, 0)
	m.now = func() time.Time { return at }
	if cmd := m.Click(5, 2); cmd != nil { // row 0 selects
		t.Fatal("single click should not navigate")
	}
	if m.Cursor() != 0 {
		t.Fatalf("cursor = %d", m.Cursor())
	}
	at = at.Add(100 * time.Millisecond)
	if cmd := m.Click(5, 2); cmd == nil {
		t.Fatal("double click should navigate")
	}
	// Click on the selector line starts editing.
	m.Click(3, 1)
	if !m.Editing() {
		t.Fatal("selector-line click should start editing")
	}
}

func TestClickOnFoldGlyphToggles(t *testing.T) {
	m := loaded(t)
	// Row 1 is <ul.list> at depth 1: glyph cells are x=3,4.
	m.Click(3, 3)
	if len(m.Rows()) >= 8 {
		t.Fatalf("fold-glyph click did not fold: %v", rowLabels(m))
	}
}

func TestSetDocReappliesSelector(t *testing.T) {
	m := loaded(t)
	for _, k := range []string{"/", "p"} {
		press(&m, k)
	}
	press(&m, "enter")
	rev := m.MatchesRev()
	m.SetDoc("/f/page.html", 2, htmldom.Parse(fixture+"\n<p>extra</p>"))
	if m.MatchesRev() == rev {
		t.Fatal("SetDoc should bump the match revision")
	}
	if len(m.Matches()) != 2 {
		t.Fatalf("matches after reparse = %d, want 2", len(m.Matches()))
	}
}

func TestNodeLabelTruncatesText(t *testing.T) {
	n := &html.Node{Type: html.TextNode, Data: strings.Repeat("x", 100)}
	if got := nodeLabel(n); len([]rune(got)) > textPreviewRunes+3 {
		t.Fatalf("label not truncated: %q", got)
	}
}
