package htmldom

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// find returns the first element matching sel, failing the test when the
// count differs from want.
func find(t *testing.T, d *Document, sel string, want int) *html.Node {
	t.Helper()
	g, err := Compile(sel)
	if err != nil {
		t.Fatalf("compile %q: %v", sel, err)
	}
	got := d.Select(g)
	if len(got) != want {
		t.Fatalf("select %q: got %d matches, want %d", sel, len(got), want)
	}
	if want == 0 {
		return nil
	}
	return got[0]
}

func TestParseBuildsNestedTree(t *testing.T) {
	d := Parse(`<div id="a"><p class="x y">hi <b>bold</b></p></div>`)
	div := find(t, d, "#a", 1)
	if div.Data != "div" {
		t.Fatalf("root element = %q, want div", div.Data)
	}
	p := div.FirstChild
	if p == nil || p.Data != "p" {
		t.Fatalf("div's child = %+v, want p", p)
	}
	b := find(t, d, "p > b", 1)
	if b.Parent != p {
		t.Fatalf("b's parent is not the p element")
	}
	if txt := p.FirstChild; txt == nil || txt.Type != html.TextNode || txt.Data != "hi " {
		t.Fatalf("p's first child = %+v, want text 'hi '", txt)
	}
}

func TestParseToleratesMessyHTML(t *testing.T) {
	// Unclosed <li>s, a stray </span>, an unclosed <b> and a void <br>
	// without a slash — the everyday fixture file.
	src := "<ul><li>one</span><li>two<br><b>bold</ul><p>after"
	d := Parse(src)
	lis := "ul > li"
	g, _ := Compile(lis)
	items := d.Select(g)
	if len(items) != 2 {
		t.Fatalf("li count = %d, want 2 (auto-close)", len(items))
	}
	// The first li ends where the second begins.
	sp0, _ := d.Span(items[0])
	sp1, _ := d.Span(items[1])
	if sp0.End != sp1.Start {
		t.Fatalf("first li ends at %d, second starts at %d — want equal", sp0.End, sp1.Start)
	}
	// The unclosed b ends where </ul> closes its ancestor.
	b := find(t, d, "b", 1)
	spb, _ := d.Span(b)
	if got := src[spb.Start:spb.End]; got != "<b>bold" {
		t.Fatalf("unclosed b spans %q, want '<b>bold'", got)
	}
	// The trailing unclosed p runs to EOF.
	p := find(t, d, "p", 1)
	spp, _ := d.Span(p)
	if spp.End != len(src) {
		t.Fatalf("unclosed trailing p ends at %d, want EOF %d", spp.End, len(src))
	}
}

func TestParseHandlesScriptRawText(t *testing.T) {
	d := Parse(`<script>if (a < b) { x("</div>"); }</script><div></div>`)
	find(t, d, "script", 1)
	find(t, d, "div", 1)
}

func TestSpansMapToSource(t *testing.T) {
	src := "<html>\n<body>\n  <div id=\"x\">text</div>\n</body>\n</html>"
	d := Parse(src)
	div := find(t, d, "#x", 1)
	sp, ok := d.Span(div)
	if !ok {
		t.Fatal("div has no span")
	}
	if got := src[sp.Start:sp.End]; got != `<div id="x">text</div>` {
		t.Fatalf("outer span = %q", got)
	}
	if got := src[sp.Start:sp.OpenEnd]; got != `<div id="x">` {
		t.Fatalf("open-tag span = %q", got)
	}
	line, col := d.Position(sp.Start)
	if line != 2 || col != 2 {
		t.Fatalf("div position = (%d,%d), want (2,2)", line, col)
	}
	if off := d.Offset(2, 2); off != sp.Start {
		t.Fatalf("Offset round-trip = %d, want %d", off, sp.Start)
	}
}

func TestPositionCountsRunes(t *testing.T) {
	src := "<p>äöü<b>x</b></p>"
	d := Parse(src)
	b := find(t, d, "b", 1)
	sp, _ := d.Span(b)
	_, col := d.Position(sp.Start)
	if col != 6 { // <p> + 3 umlaut runes
		t.Fatalf("b column = %d, want 6 (rune counting)", col)
	}
	if off := d.Offset(0, 6); off != sp.Start {
		t.Fatalf("Offset(0,6) = %d, want %d", off, sp.Start)
	}
}

func TestNodeAtFindsDeepestNode(t *testing.T) {
	src := `<div><p>one</p><p>two</p></div>`
	d := Parse(src)
	g, _ := Compile("p")
	ps := d.Select(g)
	off := strings.Index(src, "two")
	n := d.NodeAt(off)
	if n == nil || n.Type != html.TextNode || n.Data != "two" {
		t.Fatalf("NodeAt(two) = %+v, want the text node", n)
	}
	if n.Parent != ps[1] {
		t.Fatal("text node's parent is not the second p")
	}
	if d.NodeAt(len(src)+5) != nil {
		t.Fatal("NodeAt past EOF should be nil")
	}
}

func TestSelectMatchesCombinators(t *testing.T) {
	d := Parse(`<div class="a"><span>1</span></div><div class="b"><span>2</span></div>`)
	find(t, d, "div.b > span", 1)
	find(t, d, "div span", 2)
	find(t, d, "div.a, div.b", 2)
	find(t, d, "span:nth-child(1)", 2)
	find(t, d, ".missing", 0)
}

func TestCompileRejectsInvalidSelector(t *testing.T) {
	if _, err := Compile("div["); err == nil {
		t.Fatal("expected error for invalid selector")
	}
}

// pathTo asserts the selector path of the node re-finds exactly that node.
func pathTo(t *testing.T, d *Document, n *html.Node) string {
	t.Helper()
	sel := d.SelectorPath(n)
	if sel == "" {
		t.Fatal("empty selector path")
	}
	g, err := Compile(sel)
	if err != nil {
		t.Fatalf("path %q does not compile: %v", sel, err)
	}
	got := d.Select(g)
	if len(got) != 1 || got[0] != n {
		t.Fatalf("path %q matches %d nodes, want exactly the target", sel, len(got))
	}
	return sel
}

func TestSelectorPathPrefersUniqueID(t *testing.T) {
	d := Parse(`<div><section id="main"><p>x</p></section></div>`)
	sec := find(t, d, "section", 1)
	if sel := pathTo(t, d, sec); sel != "#main" {
		t.Fatalf("path = %q, want #main", sel)
	}
}

func TestSelectorPathUsesClassesAndNthChild(t *testing.T) {
	d := Parse(`<ul><li class="odd">1</li><li class="even">2</li><li class="odd">3</li></ul>`)
	g, _ := Compile("li")
	lis := d.Select(g)
	if sel := pathTo(t, d, lis[1]); !strings.Contains(sel, "li.even") {
		t.Fatalf("path = %q, want a li.even segment", sel)
	}
	// The two .odd items need a positional segment.
	if sel := pathTo(t, d, lis[2]); !strings.Contains(sel, ":nth-child(3)") {
		t.Fatalf("path = %q, want :nth-child(3)", sel)
	}
}

func TestSelectorPathDuplicateIDsNotUsed(t *testing.T) {
	d := Parse(`<div id="dup"><span>1</span></div><div id="dup"><span>2</span></div>`)
	g, _ := Compile("span")
	spans := d.Select(g)
	pathTo(t, d, spans[0])
	pathTo(t, d, spans[1])
}

func TestSelectorPathDeepStructure(t *testing.T) {
	d := Parse(`<div class="serp"><div class="result"><h3><a href="#">A</a></h3></div><div class="result"><h3><a href="#">B</a></h3></div></div>`)
	g, _ := Compile("a")
	as := d.Select(g)
	pathTo(t, d, as[0])
	pathTo(t, d, as[1])
}

func TestOuterHTMLIsVerbatimSource(t *testing.T) {
	src := "<div>\n  <p Class='One'>x &amp; y</p>\n</div>"
	d := Parse(src)
	p := find(t, d, "p", 1)
	if got := d.OuterHTML(p); got != "<p Class='One'>x &amp; y</p>" {
		t.Fatalf("outer HTML = %q", got)
	}
}

func TestCommentAndDoctypeNodes(t *testing.T) {
	d := Parse("<!doctype html>\n<!-- note -->\n<div></div>")
	var kinds []html.NodeType
	for c := d.Root.FirstChild; c != nil; c = c.NextSibling {
		kinds = append(kinds, c.Type)
	}
	want := []html.NodeType{html.DoctypeNode, html.CommentNode, html.ElementNode}
	if len(kinds) != len(want) {
		t.Fatalf("top-level nodes = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("top-level nodes = %v, want %v", kinds, want)
		}
	}
}

func TestWhitespaceOnlyTextSkipped(t *testing.T) {
	d := Parse("<div>\n   \n<p>x</p>\n</div>")
	div := find(t, d, "div", 1)
	if div.FirstChild == nil || div.FirstChild.Data != "p" {
		t.Fatal("whitespace-only text should not create nodes")
	}
}

func TestRangeOfConvertsSpan(t *testing.T) {
	src := "<div>\n<p>x</p>\n</div>"
	d := Parse(src)
	p := find(t, d, "p", 1)
	sp, _ := d.Span(p)
	r := d.RangeOf(sp)
	if r.StartLine != 1 || r.StartCol != 0 || r.EndLine != 1 || r.EndCol != 8 {
		t.Fatalf("range = %+v", r)
	}
}
