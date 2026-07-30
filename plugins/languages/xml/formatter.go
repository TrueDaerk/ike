package langxml

import (
	"errors"
	"strings"
)

// formatter.go is the built-in XML pretty-printer (Roadmap 0470, #1404). XML
// has no language server in IKE (lemminx is a JVM application, #1253), so
// the plugin ships its own formatter for .xml and the dialects it claims
// (.xsd/.xsl/.xslt/.svg/.plist/.wsdl/.csproj & co). Pure Go — a hand-rolled
// tokenizer + tree + printer that compiles without CGo; the Tree-sitter
// parse, where available, double-checks validity (grammar_cgo.go). Documents
// that fail to parse are left untouched with a message.
//
// Contract (#1404): element nesting indents per editorconfig/settings;
// attributes stay on one line until the configured max width, then wrap one
// per line aligned under the first; XML declaration, DOCTYPE, processing
// instructions, comments, CDATA, entity references and xml:space="preserve"
// subtrees pass through verbatim; text-only elements stay on one line
// (<name>value</name>); mixed content is never re-wrapped (whitespace there
// is significant); self-closing tags stay self-closing; nothing beyond
// whitespace is rewritten. Output is idempotent.

// xmlOptions carries one run's effective settings.
type xmlOptions struct {
	Indent   string // one indent unit
	TabWidth int    // visual width of a tab (line-width accounting)
	MaxWidth int    // wrap attributes beyond this visual width; 0 = never
}

var errXMLMalformed = errors.New("malformed XML — buffer left unchanged")

// --- tree -------------------------------------------------------------------

type xkind int

const (
	xElem xkind = iota
	xText
	xComment
	xCData
	xPI      // <?target …?> and the <?xml …?> declaration
	xDoctype // <!DOCTYPE …>
)

type xmlNode struct {
	kind      xkind
	name      string   // element name as written
	attrs     []string // raw attribute chunks, e.g. `id="1"`
	selfClose bool
	children  []*xmlNode
	text      string // raw source of text/comment/CDATA/PI/DOCTYPE nodes
	// source span for verbatim emission (mixed content, xml:space=preserve)
	srcStart, srcEnd   int
	lineStart, lineEnd int
	preserve           bool // xml:space="preserve" on this element
	mixed              bool // non-whitespace text mixed with child nodes
}

// parseXML builds the node tree; err on any structural breakage.
func parseXML(src string) ([]*xmlNode, error) {
	p := &xmlParser{src: src}
	roots, err := p.nodes("")
	if err != nil {
		return nil, err
	}
	if p.pos < len(src) {
		return nil, errXMLMalformed
	}
	return roots, nil
}

type xmlParser struct {
	src  string
	pos  int
	line int
}

// advance moves pos to j, tracking lines.
func (p *xmlParser) advance(j int) {
	p.line += strings.Count(p.src[p.pos:j], "\n")
	p.pos = j
}

// nodes parses siblings until the closing tag of parent (or EOF for "").
func (p *xmlParser) nodes(parent string) ([]*xmlNode, error) {
	var out []*xmlNode
	for p.pos < len(p.src) {
		if strings.HasPrefix(p.src[p.pos:], "</") {
			if parent == "" {
				return nil, errXMLMalformed
			}
			return out, nil
		}
		if p.src[p.pos] == '<' {
			n, err := p.markup()
			if err != nil {
				return nil, err
			}
			out = append(out, n)
			continue
		}
		n := p.textNode()
		if n != nil {
			out = append(out, n)
		}
	}
	if parent != "" {
		return nil, errXMLMalformed
	}
	return out, nil
}

// textNode consumes a text run up to the next '<'.
func (p *xmlParser) textNode() *xmlNode {
	start, startLine := p.pos, p.line
	j := strings.IndexByte(p.src[p.pos:], '<')
	if j < 0 {
		j = len(p.src) - p.pos
	}
	end := p.pos + j
	text := p.src[start:end]
	p.advance(end)
	if strings.TrimSpace(text) == "" {
		return nil // inter-element whitespace: re-created by the printer
	}
	return &xmlNode{kind: xText, text: text, srcStart: start, srcEnd: end, lineStart: startLine, lineEnd: p.line}
}

// markup parses one <…> construct.
func (p *xmlParser) markup() (*xmlNode, error) {
	start, startLine := p.pos, p.line
	rest := p.src[p.pos:]
	fin := func(n *xmlNode) *xmlNode {
		n.srcStart, n.srcEnd = start, p.pos
		n.lineStart, n.lineEnd = startLine, p.line
		return n
	}
	switch {
	case strings.HasPrefix(rest, "<!--"):
		j := strings.Index(rest, "-->")
		if j < 0 {
			return nil, errXMLMalformed
		}
		p.advance(p.pos + j + 3)
		return fin(&xmlNode{kind: xComment, text: p.src[start : start+j+3]}), nil
	case strings.HasPrefix(rest, "<![CDATA["):
		j := strings.Index(rest, "]]>")
		if j < 0 {
			return nil, errXMLMalformed
		}
		p.advance(p.pos + j + 3)
		return fin(&xmlNode{kind: xCData, text: p.src[start : start+j+3]}), nil
	case strings.HasPrefix(rest, "<?"):
		j := strings.Index(rest, "?>")
		if j < 0 {
			return nil, errXMLMalformed
		}
		p.advance(p.pos + j + 2)
		return fin(&xmlNode{kind: xPI, text: p.src[start : start+j+2]}), nil
	case strings.HasPrefix(rest, "<!"):
		// DOCTYPE (possibly with an [ internal subset ])
		depth := 0
		for j := 0; j < len(rest); j++ {
			switch rest[j] {
			case '[':
				depth++
			case ']':
				depth--
			case '>':
				if depth <= 0 {
					p.advance(p.pos + j + 1)
					return fin(&xmlNode{kind: xDoctype, text: p.src[start : start+j+1]}), nil
				}
			}
		}
		return nil, errXMLMalformed
	}
	// element open tag
	name, attrs, selfClose, tagEnd, err := p.openTag()
	if err != nil {
		return nil, err
	}
	n := &xmlNode{kind: xElem, name: name, attrs: attrs, selfClose: selfClose}
	for _, a := range attrs {
		if strings.HasPrefix(a, "xml:space") && strings.Contains(a, "preserve") {
			n.preserve = true
		}
	}
	p.advance(tagEnd)
	if !selfClose {
		kids, err := p.nodes(name)
		if err != nil {
			return nil, err
		}
		n.children = kids
		// consume the matching close tag
		if !strings.HasPrefix(p.src[p.pos:], "</") {
			return nil, errXMLMalformed
		}
		j := strings.IndexByte(p.src[p.pos:], '>')
		if j < 0 {
			return nil, errXMLMalformed
		}
		closeName := strings.TrimSpace(p.src[p.pos+2 : p.pos+j])
		if closeName != name {
			return nil, errXMLMalformed
		}
		p.advance(p.pos + j + 1)
	}
	n.srcStart, n.srcEnd = start, p.pos
	n.lineStart, n.lineEnd = startLine, p.line
	n.mixed = mixedContent(n)
	return n, nil
}

// openTag scans `<name attr… >` / `<name attr… />` from p.pos; returns the
// end offset of the tag without consuming it.
func (p *xmlParser) openTag() (name string, attrs []string, selfClose bool, end int, err error) {
	i := p.pos + 1
	src := p.src
	j := i
	for j < len(src) && !isXMLSpace(src[j]) && src[j] != '>' && src[j] != '/' {
		if src[j] == '<' {
			return "", nil, false, 0, errXMLMalformed
		}
		j++
	}
	if j == i {
		return "", nil, false, 0, errXMLMalformed
	}
	name = src[i:j]
	// attributes: whitespace-separated chunks with quote awareness
	for {
		for j < len(src) && isXMLSpace(src[j]) {
			j++
		}
		if j >= len(src) {
			return "", nil, false, 0, errXMLMalformed
		}
		if src[j] == '>' {
			return name, attrs, false, j + 1, nil
		}
		if src[j] == '/' && j+1 < len(src) && src[j+1] == '>' {
			return name, attrs, true, j + 2, nil
		}
		a := j
		var quote byte
		for j < len(src) {
			c := src[j]
			if quote != 0 {
				if c == quote {
					quote = 0
				}
				j++
				continue
			}
			if c == '"' || c == '\'' {
				quote = c
				j++
				continue
			}
			if isXMLSpace(c) || c == '>' || (c == '/' && j+1 < len(src) && src[j+1] == '>') {
				break
			}
			j++
		}
		if j >= len(src) || j == a {
			return "", nil, false, 0, errXMLMalformed
		}
		attrs = append(attrs, src[a:j])
	}
}

func isXMLSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// mixedContent reports non-whitespace text mixed with other child nodes —
// whitespace is significant there, so the printer passes it through.
func mixedContent(n *xmlNode) bool {
	hasText, hasOther := false, false
	for _, c := range n.children {
		if c.kind == xText {
			hasText = true
		} else {
			hasOther = true
		}
	}
	return hasText && hasOther
}

// --- printer ----------------------------------------------------------------

type xmlPrinter struct {
	opts  xmlOptions
	src   string
	lines []string
}

// formatXML pretty-prints the document; parse failure leaves it untouched
// (the caller surfaces errXMLMalformed).
func formatXML(text string, opts xmlOptions) (string, error) {
	if strings.TrimSpace(text) == "" {
		return text, nil
	}
	roots, err := parseXML(text)
	if err != nil {
		return "", err
	}
	if bad, checked := xmlParseHasErrors(text); checked && bad {
		return "", errXMLMalformed
	}
	p := &xmlPrinter{opts: opts, src: text}
	for _, n := range roots {
		p.node(n, 0)
	}
	return strings.Join(p.lines, "\n") + "\n", nil
}

func (p *xmlPrinter) indent(depth int) string { return strings.Repeat(p.opts.Indent, depth) }

// visualLen measures a rendered line with tabs expanded.
func (p *xmlPrinter) visualLen(s string) int {
	w := p.opts.TabWidth
	if w <= 0 {
		w = 4
	}
	return len(s) + strings.Count(s, "\t")*(w-1)
}

// verbatim emits a node's source span unchanged, re-anchoring only the first
// line at the current indent.
func (p *xmlPrinter) verbatim(n *xmlNode, depth int) {
	chunk := p.src[n.srcStart:n.srcEnd]
	parts := strings.Split(chunk, "\n")
	p.lines = append(p.lines, p.indent(depth)+parts[0])
	p.lines = append(p.lines, parts[1:]...)
}

// node emits one node at depth.
func (p *xmlPrinter) node(n *xmlNode, depth int) {
	switch n.kind {
	case xPI, xDoctype, xComment, xCData:
		p.verbatim(n, depth)
	case xText:
		p.lines = append(p.lines, p.indent(depth)+strings.TrimSpace(n.text))
	case xElem:
		p.element(n, depth)
	}
}

// element lays out one element.
func (p *xmlPrinter) element(n *xmlNode, depth int) {
	if n.preserve || n.mixed {
		p.verbatim(n, depth)
		return
	}
	open := p.openTagLines(n, depth)
	if n.selfClose {
		p.lines = append(p.lines, open...)
		return
	}
	if len(n.children) == 0 {
		// empty but not self-closing: keep the written form (<b></b>)
		open[len(open)-1] += "</" + n.name + ">"
		p.lines = append(p.lines, open...)
		return
	}
	// text-only elements stay on one line: <name>value</name>
	if len(n.children) == 1 && n.children[0].kind == xText {
		text := strings.TrimSpace(n.children[0].text)
		if len(open) == 1 && !strings.Contains(text, "\n") {
			p.lines = append(p.lines, open[0]+text+"</"+n.name+">")
			return
		}
		p.lines = append(p.lines, open...)
		p.lines = append(p.lines, p.indent(depth+1)+text)
		p.lines = append(p.lines, p.indent(depth)+"</"+n.name+">")
		return
	}
	p.lines = append(p.lines, open...)
	for _, c := range n.children {
		p.node(c, depth+1)
	}
	p.lines = append(p.lines, p.indent(depth)+"</"+n.name+">")
}

// openTagLines renders the open tag: one line while it fits, else the
// attributes wrap one per line aligned under the first (#1404).
func (p *xmlPrinter) openTagLines(n *xmlNode, depth int) []string {
	ind := p.indent(depth)
	closer := ">"
	if n.selfClose {
		closer = "/>"
	}
	one := ind + "<" + n.name
	if len(n.attrs) > 0 {
		one += " " + strings.Join(n.attrs, " ")
	}
	one += closer
	if p.opts.MaxWidth <= 0 || p.visualLen(one) <= p.opts.MaxWidth || len(n.attrs) < 2 {
		return []string{one}
	}
	// wrapped: first attribute on the tag line, the rest aligned under it
	head := ind + "<" + n.name + " "
	align := strings.Repeat(" ", p.visualLen(head))
	lines := []string{head + n.attrs[0]}
	for i := 1; i < len(n.attrs); i++ {
		lines = append(lines, align+n.attrs[i])
	}
	lines[len(lines)-1] += closer
	return lines
}
