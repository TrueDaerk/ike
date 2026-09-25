// Package htmlrender is the UI-free core of the HTML preview (Epic 0530, #2739):
// it turns an HTML document into styled terminal lines plus the indexes a
// preview pane needs, the way a text-mode browser (w3m, lynx) reads a page.
//
// The model is a two-level flow. The document is parsed tolerantly with the
// golang.org/x/net/html tokenizer into a tree that remembers every node's
// source byte offset (parse.go; the DOM inspector parses the same way). A
// walk over the tree then keeps a stack of block indents (list markers,
// blockquote bars, definition and figure indents) and collects the inline
// content of the current block as fragments — words, collapsed spaces and
// forced breaks, each carrying its look, its link, its image and the source
// offset of the word. Every block boundary lays the pending fragments out:
// greedy word wrap at Options.Width minus the indent, or, inside <pre>, one
// verbatim line per source line cut at the width with the shared hscroll
// overflow marker. Vertical margins collapse into at most one blank line.
//
// What the walk emits is a Document: the rendered lines (SGR-styled from the
// theme palette, links wrapped in OSC 8 hyperlink sequences like the markdown
// preview's glamour output), the link index (label, href, first/last line),
// the image index (src, alt, line), the id/name anchors, and the source map
// (rendered line ↔ source offset/line, nearest-match both ways) the pane's
// cursor sync reads. Tables and images get placeholder treatment here — a
// row per line with "│" between cells, and "[alt]" — until the pane grows the
// grid (0530/4) and inline pixels (0530/5).
//
// Rendering is pure: no bubbletea, no I/O, no shared state, so a pane can run
// it on a goroutine.
package htmlrender

import (
	"bytes"
	"path"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/net/html"

	"ike/internal/hscroll"
	"ike/internal/theme"
)

// Options configure one render.
type Options struct {
	// Width is the display width lines wrap at; <= 0 means DefaultWidth.
	Width int
	// Palette colours the output; nil uses the default theme.
	Palette *theme.Palette
}

// DefaultWidth is the wrap width when Options.Width is unset.
const DefaultWidth = 80

// minWidth keeps a pathological pane width from starving the layout.
const minWidth = 4

// Link is one hyperlink of the rendered document.
type Link struct {
	Label     string // the link's visible text, whitespace-collapsed
	Href      string // the href attribute, as written
	FirstLine int    // first rendered line the label occupies
	LastLine  int    // last rendered line (differs when the label wraps)
}

// Image is one <img> of the rendered document.
type Image struct {
	Src  string
	Alt  string
	Line int // rendered line of its placeholder
}

// Document is a rendered HTML document.
type Document struct {
	Title  string   // the <title> text, whitespace-collapsed
	Lines  []string // styled lines, each at most Options.Width cells wide
	Links  []Link   // in reading order
	Images []Image  // in reading order
	// Anchors maps an element id (or an <a name>) to the rendered line its
	// content starts on — where an in-document "#anchor" link lands.
	Anchors map[string]int
	sourceMap
}

// Render lays doc out at opts.Width. It never fails: malformed markup renders
// whatever structure could be recovered.
func Render(doc []byte, opts Options) Document {
	width := opts.Width
	if width <= 0 {
		width = DefaultWidth
	}
	r := &renderer{
		t:       parse(doc),
		width:   max(width, minWidth),
		sty:     newStyles(opts.Palette),
		sgr:     map[style]string{},
		blank:   -1,
		anchorL: map[string]int{},
	}
	r.walk(r.t.root, ctx{link: -1})
	r.flush()
	for _, id := range r.anchors {
		r.claimAnchor(id, max(0, len(r.lines)-1))
	}
	out := Document{Title: r.title, Lines: r.lines, Images: r.images, Anchors: r.anchorL}
	for i := range r.links {
		l := r.links[i].Link
		if l.FirstLine < 0 {
			continue // nothing of it was rendered
		}
		l.Label = strings.TrimSpace(string(r.links[i].label))
		out.Links = append(out.Links, l)
	}
	out.sourceMap = newSourceMap(doc, r.src)
	return out
}

// fragKind distinguishes the pieces of an inline flow.
type fragKind uint8

const (
	fWord  fragKind = iota // unbreakable text
	fSpace                 // a collapsed run of whitespace: a break opportunity
	fBreak                 // <br> (or a newline inside <pre>): a forced line end
)

// frag is one piece of inline content waiting for layout — and, once laid
// out, one run of a rendered line (prefix runs included).
type frag struct {
	kind    fragKind
	text    string
	w       int // display width of text
	st      style
	link    int // index into renderer.links, -1 for none
	img     int // index into renderer.images, -1 for none
	off     int // source byte offset, -1 for none
	anchors []string
}

// indent is one level of block indentation: the text a line opens with at
// this level. The first line of the block shows first (a list marker), later
// lines rest (the same width in spaces); bar is what a separator blank line
// keeps of it (the blockquote rule).
type indent struct {
	first, rest string
	w           int
	st          style
	bar         string
	used        bool
}

// listState numbers the items of one open list.
type listState struct {
	ordered bool
	next    int
	step    int
	width   int // marker width, so "9." and "10." align
}

// ctx is the inline context an element hands its children.
type ctx struct {
	st   style
	link int
}

type linkBuild struct {
	Link
	open  string // the OSC 8 sequence opening the link
	label []byte
	gap   bool // a space belonging to the link is pending in the label
}

type renderer struct {
	t     *tree
	width int
	sty   styles
	sgr   map[style]string

	lines     []string
	src       []int
	prefix    []indent
	blank     int // indent depth of a pending blank line, -1 for none
	lastBlank bool

	frags    []frag
	space    bool // a collapsed space is pending before the next word
	spaceCtx ctx  // the look of that space (the whitespace's own element)
	anchors  []string
	pre      int
	preStart bool // the next text is the first inside <pre>

	lists   []listState
	cells   []int // placeholder tables: cells seen per open row
	title   string
	links   []linkBuild
	images  []Image
	anchorL map[string]int
}

// walk renders n's children.
func (r *renderer) walk(n *html.Node, c ctx) {
	for k := n.FirstChild; k != nil; k = k.NextSibling {
		switch k.Type {
		case html.TextNode:
			r.text(k, c)
		case html.ElementNode:
			r.element(k, c)
		}
	}
}

// skipped are the elements a reading view never shows: scripts and styles,
// metadata, embedded content with no text form, and the parts of form
// controls that only make sense in an interactive widget.
var skipped = map[string]bool{
	"script": true, "style": true, "template": true, "noscript": true,
	"meta": true, "link": true, "base": true, "svg": true, "iframe": true,
	"object": true, "embed": true, "canvas": true, "audio": true,
	"video": true, "datalist": true, "option": true, "optgroup": true,
	"param": true, "source": true, "track": true, "map": true, "area": true,
	"colgroup": true, "col": true,
}

// blockTags are the generic containers: each starts and ends a line but adds
// no margin or indent of its own.
var blockTags = map[string]bool{
	"address": true, "article": true, "aside": true, "body": true,
	"center": true, "dialog": true, "div": true, "fieldset": true,
	"footer": true, "form": true, "header": true, "hgroup": true,
	"html": true, "legend": true, "main": true, "nav": true, "search": true,
	"section": true,
}

func (r *renderer) element(n *html.Node, c ctx) {
	tag := n.Data
	if _, hidden := attr(n, "hidden"); hidden || skipped[tag] {
		return
	}
	r.preStart = false
	off := r.t.start[n]
	if id, _ := attr(n, "id"); id != "" {
		r.anchor(id)
	}
	switch tag {
	case "head":
		r.captureTitle(n)
	case "title":
		if r.title == "" {
			r.title = collapse(textContent(n))
		}
	case "h1", "h2", "h3", "h4", "h5", "h6":
		r.heading(n, c, int(tag[1]-'0'))
	case "p":
		r.gap()
		r.walk(n, c)
		r.gap()
	case "pre":
		r.gap()
		r.push(indent{first: "  ", rest: "  ", w: 2})
		r.pre++
		r.preStart = true
		c.st = r.sty.pre
		r.walk(n, c)
		r.flush()
		r.pre--
		r.pop()
		r.gap()
	case "blockquote":
		r.gap()
		r.push(indent{first: "│ ", rest: "│ ", w: 2, st: r.sty.quote, bar: "│"})
		r.walk(n, c)
		r.flush()
		r.pop()
		r.gap()
	case "ul", "ol", "menu", "dir":
		r.list(n, c)
	case "li":
		r.item(n, c)
	case "dl", "details", "table":
		r.gap()
		r.walk(n, c)
		r.gap()
	case "dt":
		r.flush()
		r.walk(n, ctx{st: c.st.with(attrBold), link: c.link})
		r.flush()
	case "dd":
		r.indented(n, c, 4)
	case "figure":
		r.gap()
		r.indented(n, c, 2)
		r.gap()
	case "figcaption", "caption":
		r.flush()
		r.walk(n, ctx{st: c.st.with(r.sty.caption.attrs), link: c.link})
		r.flush()
	case "summary":
		r.flush()
		r.word("▾", off, ctx{st: r.sty.summary, link: -1}, -1)
		r.softSpace(c)
		r.walk(n, ctx{st: c.st.with(attrBold), link: c.link})
		r.flush()
	case "hr":
		r.gap()
		avail := r.avail()
		r.emit([]frag{{text: strings.Repeat("─", avail), w: avail, st: r.sty.rule, link: -1, img: -1, off: off}}, off)
		r.gap()
	case "br":
		r.lineBreak(off)
	case "img":
		r.image(n, c)
	case "a":
		r.link(n, c)
	case "tr":
		r.flush()
		r.cells = append(r.cells, 0)
		r.walk(n, c)
		r.cells = r.cells[:len(r.cells)-1]
		r.flush()
	case "td", "th":
		r.cell(n, c)
	case "thead":
		r.walk(n, ctx{st: c.st.with(attrBold), link: c.link})
	case "strong", "b":
		r.walk(n, ctx{st: c.st.with(attrBold), link: c.link})
	case "em", "i", "cite", "dfn", "var":
		r.walk(n, ctx{st: c.st.with(attrItalic), link: c.link})
	case "u", "ins":
		r.walk(n, ctx{st: c.st.with(attrUnderline), link: c.link})
	case "s", "del", "strike":
		r.walk(n, ctx{st: c.st.with(attrStrike), link: c.link})
	case "mark":
		c.st.fg, c.st.bg = r.sty.mark.fg, r.sty.mark.bg
		r.walk(n, c)
	case "code", "kbd", "samp", "tt":
		if r.pre == 0 {
			c.st.fg, c.st.bg = r.sty.code.fg, r.sty.code.bg
		}
		r.walk(n, c)
	case "q":
		r.word("“", off, c, -1)
		r.walk(n, c)
		r.space = false
		r.word("”", off, c, -1)
	case "input":
		r.input(n, c)
	case "select":
		r.words("["+collapse(selectedOption(n))+" ▾]", off, c, -1)
	default:
		if blockTags[tag] {
			r.flush()
			r.walk(n, c)
			r.flush()
			return
		}
		// Inline or unknown (custom elements are inline, as in a browser).
		r.walk(n, c)
	}
}

// heading renders h1 as an accent block and the lower levels behind their
// "##" marker, matching the markdown preview.
func (r *renderer) heading(n *html.Node, c ctx, level int) {
	r.gap()
	off := r.t.start[n]
	start := len(r.frags)
	if level == 1 {
		c.st = r.sty.h1
		r.word(" ", off, c, -1)
	} else {
		c.st = r.sty.heading
		r.word(strings.Repeat("#", level), off, c, -1)
		r.softSpace(c)
	}
	marker := len(r.frags)
	r.walk(n, c)
	switch {
	case len(r.frags) == marker:
		r.frags = r.frags[:start] // an empty heading shows nothing
	case level == 1:
		r.space = false
		r.word(" ", off, c, -1)
	}
	r.gap()
}

// list renders ul/ol. A top-level list is a margin block; a nested one sits
// directly under its parent item.
func (r *renderer) list(n *html.Node, c ctx) {
	nested := len(r.lists) > 0
	if nested {
		r.flush()
	} else {
		r.gap()
	}
	ls := listState{ordered: n.Data == "ol", next: 1, step: 1}
	if ls.ordered {
		count := 0
		for k := n.FirstChild; k != nil; k = k.NextSibling {
			if k.Type == html.ElementNode && k.Data == "li" {
				count++
			}
		}
		if _, ok := attr(n, "reversed"); ok {
			ls.step, ls.next = -1, max(count, 1)
		}
		if v, ok := attr(n, "start"); ok {
			if s, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				ls.next = s
			}
		}
		last := ls.next + ls.step*max(count-1, 0)
		ls.width = max(len(strconv.Itoa(ls.next)), len(strconv.Itoa(last))) + 1
	}
	r.lists = append(r.lists, ls)
	r.walk(n, c)
	r.flush()
	r.lists = r.lists[:len(r.lists)-1]
	if !nested {
		r.gap()
	}
}

// item renders one li behind its marker; later lines hang under the text.
func (r *renderer) item(n *html.Node, c ctx) {
	r.flush()
	marker := "•"
	if k := len(r.lists); k > 0 && r.lists[k-1].ordered {
		ls := &r.lists[k-1]
		if v, ok := attr(n, "value"); ok {
			if s, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				ls.next = s
			}
		}
		marker = strconv.Itoa(ls.next) + "."
		if pad := ls.width - len(marker); pad > 0 {
			marker = strings.Repeat(" ", pad) + marker
		}
		ls.next += ls.step
	}
	w := ansi.StringWidth(marker) + 1
	r.push(indent{first: marker + " ", rest: strings.Repeat(" ", w), w: w})
	r.walk(n, c)
	r.flush()
	if !r.prefix[len(r.prefix)-1].used {
		r.emit(nil, r.t.start[n]) // an empty item still shows its marker
	}
	r.pop()
}

// indented renders n's content indented by w columns.
func (r *renderer) indented(n *html.Node, c ctx, w int) {
	r.flush()
	pad := strings.Repeat(" ", w)
	r.push(indent{first: pad, rest: pad, w: w})
	r.walk(n, c)
	r.flush()
	r.pop()
}

// cell renders a table cell as part of its row's line (placeholder table
// layout until 0530/4): cells after the first are set off by a "│".
func (r *renderer) cell(n *html.Node, c ctx) {
	if k := len(r.cells); k > 0 {
		if r.cells[k-1] > 0 {
			r.softSpace(c)
			r.word("│", r.t.start[n], ctx{st: r.sty.sep, link: -1}, -1)
			r.softSpace(c)
		}
		r.cells[k-1]++
	}
	if n.Data == "th" {
		c.st = c.st.with(attrBold)
	}
	r.walk(n, c)
}

// link renders an <a>: with an href its content becomes one indexed,
// hyperlinked run; without one it is plain inline content (a named anchor).
func (r *renderer) link(n *html.Node, c ctx) {
	if name, _ := attr(n, "name"); name != "" {
		r.anchor(name)
	}
	href, ok := attr(n, "href")
	if !ok {
		r.walk(n, c)
		return
	}
	href = sanitize(strings.TrimSpace(href), false)
	id := len(r.links)
	r.links = append(r.links, linkBuild{
		Link: Link{Href: href, FirstLine: -1, LastLine: -1},
		open: ansi.SetHyperlink(href, "id=ike-"+strconv.Itoa(id)),
	})
	c.link = id
	c.st.fg = r.sty.link.fg
	c.st = c.st.with(r.sty.link.attrs)
	r.walk(n, c)
}

// image renders an <img> as its bracketed alt text (or file name) and
// indexes it.
func (r *renderer) image(n *html.Node, c ctx) {
	src, _ := attr(n, "src")
	alt, _ := attr(n, "alt")
	src = sanitize(strings.TrimSpace(src), false)
	alt = collapse(sanitize(alt, false))
	id := len(r.images)
	r.images = append(r.images, Image{Src: src, Alt: alt, Line: -1})
	label := alt
	if label == "" {
		label = "image: " + imageName(src)
	}
	st := r.sty.image
	if c.link >= 0 {
		st = st.with(attrUnderline)
	}
	r.words("["+label+"]", r.t.start[n], ctx{st: st, link: c.link}, id)
}

// imageName is the short name an alt-less image is shown by.
func imageName(src string) string {
	if src == "" {
		return "image"
	}
	if strings.HasPrefix(src, "data:") {
		return "data"
	}
	if i := strings.IndexAny(src, "?#"); i >= 0 {
		src = src[:i]
	}
	if name := path.Base(src); name != "." && name != "/" {
		return name
	}
	return src
}

// input renders a form control as the text a reader sees of it.
func (r *renderer) input(n *html.Node, c ctx) {
	typ, _ := attr(n, "type")
	value, _ := attr(n, "value")
	_, checked := attr(n, "checked")
	off := r.t.start[n]
	switch strings.ToLower(typ) {
	case "hidden":
	case "checkbox":
		r.word(map[bool]string{true: "[x]", false: "[ ]"}[checked], off, c, -1)
	case "radio":
		r.word(map[bool]string{true: "(•)", false: "( )"}[checked], off, c, -1)
	case "submit", "reset", "button":
		if value == "" {
			value = typ
		}
		r.words("["+collapse(value)+"]", off, c, -1)
	case "image":
		alt, _ := attr(n, "alt")
		r.words("["+collapse(alt)+"]", off, c, -1)
	default:
		if value == "" {
			value, _ = attr(n, "placeholder")
		}
		if value = collapse(value); value == "" {
			value = "_____"
		}
		r.words("["+value+"]", off, c, -1)
	}
}

// selectedOption is the text a closed <select> shows.
func selectedOption(n *html.Node) string {
	var first *html.Node
	var find func(*html.Node) *html.Node
	find = func(p *html.Node) *html.Node {
		for k := p.FirstChild; k != nil; k = k.NextSibling {
			if k.Type != html.ElementNode {
				continue
			}
			if k.Data == "option" {
				if first == nil {
					first = k
				}
				if _, ok := attr(k, "selected"); ok {
					return k
				}
			}
			if o := find(k); o != nil {
				return o
			}
		}
		return nil
	}
	if o := find(n); o != nil {
		return textContent(o)
	}
	if first != nil {
		return textContent(first)
	}
	return ""
}

// captureTitle takes the <title> out of a <head>, whose other content never
// renders.
func (r *renderer) captureTitle(n *html.Node) {
	for k := n.FirstChild; k != nil; k = k.NextSibling {
		if k.Type != html.ElementNode {
			continue
		}
		if k.Data == "title" && r.title == "" {
			r.title = collapse(textContent(k))
		}
		r.captureTitle(k)
	}
}

// textContent concatenates the text below n.
func textContent(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(p *html.Node) {
		for k := p.FirstChild; k != nil; k = k.NextSibling {
			if k.Type == html.TextNode {
				b.WriteString(k.Data)
			}
			walk(k)
		}
	}
	walk(n)
	return sanitize(b.String(), false)
}

// collapse applies HTML whitespace collapsing to a whole string.
func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// --- inline flow ---

// text adds a text node to the flow: word by word with collapsed whitespace,
// or verbatim inside <pre>. Words are cut from the raw source bytes and
// decoded one by one, so each keeps its exact source offset.
func (r *renderer) text(n *html.Node, c ctx) {
	raw, base := r.t.raw(n), r.t.start[n]
	if r.pre > 0 {
		r.preText(raw, base, c)
		return
	}
	for i := 0; i < len(raw); {
		if isSpace(raw[i]) {
			r.softSpace(c)
			i++
			continue
		}
		j := i
		for j < len(raw) && !isSpace(raw[j]) {
			j++
		}
		r.word(decode(raw[i:j], false), base+i, c, -1)
		i = j
	}
}

// preText adds preformatted text: every source line a run, every newline a
// forced break. The newline directly after <pre> is dropped, as HTML does.
func (r *renderer) preText(raw []byte, base int, c ctx) {
	if r.preStart {
		r.preStart = false
		switch {
		case bytes.HasPrefix(raw, []byte("\r\n")):
			raw, base = raw[2:], base+2
		case bytes.HasPrefix(raw, []byte("\n")):
			raw, base = raw[1:], base+1
		}
	}
	for len(raw) > 0 {
		line, rest, nl := bytes.Cut(raw, []byte("\n"))
		if text := decode(bytes.TrimSuffix(line, []byte("\r")), true); text != "" {
			r.add(frag{kind: fWord, text: text, w: width(text), st: c.st, link: c.link, img: -1, off: base}, c)
		}
		if nl {
			r.frags = append(r.frags, frag{kind: fBreak, off: base + len(line), link: -1, img: -1})
		}
		base += len(line) + 1
		raw = rest
	}
}

// isSpace reports HTML's ASCII whitespace.
func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f'
}

// decode turns raw source text into display text: entities resolved,
// control characters (terminal escapes among them) neutralised.
func decode(raw []byte, keepTab bool) string {
	s := string(raw)
	if bytes.IndexByte(raw, '&') >= 0 {
		s = html.UnescapeString(s)
	}
	return sanitize(s, keepTab)
}

// sanitize replaces control characters — C0 (tabs kept on request), DEL and
// C1 — with spaces, so document text can never smuggle an escape sequence
// into the terminal, and a non-breaking space with a plain one (it stays
// unbreakable: it lives inside a word). Invalid UTF-8 becomes U+FFFD, so
// every run measures the same alone as inside its line.
func sanitize(s string, keepTab bool) string {
	s = strings.ToValidUTF8(s, "�")
	clean := true
	for i := 0; i < len(s); i++ {
		if b := s[i]; b < 0x20 || b == 0x7f || b == 0xc2 {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' && keepTab:
			return r
		case r < 0x20, r >= 0x7f && r <= 0x9f, r == 0xa0:
			return ' '
		}
		return r
	}, s)
}

// width is the display width of plain text, with an ASCII fast path.
func width(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return ansi.StringWidth(s)
		}
	}
	return len(s)
}

// softSpace records collapsible whitespace: one space before the next word,
// in the look of the element the whitespace belongs to.
func (r *renderer) softSpace(c ctx) {
	if !r.space {
		r.space, r.spaceCtx = true, c
	}
}

// word appends one unbreakable word, preceded by the pending space unless it
// would open a line.
func (r *renderer) word(text string, off int, c ctx, img int) {
	if text == "" {
		return
	}
	if r.space {
		r.space = false
		if n := len(r.frags); n > 0 && r.frags[n-1].kind != fBreak {
			sc := r.spaceCtx
			r.add(frag{kind: fSpace, text: " ", w: 1, st: sc.st, link: sc.link, img: -1, off: off}, sc)
		}
	}
	r.add(frag{kind: fWord, text: text, w: width(text), st: c.st, link: c.link, img: img, off: off}, c)
}

// words appends text split at its spaces, all words at offset off.
func (r *renderer) words(text string, off int, c ctx, img int) {
	for i, w := range strings.Fields(text) {
		if i > 0 {
			r.softSpace(c)
		}
		r.word(w, off, c, img)
	}
}

// add appends a fragment, handing it the pending anchors and extending its
// link's label.
func (r *renderer) add(f frag, c ctx) {
	if f.kind == fWord && len(r.anchors) > 0 {
		f.anchors, r.anchors = r.anchors, nil
	}
	if c.link >= 0 {
		l := &r.links[c.link]
		if f.kind == fSpace {
			l.gap = true
		} else {
			if l.gap && len(l.label) > 0 {
				l.label = append(l.label, ' ')
			}
			l.gap = false
			l.label = append(l.label, f.text...)
		}
	}
	r.frags = append(r.frags, f)
}

// lineBreak is <br>: the current line ends here.
func (r *renderer) lineBreak(off int) {
	r.space = false
	r.frags = append(r.frags, frag{kind: fBreak, off: off, link: -1, img: -1})
}

// anchor queues an id for the next rendered content.
func (r *renderer) anchor(id string) {
	if _, ok := r.anchorL[id]; !ok {
		r.anchors = append(r.anchors, id)
	}
}

func (r *renderer) claimAnchor(id string, line int) {
	if _, ok := r.anchorL[id]; !ok {
		r.anchorL[id] = line
	}
}

// --- block layout ---

func (r *renderer) push(in indent) { r.prefix = append(r.prefix, in) }
func (r *renderer) pop()           { r.prefix = r.prefix[:len(r.prefix)-1] }

// gap ends the current block and asks for a blank line before the next
// content. Adjacent margins collapse into one blank line, drawn at the
// shallowest indent that asked for it, so a blockquote's surrounding margin
// stays outside its bar.
func (r *renderer) gap() {
	r.flush()
	if d := len(r.prefix); r.blank < 0 || d < r.blank {
		r.blank = d
	}
}

// flush lays out the pending inline content of the current block.
func (r *renderer) flush() {
	r.space = false
	if len(r.frags) == 0 {
		return
	}
	if r.pre > 0 {
		r.layoutPre(r.frags)
	} else {
		r.layoutFlow(r.frags)
	}
	r.frags = r.frags[:0]
}

// maxPrefix is the widest indent a line may carry; deeper nesting is cut
// from the left so the content keeps a readable column.
func (r *renderer) maxPrefix() int {
	return r.width - max(1, min(20, r.width/2))
}

// avail is the content width left beside the current indent.
func (r *renderer) avail() int {
	w := 0
	for _, in := range r.prefix {
		w += in.w
	}
	return r.width - min(w, r.maxPrefix())
}

// layoutFlow word-wraps inline content at the available width. A word wider
// than a whole line is broken at the cell boundary.
func (r *renderer) layoutFlow(frags []frag) {
	avail := r.avail()
	var line []frag
	w := 0
	var sp *frag
	emit := func(fallback int) {
		r.emit(line, lineSource(line, fallback))
		line, w, sp = line[:0], 0, nil
	}
	for i := 0; i < len(frags); {
		switch frags[i].kind {
		case fBreak:
			emit(frags[i].off)
			i++
			continue
		case fSpace:
			if w > 0 {
				sp = &frags[i]
			}
			i++
			continue
		}
		j, gw := i, 0
		for j < len(frags) && frags[j].kind == fWord {
			gw += frags[j].w
			j++
		}
		need := gw
		if sp != nil {
			need++
		}
		if w > 0 && w+need > avail {
			emit(-1)
		}
		if sp != nil {
			line = append(line, *sp)
			w += sp.w
			sp = nil
		}
		for k := i; k < j; k++ {
			f := frags[k]
			for f.w > avail-w {
				if w >= avail {
					emit(-1)
					continue
				}
				head, tail := splitCells(f.text, avail-w)
				if head == "" {
					if w > 0 {
						emit(-1)
						continue
					}
					head, tail = firstRune(f.text)
				}
				h := f
				h.text, h.w = head, width(head)
				line = append(line, h)
				emit(-1)
				f.text, f.w, f.anchors = tail, width(tail), nil
			}
			line = append(line, f)
			w += f.w
		}
		i = j
	}
	if len(line) > 0 {
		emit(-1)
	}
}

// layoutPre lays out preformatted content: one line per source line, never
// wrapped, a line overflowing the width cut with the hscroll marker.
func (r *renderer) layoutPre(frags []frag) {
	avail := r.avail()
	var line []frag
	w := 0
	emit := func(fallback int) {
		if _, right := hscroll.Cut(0, avail, w); right {
			line = cutFrags(line, avail-1)
			line = append(line, frag{text: hscroll.RightGlyph, w: 1, st: r.sty.edge, link: -1, img: -1, off: -1})
		}
		r.emit(line, lineSource(line, fallback))
		line, w = line[:0], 0
	}
	for _, f := range frags {
		if f.kind == fBreak {
			emit(f.off)
			continue
		}
		f.text = expandTabs(f.text, w)
		f.w = width(f.text)
		line = append(line, f)
		w += f.w
	}
	if len(line) > 0 {
		emit(-1)
	}
}

// lineSource is the source offset a laid-out line maps to: its first word's.
func lineSource(line []frag, fallback int) int {
	for _, f := range line {
		if f.kind == fWord && f.off >= 0 {
			return f.off
		}
	}
	for _, f := range line {
		if f.off >= 0 {
			return f.off
		}
	}
	return fallback
}

// expandTabs expands tabs to 8-column stops, col being the line's width so far.
func expandTabs(s string, col int) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	var b strings.Builder
	for _, ch := range s {
		if ch == '\t' {
			n := 8 - (col % 8)
			b.WriteString(strings.Repeat(" ", n))
			col += n
			continue
		}
		b.WriteRune(ch)
		col += width(string(ch))
	}
	return b.String()
}

// splitCells splits plain text after at most n display cells.
func splitCells(s string, n int) (head, tail string) {
	if n <= 0 {
		return "", s
	}
	if w := width(s); w == len(s) { // ASCII
		if n >= len(s) {
			return s, ""
		}
		return s[:n], s[n:]
	}
	head = ansi.Truncate(s, n, "")
	if !strings.HasPrefix(s, head) {
		return "", s
	}
	return head, s[len(head):]
}

// firstRune splits off s's first rune — the progress guarantee when not even
// one character fits.
func firstRune(s string) (string, string) {
	for i := range s {
		if i > 0 {
			return s[:i], s[i:]
		}
	}
	return s, ""
}

// cutFrags keeps the first n display cells of a line.
func cutFrags(line []frag, n int) []frag {
	out := make([]frag, 0, len(line))
	w := 0
	for _, f := range line {
		if w+f.w <= n {
			out = append(out, f)
			w += f.w
			continue
		}
		if head, _ := splitCells(f.text, n-w); head != "" {
			f.text, f.w = head, width(head)
			out = append(out, f)
			w += f.w
		}
		break
	}
	if w < n {
		out = append(out, frag{text: strings.Repeat(" ", n-w), w: n - w, link: -1, img: -1, off: -1})
	}
	return out
}

// emit writes one content line: the pending blank line first, then the
// indent (consuming list markers), then body. It records where the line's
// links, images and anchors landed.
func (r *renderer) emit(body []frag, src int) {
	if r.blank >= 0 {
		if len(r.lines) > 0 && !r.lastBlank {
			r.lines = append(r.lines, r.blankLine(min(r.blank, len(r.prefix))))
			r.src = append(r.src, -1)
		}
		r.blank = -1
	}
	idx := len(r.lines)
	line := r.compose(append(r.prefixFrags(), body...))
	r.lines = append(r.lines, line)
	r.src = append(r.src, src)
	r.lastBlank = false
	for _, f := range body {
		if f.link >= 0 {
			l := &r.links[f.link]
			if l.FirstLine < 0 {
				l.FirstLine = idx
			}
			l.LastLine = idx
		}
		if f.img >= 0 && r.images[f.img].Line < 0 {
			r.images[f.img].Line = idx
		}
		for _, id := range f.anchors {
			r.claimAnchor(id, idx)
		}
	}
	for _, id := range r.anchors {
		r.claimAnchor(id, idx)
	}
	r.anchors = nil
}

// prefixFrags is the indent of the next content line: each level's first
// text once, its rest text after. Nesting deeper than maxPrefix loses its
// leftmost columns.
func (r *renderer) prefixFrags() []frag {
	var out []frag
	w := 0
	for i := range r.prefix {
		in := &r.prefix[i]
		text := in.rest
		if !in.used {
			text, in.used = in.first, true
		}
		out = append(out, frag{text: text, w: in.w, st: in.st, link: -1, img: -1, off: -1})
		w += in.w
	}
	for drop := w - r.maxPrefix(); drop > 0 && len(out) > 0; {
		if out[0].w <= drop {
			drop -= out[0].w
			out = out[1:]
			continue
		}
		_, tail := splitCells(out[0].text, drop)
		out[0].text, out[0].w = tail, width(tail)
		break
	}
	return out
}

// blankLine is a separator line at indent depth d: empty, except that a
// blockquote bar at or below d carries on through it.
func (r *renderer) blankLine(d int) string {
	k := -1
	for i := d - 1; i >= 0; i-- {
		if r.prefix[i].bar != "" {
			k = i
			break
		}
	}
	if k < 0 {
		return ""
	}
	var fs []frag
	for i := 0; i < k; i++ {
		fs = append(fs, frag{text: r.prefix[i].rest, w: r.prefix[i].w, link: -1, st: r.prefix[i].st})
	}
	fs = append(fs, frag{text: r.prefix[k].bar, w: width(r.prefix[k].bar), link: -1, st: r.prefix[k].st})
	return r.compose(fs)
}

// compose renders runs into one styled line: adjacent runs with the same look
// and link merge, every link is wrapped in its OSC 8 sequence.
func (r *renderer) compose(fs []frag) string {
	var b strings.Builder
	cur := -1
	for i := 0; i < len(fs); {
		f := fs[i]
		j := i + 1
		for j < len(fs) && fs[j].st == f.st && fs[j].link == f.link {
			j++
		}
		if f.link != cur {
			if cur >= 0 {
				b.WriteString(ansi.ResetHyperlink())
			}
			if f.link >= 0 {
				b.WriteString(r.links[f.link].open)
			}
			cur = f.link
		}
		sgr, ok := r.sgr[f.st]
		if !ok {
			sgr = f.st.sgr()
			r.sgr[f.st] = sgr
		}
		b.WriteString(sgr)
		for k := i; k < j; k++ {
			b.WriteString(fs[k].text)
		}
		if sgr != "" {
			b.WriteString(sgrReset)
		}
		i = j
	}
	if cur >= 0 {
		b.WriteString(ansi.ResetHyperlink())
	}
	return b.String()
}
