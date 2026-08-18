// Package htmldom builds a DOM tree with source positions for the DOM
// inspector tool window (#1929). The parser is the x/net/html tokenizer driven
// by a small tolerant stack machine — not html.Parse — because the tokenizer
// is the only layer that exposes byte offsets, and because real-world fixture
// files are messy: stray end tags are dropped, unclosed elements end where
// their ancestor closes (or at EOF), and the tree mirrors the source rather
// than the HTML5 recovery algorithm (no implied <html>/<head>/<body> nodes).
// Nodes are real *html.Node values so cascadia CSS selectors match them
// directly; a side table maps each node to its source span.
package htmldom

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
)

// Span is a node's extent in the source, in byte offsets. End is exclusive.
// OpenEnd is the end of the opening tag (== End for leaves), which is what the
// editor highlights for a matched element — painting the whole outer HTML of
// <body> would flood the buffer.
type Span struct {
	Start, End int
	OpenEnd    int
}

// Range is a node span in 0-based editor coordinates (rune columns, end
// exclusive), ready for cursor jumps and highlight messages.
type Range struct {
	StartLine, StartCol int
	EndLine, EndCol     int
}

// Document is one parsed buffer: the node tree, the source it came from and
// the offset↔position tables.
type Document struct {
	Root       *html.Node // DocumentNode; children are the top-level nodes
	src        string
	spans      map[*html.Node]Span
	lineStarts []int // byte offset of each line start
}

// voidElements never take a closing tag (HTML spec §13.1.2).
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}

// autoClose lists, per starting tag, the open tags it implicitly closes when
// it appears as their next sibling — the common omitted-end-tag cases (<p>,
// <li>, table rows/cells) so fixture files without closing tags still nest
// sensibly. Deliberately modest: full HTML5 recovery is not the goal.
var autoClose = map[string]map[string]bool{
	"p":        {"p": true},
	"li":       {"li": true},
	"dt":       {"dd": true, "dt": true},
	"dd":       {"dd": true, "dt": true},
	"tr":       {"tr": true, "td": true, "th": true},
	"td":       {"td": true, "th": true},
	"th":       {"td": true, "th": true},
	"thead":    {"tr": true, "td": true, "th": true},
	"tbody":    {"tr": true, "td": true, "th": true, "thead": true, "tbody": true},
	"tfoot":    {"tr": true, "td": true, "th": true, "thead": true, "tbody": true},
	"option":   {"option": true},
	"optgroup": {"option": true, "optgroup": true},
}

// Parse builds the document from src. It never fails: any input yields a tree
// of whatever structure could be recovered.
func Parse(src string) *Document {
	d := &Document{
		Root:  &html.Node{Type: html.DocumentNode},
		src:   src,
		spans: make(map[*html.Node]Span),
	}
	d.lineStarts = lineStarts(src)
	z := html.NewTokenizer(strings.NewReader(src))
	stack := []*html.Node{d.Root}
	off := 0
	for {
		tt := z.Next()
		raw := z.Raw()
		start, end := off, off+len(raw)
		off = end
		switch tt {
		case html.ErrorToken:
			// EOF (a string reader cannot fail otherwise): every element
			// still open extends to the end of the source.
			for i := len(stack) - 1; i >= 1; i-- {
				d.setEnd(stack[i], len(src))
			}
			return d
		case html.StartTagToken, html.SelfClosingTagToken:
			tok := z.Token()
			for top := stack[len(stack)-1]; top.Type == html.ElementNode && autoClose[tok.Data][top.Data]; top = stack[len(stack)-1] {
				d.setEnd(top, start)
				stack = stack[:len(stack)-1]
			}
			n := &html.Node{Type: html.ElementNode, Data: tok.Data, DataAtom: tok.DataAtom, Attr: tok.Attr}
			stack[len(stack)-1].AppendChild(n)
			d.spans[n] = Span{Start: start, End: end, OpenEnd: end}
			if tt == html.StartTagToken && !voidElements[tok.Data] {
				stack = append(stack, n)
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			tag := string(name)
			idx := -1
			for i := len(stack) - 1; i >= 1; i-- {
				if stack[i].Data == tag {
					idx = i
					break
				}
			}
			if idx < 0 {
				continue // stray end tag: dropped
			}
			// Elements the end tag implicitly closes end where it starts;
			// the matched element ends after it.
			for i := len(stack) - 1; i > idx; i-- {
				d.setEnd(stack[i], start)
			}
			d.setEnd(stack[idx], end)
			stack = stack[:idx]
		case html.TextToken:
			text := string(z.Text())
			if strings.TrimSpace(text) == "" {
				continue // inter-tag whitespace: no node, offsets unaffected
			}
			n := &html.Node{Type: html.TextNode, Data: text}
			stack[len(stack)-1].AppendChild(n)
			d.spans[n] = Span{Start: start, End: end, OpenEnd: end}
		case html.CommentToken:
			n := &html.Node{Type: html.CommentNode, Data: string(z.Text())}
			stack[len(stack)-1].AppendChild(n)
			d.spans[n] = Span{Start: start, End: end, OpenEnd: end}
		case html.DoctypeToken:
			n := &html.Node{Type: html.DoctypeNode, Data: string(z.Text())}
			stack[len(stack)-1].AppendChild(n)
			d.spans[n] = Span{Start: start, End: end, OpenEnd: end}
		}
	}
}

// setEnd finalizes a node's source extent.
func (d *Document) setEnd(n *html.Node, off int) {
	sp := d.spans[n]
	sp.End = off
	d.spans[n] = sp
}

// Span reports a node's source extent.
func (d *Document) Span(n *html.Node) (Span, bool) {
	sp, ok := d.spans[n]
	return sp, ok
}

// OuterHTML returns a node's exact source text (not a re-serialization, so
// the original formatting and any tolerated breakage survive the copy).
func (d *Document) OuterHTML(n *html.Node) string {
	sp, ok := d.spans[n]
	if !ok {
		return ""
	}
	return d.src[sp.Start:sp.End]
}

// Position converts a byte offset into 0-based (line, rune column).
func (d *Document) Position(off int) (line, col int) {
	if off < 0 {
		off = 0
	}
	if off > len(d.src) {
		off = len(d.src)
	}
	line = sort.Search(len(d.lineStarts), func(i int) bool { return d.lineStarts[i] > off }) - 1
	return line, utf8.RuneCountInString(d.src[d.lineStarts[line]:off])
}

// Offset converts a 0-based (line, rune column) into a byte offset, clamping
// past-end lines and columns to the nearest valid position.
func (d *Document) Offset(line, col int) int {
	if line < 0 {
		return 0
	}
	if line >= len(d.lineStarts) {
		return len(d.src)
	}
	off := d.lineStarts[line]
	for ; col > 0 && off < len(d.src); col-- {
		r, size := utf8.DecodeRuneInString(d.src[off:])
		if r == '\n' {
			break
		}
		off += size
	}
	return off
}

// RangeOf converts a span into editor coordinates.
func (d *Document) RangeOf(sp Span) Range {
	sl, sc := d.Position(sp.Start)
	el, ec := d.Position(sp.End)
	return Range{StartLine: sl, StartCol: sc, EndLine: el, EndCol: ec}
}

// NodeAt finds the deepest node whose span contains the byte offset, or nil
// when no node does (offset in inter-tag whitespace at the top level).
func (d *Document) NodeAt(off int) *html.Node {
	cur := d.Root
descend:
	for {
		for c := cur.FirstChild; c != nil; c = c.NextSibling {
			if sp, ok := d.spans[c]; ok && sp.Start <= off && off < sp.End {
				cur = c
				continue descend
			}
		}
		break
	}
	if cur == d.Root {
		return nil
	}
	return cur
}

// Compile parses a CSS selector (comma groups allowed). The error is
// cascadia's message, surfaced verbatim in the pane.
func Compile(sel string) (cascadia.SelectorGroup, error) {
	return cascadia.ParseGroup(sel)
}

// Select returns every element the compiled selector matches, in document
// order.
func (d *Document) Select(sel cascadia.SelectorGroup) []*html.Node {
	if sel == nil {
		return nil
	}
	return cascadia.QueryAll(d.Root, sel)
}

// SelectorPath builds a CSS selector that re-finds exactly n: the shortest
// suffix of the ancestor chain that is unique in the document, preferring a
// document-unique #id, then a sibling-unique tag.classes segment, then
// tag:nth-child. When even the full chain stays ambiguous every segment is
// nth-child-qualified (pathological self-similar trees).
func (d *Document) SelectorPath(n *html.Node) string {
	if n == nil || n.Type != html.ElementNode {
		return ""
	}
	var parts []string
	for cur := n; cur != nil && cur.Type == html.ElementNode; cur = cur.Parent {
		if id := attrVal(cur, "id"); id != "" && validIdent(id) && d.idUnique(id) {
			parts = append([]string{"#" + id}, parts...)
			// A document-unique id anchors the whole path; nothing above
			// it can add ambiguity.
			return strings.Join(parts, " > ")
		}
		parts = append([]string{d.segment(cur)}, parts...)
		if sel := strings.Join(parts, " > "); d.matchesOnly(sel, n) {
			return sel
		}
	}
	// Full chain still ambiguous: qualify every segment by child position.
	parts = parts[:0]
	for cur := n; cur != nil && cur.Type == html.ElementNode; cur = cur.Parent {
		parts = append([]string{nthChildSegment(cur)}, parts...)
	}
	return strings.Join(parts, " > ")
}

// segment describes one element among its siblings: tag plus classes when
// that is sibling-unique, tag:nth-child otherwise.
func (d *Document) segment(n *html.Node) string {
	seg := n.Data
	for _, c := range classList(n) {
		if validIdent(c) {
			seg += "." + c
		}
	}
	dup := 0
	for s := firstElement(n.Parent); s != nil; s = nextElement(s) {
		if s.Data == n.Data && sameClasses(s, n) {
			dup++
		}
	}
	if dup <= 1 {
		return seg
	}
	return nthChildSegment(n)
}

// nthChildSegment is tag:nth-child(k) with k the 1-based position among the
// parent's element children — unique among siblings by construction.
func nthChildSegment(n *html.Node) string {
	k := 1
	for s := firstElement(n.Parent); s != nil && s != n; s = nextElement(s) {
		k++
	}
	return n.Data + ":nth-child(" + itoa(k) + ")"
}

// matchesOnly reports whether sel matches exactly n in the document.
func (d *Document) matchesOnly(sel string, n *html.Node) bool {
	g, err := cascadia.ParseGroup(sel)
	if err != nil {
		return false
	}
	found := cascadia.QueryAll(d.Root, g)
	return len(found) == 1 && found[0] == n
}

// idUnique reports whether exactly one element in the document carries id.
func (d *Document) idUnique(id string) bool {
	count := 0
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && attrVal(n, "id") == id {
			count++
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(d.Root)
	return count == 1
}

// attrVal returns an element's attribute value ("" when absent).
func attrVal(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// classList splits an element's class attribute into its class names.
func classList(n *html.Node) []string {
	return strings.Fields(attrVal(n, "class"))
}

// sameClasses reports whether two elements carry the same valid-ident class
// set (order-insensitive) — the sibling-uniqueness test for tag.classes.
func sameClasses(a, b *html.Node) bool {
	av, bv := validClasses(a), validClasses(b)
	if len(av) != len(bv) {
		return false
	}
	set := make(map[string]bool, len(av))
	for _, c := range av {
		set[c] = true
	}
	for _, c := range bv {
		if !set[c] {
			return false
		}
	}
	return true
}

func validClasses(n *html.Node) []string {
	var out []string
	for _, c := range classList(n) {
		if validIdent(c) {
			out = append(out, c)
		}
	}
	return out
}

// validIdent reports whether s is a CSS identifier that needs no escaping —
// anything else is skipped rather than escaped, since a position segment is
// always available as fallback.
func validIdent(s string) bool {
	if s == "" || (s[0] >= '0' && s[0] <= '9') || s[0] == '-' {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

func firstElement(n *html.Node) *html.Node {
	if n == nil {
		return nil
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			return c
		}
	}
	return nil
}

func nextElement(n *html.Node) *html.Node {
	for s := n.NextSibling; s != nil; s = s.NextSibling {
		if s.Type == html.ElementNode {
			return s
		}
	}
	return nil
}

// lineStarts indexes the byte offset of every line start.
func lineStarts(src string) []int {
	starts := []int{0}
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
