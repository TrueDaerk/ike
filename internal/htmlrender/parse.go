package htmlrender

import (
	"bytes"

	"golang.org/x/net/html"
)

// tree is a parsed document: real *html.Node values (so a later CSS pass can
// match them with cascadia, as the DOM inspector does) plus the source byte
// offset every node's token starts at.
type tree struct {
	root  *html.Node
	src   []byte
	start map[*html.Node]int
	// end records where each text node's raw token ends, so the renderer can
	// re-split the raw bytes and give every word its exact source offset.
	end map[*html.Node]int
}

// raw returns the undecoded source bytes of a text node.
func (t *tree) raw(n *html.Node) []byte {
	return t.src[t.start[n]:t.end[n]]
}

// voidElements never take a closing tag (HTML spec §13.1.2).
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}

// autoClose lists, per starting tag, the open elements it implicitly closes —
// the omitted-end-tag cases real pages rely on (<p>, <li>, <dt>/<dd>, table
// rows and cells, and a block start ending an open paragraph). The DOM
// inspector's parser (internal/htmldom) uses the same modest rule set; full
// HTML5 tree recovery is not the goal.
var autoClose = func() map[string]map[string]bool {
	m := map[string]map[string]bool{
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
	// Every block that cannot live inside a paragraph ends an open one.
	for _, tag := range []string{
		"address", "article", "aside", "blockquote", "details", "div", "dl",
		"fieldset", "figcaption", "figure", "footer", "form", "h1", "h2",
		"h3", "h4", "h5", "h6", "header", "hgroup", "hr", "main", "menu",
		"nav", "ol", "p", "pre", "section", "table", "ul",
		// Items and cells end a paragraph left open inside the previous one.
		"li", "dt", "dd", "tr", "td", "th",
	} {
		if m[tag] == nil {
			m[tag] = map[string]bool{}
		}
		m[tag]["p"] = true
	}
	return m
}()

// inlineTags are the elements an implicit close may reach past: closing an
// open <p> or <li> also ends the formatting elements still open inside it,
// but never crosses a block (a nested list, a cell, a container).
var inlineTags = map[string]bool{
	"a": true, "abbr": true, "b": true, "bdi": true, "bdo": true, "cite": true,
	"code": true, "data": true, "dfn": true, "em": true, "font": true,
	"i": true, "kbd": true, "label": true, "mark": true, "q": true, "s": true,
	"samp": true, "small": true, "span": true, "strike": true, "strong": true,
	"sub": true, "sup": true, "time": true, "tt": true, "u": true, "var": true,
	"del": true, "ins": true,
}

// parse builds the tree from src. It never fails: any input yields whatever
// structure could be recovered. Unlike html.Parse it mirrors the source (no
// implied <html>/<head>/<body>, stray end tags dropped, unclosed elements
// ending with their ancestor or at EOF) and — unlike the DOM inspector's
// parser — keeps whitespace-only text, which inline flow needs for the space
// between two inline elements.
func parse(src []byte) *tree {
	t := &tree{
		root:  &html.Node{Type: html.DocumentNode},
		src:   src,
		start: make(map[*html.Node]int),
		end:   make(map[*html.Node]int),
	}
	z := html.NewTokenizer(bytes.NewReader(src))
	stack := []*html.Node{t.root}
	off := 0
	for {
		tt := z.Next()
		start := off
		off += len(z.Raw())
		switch tt {
		case html.ErrorToken:
			return t // EOF: a byte reader cannot fail otherwise
		case html.StartTagToken, html.SelfClosingTagToken:
			tok := z.Token()
			stack = implicitClose(stack, tok.Data)
			n := &html.Node{Type: html.ElementNode, Data: tok.Data, DataAtom: tok.DataAtom, Attr: tok.Attr}
			stack[len(stack)-1].AppendChild(n)
			t.start[n] = start
			if tt == html.StartTagToken && !voidElements[tok.Data] {
				stack = append(stack, n)
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			for i := len(stack) - 1; i >= 1; i-- {
				if stack[i].Data == string(name) {
					stack = stack[:i]
					break
				}
			}
		case html.TextToken:
			n := &html.Node{Type: html.TextNode, Data: string(z.Text())}
			stack[len(stack)-1].AppendChild(n)
			t.start[n], t.end[n] = start, off
		}
	}
}

// implicitClose pops the open elements a starting tag ends: the nearest open
// element in its autoClose set, provided only inline elements lie above it.
// It repeats, so a <tr> ends both the open cell and the open row.
func implicitClose(stack []*html.Node, tag string) []*html.Node {
	set := autoClose[tag]
	if set == nil {
		return stack
	}
	for {
		idx := -1
		for i := len(stack) - 1; i >= 1; i-- {
			if set[stack[i].Data] {
				idx = i
				break
			}
			if !inlineTags[stack[i].Data] {
				break
			}
		}
		if idx < 0 {
			return stack
		}
		stack = stack[:idx]
	}
}

// attr returns an element's attribute value and whether it is present.
func attr(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}
