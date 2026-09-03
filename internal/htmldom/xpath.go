package htmldom

// xpath.go names a node's location as an XPath (#2414) — the spelling the
// xmq playground's `select` command takes, next to SelectorPath's CSS one.
// Two entry points because the two markup languages parse differently:
// XPath works over the html.Node tree this package already builds (the DOM
// the HTML5 algorithm produced, implied <html>/<body> included — which is
// the tree an XPath engine over parsed HTML sees too), while XMLXPathAt
// scans the raw source with encoding/xml, because pushing XML through the
// HTML parser would lowercase its tags and wrap it in a phantom <html>.

import (
	"encoding/xml"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// XPath renders the absolute XPath of element n: `/html/body/div[2]/a`.
// Each step carries a positional predicate exactly when the element has
// same-named element siblings — `div[2]` names the second div, a lone `head`
// stays bare. A non-element node (NodeAt answers the text node under a
// caret) resolves as its nearest element ancestor; with none there is no
// path.
func (d *Document) XPath(n *html.Node) string {
	for n != nil && n.Type != html.ElementNode {
		n = n.Parent
	}
	if n == nil {
		return ""
	}
	var parts []string
	for cur := n; cur != nil && cur.Type == html.ElementNode; cur = cur.Parent {
		parts = append([]string{xpathStep(cur)}, parts...)
	}
	return "/" + strings.Join(parts, "/")
}

// xpathStep is one path step: the tag, positionally qualified when the
// element shares its name with a sibling.
func xpathStep(n *html.Node) string {
	pos, dup := 1, 0
	if n.Parent != nil {
		for s := n.Parent.FirstChild; s != nil; s = s.NextSibling {
			if s.Type != html.ElementNode || s.Data != n.Data {
				continue
			}
			dup++
			if s == n {
				pos = dup
			}
		}
	}
	if dup > 1 {
		return n.Data + "[" + itoa(pos) + "]"
	}
	return n.Data
}

// XMLXPathAt returns the XPath of the innermost element enclosing byte
// offset off in the XML source — start tag, content and end tag all count as
// inside. ok is false when the offset sits outside every element (before the
// root, in the epilogue) or the document does not scan up to it. The scan is
// token-based and lenient (Strict off), so a half-edited document still
// answers for the part that parses.
func XMLXPathAt(src string, off int) (string, bool) {
	dec := xml.NewDecoder(strings.NewReader(src))
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	type level struct {
		step   string
		counts map[string]int
	}
	stack := []level{{counts: map[string]int{}}}
	path := func() (string, bool) {
		if len(stack) < 2 {
			return "", false
		}
		parts := make([]string, 0, len(stack)-1)
		for _, l := range stack[1:] {
			parts = append(parts, l.step)
		}
		return "/" + strings.Join(parts, "/"), true
	}
	for {
		before := dec.InputOffset()
		tok, err := dec.Token()
		if err != nil {
			// EOF or a scan error past the target: the enclosing elements
			// seen so far are the answer — for a well-formed document the
			// stack is empty here and there is none.
			return path()
		}
		if int64(off) < before {
			return path()
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := t.Name.Local
			top := &stack[len(stack)-1]
			top.counts[name]++
			step := name
			if k := top.counts[name]; k > 1 {
				step = name + "[" + itoa(k) + "]"
			}
			stack = append(stack, level{step: step, counts: map[string]int{}})
			if int64(off) < dec.InputOffset() {
				return path()
			}
		case xml.EndElement:
			if int64(off) < dec.InputOffset() {
				return path()
			}
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		default:
			if int64(off) < dec.InputOffset() {
				return path()
			}
		}
	}
}

// XMLOffset converts 0-based (line, rune column) coordinates into a byte
// offset in src — the glue between an editor cursor and XMLXPathAt, for XML
// buffers that never went through Parse and so have no Document to ask.
func XMLOffset(src string, line, col int) int {
	off := 0
	for line > 0 {
		i := strings.IndexByte(src[off:], '\n')
		if i < 0 {
			return len(src)
		}
		off += i + 1
		line--
	}
	end := len(src)
	if i := strings.IndexByte(src[off:], '\n'); i >= 0 {
		end = off + i
	}
	for col > 0 && off < end {
		_, size := utf8.DecodeRuneInString(src[off:end])
		off += size
		col--
	}
	return off
}
