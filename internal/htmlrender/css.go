package htmlrender

import (
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/andybalholm/cascadia"
	"golang.org/x/image/colornames"
	"golang.org/x/net/html"
)

// The minimal CSS subset of the reading view (0530/6, #2744): enough for
// hidden elements to stay hidden and simple emphasis to survive, no layout
// engine. Declarations come from <style> blocks — selectors matched with
// cascadia, the DOM inspector's engine — and from inline style="" attributes.
//
// Supported properties:
//
//   - display:none, visibility:hidden (and collapse): the element is not
//     rendered; a later display/visibility value on the same element
//     reverts it.
//   - font-weight: bold, bolder or a number >= 600 turn bold on; normal,
//     lighter or a smaller number turn it off.
//   - font-style: italic/oblique on, normal off.
//   - text-decoration, text-decoration-line: underline and line-through add
//     their attribute, none clears both (a link's underline included).
//   - color: a named colour, #rgb, #rgba, #rrggbb, #rrggbbaa, rgb()/rgba() —
//     emitted as true colour; the terminal layer (bubbletea's screen)
//     downsamples it to the nearest colour of a 256- or 16-colour terminal,
//     as it does the theme's colours. Fully transparent colours are ignored.
//   - text-align: center and right (end) on block elements; left, start and
//     justify reset an inherited alignment.
//
// The cascade: stylesheet rules sort by cascadia's specificity, then by
// source order; inline style wins over any rule; !important declarations
// win over normal ones (inline ones over the stylesheet's). A property the
// renderer cannot use is ignored; an invalid value leaves the property as
// it was. Everything inherits along the render walk, so the element's look
// reaches its whole content.
//
// A stylesheet that fails to parse — unbalanced braces, an unterminated
// comment or string — is ignored as a whole; the document still renders. A
// rule whose selector cascadia rejects (a pseudo-element) is dropped alone,
// as a browser drops it; dynamic pseudo-classes (:hover, :focus) parse but
// never match. @media, @import, @font-face and every other at-rule are
// skipped, as are <style media="..."> blocks for anything but screen/all;
// external stylesheets are never fetched.

// Values of ctx.align and cssProps.align.
const (
	alignNone uint8 = iota
	alignLeft
	alignCenter
	alignRight
)

// decl is one property declaration.
type decl struct {
	prop, val string
	important bool
}

// cssRule is one stylesheet rule.
type cssRule struct {
	sel   cascadia.SelectorGroup
	decls []decl
}

// cssProps is the computed subset for one element.
type cssProps struct {
	displayNone, visHidden bool
	weight, italic         int8  // 1 on, -1 off, 0 unset
	decoSet                bool  // text-decoration was given
	deco                   uint8 // its attrUnderline/attrStrike bits (0: none)
	fg                     uint32
	align                  uint8
}

// cascade maps every element with declarations to its computed props. A nil
// cascade (a document without CSS) matches nothing.
type cascade map[*html.Node]*cssProps

// newCascade computes the CSS of t's elements: nil when the document has no
// <style> block and no style attribute, so plain documents pay nothing.
func newCascade(t *tree) cascade {
	type match struct {
		n         *html.Node
		important bool
		inline    bool
		spec      cascadia.Specificity
		order     int
		decl      decl
	}
	var matches []match
	order := 0
	var sheets []string
	var inline []*html.Node
	var find func(*html.Node)
	find = func(p *html.Node) {
		for k := p.FirstChild; k != nil; k = k.NextSibling {
			if k.Type != html.ElementNode {
				continue
			}
			if k.Data == "style" {
				if screenMedia(k) {
					sheets = append(sheets, rawText(k))
				}
				continue
			}
			if _, ok := attr(k, "style"); ok {
				inline = append(inline, k)
			}
			find(k)
		}
	}
	find(t.root)
	if len(sheets) == 0 && len(inline) == 0 {
		return nil
	}
	var idx selIndex
	for _, s := range sheets {
		rules, ok := parseSheet(s)
		if !ok {
			continue
		}
		for _, rule := range rules {
			for _, sel := range rule.sel {
				idx.add(indexedSel{sel: sel, spec: sel.Specificity(), order: order, decls: rule.decls})
			}
			order++
		}
	}
	idx.match(t.root, func(n *html.Node, s *indexedSel) {
		for _, d := range s.decls {
			matches = append(matches, match{n: n, important: d.important, spec: s.spec, order: s.order, decl: d})
		}
	})
	for _, n := range inline {
		v, _ := attr(n, "style")
		for _, d := range parseDecls(v) {
			matches = append(matches, match{n: n, important: d.important, inline: true, order: order, decl: d})
		}
	}
	if len(matches) == 0 {
		return nil
	}
	sort.SliceStable(matches, func(i, j int) bool {
		a, b := matches[i], matches[j]
		if a.important != b.important {
			return !a.important
		}
		if a.inline != b.inline {
			return !a.inline
		}
		if a.spec != b.spec {
			return a.spec.Less(b.spec)
		}
		return a.order < b.order
	})
	c := cascade{}
	for _, m := range matches {
		p := c[m.n]
		if p == nil {
			p = &cssProps{}
			c[m.n] = p
		}
		p.set(m.decl)
	}
	return c
}

// screenMedia reports whether a <style> block applies to a screen: no media
// attribute, or one naming screen or all.
func screenMedia(n *html.Node) bool {
	m, ok := attr(n, "media")
	if !ok || strings.TrimSpace(m) == "" {
		return true
	}
	m = strings.ToLower(m)
	return strings.Contains(m, "screen") || strings.Contains(m, "all")
}

// rawText is the verbatim text inside a raw-text element (<style>).
func rawText(n *html.Node) string {
	var b strings.Builder
	for k := n.FirstChild; k != nil; k = k.NextSibling {
		if k.Type == html.TextNode {
			b.WriteString(k.Data)
		}
	}
	return b.String()
}

// hidden reports whether CSS hides n.
func (c cascade) hidden(n *html.Node) bool {
	p := c[n]
	return p != nil && (p.displayNone || p.visHidden)
}

// apply returns the inline context of n's content: c with n's CSS look.
func (c cascade) apply(n *html.Node, x ctx) ctx {
	p := c[n]
	if p == nil {
		return x
	}
	switch p.weight {
	case 1:
		x.st.attrs |= attrBold
	case -1:
		x.st.attrs &^= attrBold
	}
	switch p.italic {
	case 1:
		x.st.attrs |= attrItalic
	case -1:
		x.st.attrs &^= attrItalic
	}
	if p.decoSet {
		if p.deco == 0 {
			x.st.attrs &^= attrUnderline | attrStrike
		}
		x.st.attrs |= p.deco
	}
	if p.fg != 0 {
		x.st.fg = p.fg
	}
	if p.align != alignNone && alignBlock(n.Data) {
		x.align = p.align
	}
	return x
}

// alignBlock reports whether text-align applies to a tag: the block
// containers, not inline elements.
func alignBlock(tag string) bool {
	if blockTags[tag] {
		return true
	}
	switch tag {
	case "p", "h1", "h2", "h3", "h4", "h5", "h6", "li", "dd", "dt", "dl",
		"ul", "ol", "menu", "dir", "blockquote", "pre", "figure",
		"figcaption", "caption", "details", "summary", "table", "thead",
		"tbody", "tfoot", "tr", "td", "th":
		return true
	}
	return false
}

// set applies one declaration; an unknown property or invalid value is a
// no-op.
func (p *cssProps) set(d decl) {
	v := strings.ToLower(d.val)
	switch d.prop {
	case "display":
		if v != "" && !strings.HasPrefix(v, "var(") {
			p.displayNone = v == "none"
		}
	case "visibility":
		switch v {
		case "hidden", "collapse":
			p.visHidden = true
		case "visible":
			p.visHidden = false
		}
	case "font-weight":
		switch v {
		case "bold", "bolder":
			p.weight = 1
		case "normal", "lighter":
			p.weight = -1
		default:
			if w, err := strconv.ParseFloat(v, 64); err == nil && w >= 1 && w <= 1000 {
				p.weight = map[bool]int8{true: 1, false: -1}[w >= 600]
			}
		}
	case "font-style":
		switch {
		case v == "italic" || strings.HasPrefix(v, "oblique"):
			p.italic = 1
		case v == "normal":
			p.italic = -1
		}
	case "text-decoration", "text-decoration-line":
		var bits uint8
		none := false
		for _, f := range strings.Fields(v) {
			switch f {
			case "underline":
				bits |= attrUnderline
			case "line-through":
				bits |= attrStrike
			case "none":
				none = true
			}
		}
		if bits != 0 || none {
			p.decoSet, p.deco = true, bits
		}
	case "color":
		if c, ok := parseColor(v); ok {
			p.fg = c
		}
	case "text-align":
		switch v {
		case "center":
			p.align = alignCenter
		case "right", "end":
			p.align = alignRight
		case "left", "start", "justify":
			p.align = alignLeft
		}
	}
}

// --- selector index ---

// indexedSel is one selector of a stylesheet rule, ready for matching. anc
// are the keys its ancestors must carry (see selectorKeys).
type indexedSel struct {
	sel   cascadia.Sel
	spec  cascadia.Specificity
	order int
	decls []decl
	anc   []string
}

// selIndex buckets selectors by what their subject (the rightmost compound)
// requires — an id, else a class, else a tag — and skips a selector whose
// ancestor compounds name an id, class or tag no ancestor carries: the
// browser's selector-matching shortcuts, so each element is tested only
// against the few selectors it could match. Keys are "#id", ".class" and
// the tag name.
type selIndex struct {
	byKey map[string][]*indexedSel
	any   []*indexedSel
}

func (x *selIndex) add(s indexedSel) {
	subject, anc := selectorKeys(s.sel.String())
	s.anc = anc
	if subject == "" {
		x.any = append(x.any, &s)
		return
	}
	if x.byKey == nil {
		x.byKey = map[string][]*indexedSel{}
	}
	x.byKey[subject] = append(x.byKey[subject], &s)
}

// match calls fn for every element below root and every selector matching
// it.
func (x *selIndex) match(root *html.Node, fn func(*html.Node, *indexedSel)) {
	anc := map[string]int{} // keys of the open ancestors, counted
	test := func(n *html.Node, ss []*indexedSel) {
	next:
		for _, s := range ss {
			for _, a := range s.anc {
				if anc[a] == 0 {
					continue next
				}
			}
			if s.sel.Match(n) {
				fn(n, s)
			}
		}
	}
	var visit func(*html.Node)
	visit = func(p *html.Node) {
		for n := p.FirstChild; n != nil; n = n.NextSibling {
			if n.Type != html.ElementNode {
				continue
			}
			keys := elementKeys(n)
			for _, k := range keys {
				test(n, x.byKey[k])
			}
			test(n, x.any)
			for _, k := range keys {
				anc[k]++
			}
			visit(n)
			for _, k := range keys {
				anc[k]--
			}
		}
	}
	visit(root)
}

// elementKeys are the index keys an element carries: its tag, its id and
// each distinct class.
func elementKeys(n *html.Node) []string {
	keys := []string{n.Data}
	if id, ok := attr(n, "id"); ok {
		keys = append(keys, "#"+id)
	}
	if cls, ok := attr(n, "class"); ok {
		for _, c := range strings.FieldsFunc(cls, func(r rune) bool { return r < 0x80 && isSpace(byte(r)) }) {
			if !slices.Contains(keys[1:], "."+c) {
				keys = append(keys, "."+c)
			}
		}
	}
	return keys
}

// selectorKeys reads a serialised selector (cascadia's String form): the key
// its subject compound requires ("" for none the index can use), and the
// keys of the compounds that must be ancestors of the subject — those
// joined to it by descendant and child combinators only.
func selectorKeys(s string) (subject string, anc []string) {
	parts, combs := compounds(s)
	if len(parts) == 0 {
		return "", nil
	}
	subject = compoundKey(parts[len(parts)-1])
	for i := len(parts) - 2; i >= 0; i-- {
		if c := combs[i]; c != ' ' && c != '>' {
			break // a sibling: not an ancestor, nor are the ones before it
		}
		if k := compoundKey(parts[i]); k != "" {
			anc = append(anc, k)
		}
	}
	return subject, anc
}

// compounds splits a serialised selector at its top-level combinators:
// combs[i] joins parts[i] and parts[i+1] (' ' descendant, '>', '+', '~').
// Parenthesised arguments, attribute brackets, strings and escapes stay
// inside their compound.
func compounds(s string) (parts []string, combs []byte) {
	depth, start := 0, 0
	comb := byte(0)
	end := func(i int) {
		if i > start {
			if len(parts) > 0 {
				combs = append(combs, max(comb, ' '))
			}
			parts = append(parts, s[start:i])
			comb = 0
		}
	}
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\':
			i++
		case c == '"' || c == '\'':
			if k := skipString(s, i); k >= 0 {
				i = k
			}
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			depth--
		case depth == 0 && (c == ' ' || c == '>' || c == '+' || c == '~'):
			end(i)
			if c != ' ' {
				comb = c
			}
			start = i + 1
		}
	}
	end(len(s))
	return parts, combs
}

// compoundKey is the index key a compound selector requires: its id, else
// its first class, else its tag; "" when it names none (universal,
// attribute- or pseudo-class-only) or the name is escaped, which the index
// leaves to plain matching.
func compoundKey(s string) string {
	ident := func(i int) string {
		j := i
		for j < len(s) && (s[j] == '-' || s[j] == '_' || s[j] >= 0x80 ||
			'a' <= s[j] && s[j] <= 'z' || 'A' <= s[j] && s[j] <= 'Z' || '0' <= s[j] && s[j] <= '9') {
			j++
		}
		if j < len(s) && s[j] == '\\' {
			return "" // an escaped name
		}
		return s[i:j]
	}
	var class, tag string
	depth := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\':
			i++
		case c == '"' || c == '\'':
			if k := skipString(s, i); k >= 0 {
				i = k
			}
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			depth--
		case depth > 0:
		case c == '#':
			if id := ident(i + 1); id != "" {
				return "#" + id
			}
		case c == '.' && class == "":
			class = ident(i + 1)
		case i == 0 && c != '*' && c != ':':
			tag = strings.ToLower(ident(0))
		}
	}
	if class != "" {
		return "." + class
	}
	return tag
}

// --- parsing ---

// parseSheet splits a stylesheet into its style rules. ok is false when the
// sheet is malformed (unbalanced braces, an unterminated comment or string):
// the caller then ignores it whole. At-rules are skipped; a rule whose
// selector cascadia cannot parse is dropped alone.
func parseSheet(s string) (rules []cssRule, ok bool) {
	s, ok = stripComments(s)
	if !ok {
		return nil, false
	}
	for i := 0; ; {
		for i < len(s) && (isSpace(s[i]) || s[i] == ';') {
			i++
		}
		switch {
		case i >= len(s):
			return rules, true
		case strings.HasPrefix(s[i:], "<!--"):
			i += 4
			continue
		case strings.HasPrefix(s[i:], "-->"):
			i += 3
			continue
		case s[i] == '}':
			return nil, false
		}
		// The prelude runs to the block ('{') or, for a statement at-rule
		// (@import, @charset), to its ';'.
		at := s[i] == '@'
		j, stop := scanTo(s, i, "{;}")
		if stop < 0 || s[j] == '}' {
			return nil, false
		}
		if s[j] == ';' {
			if !at {
				return nil, false
			}
			i = j + 1
			continue
		}
		end := matchBrace(s, j)
		if end < 0 {
			return nil, false
		}
		if !at {
			if sel, err := cascadia.ParseGroup(strings.TrimSpace(s[i:j])); err == nil {
				rules = append(rules, cssRule{sel: sel, decls: parseDecls(s[j+1 : end])})
			}
		}
		i = end + 1
	}
}

// stripComments removes /* */ comments; false for an unterminated one.
func stripComments(s string) (string, bool) {
	if !strings.Contains(s, "/*") {
		return s, true
	}
	var b strings.Builder
	for {
		i := strings.Index(s, "/*")
		if i < 0 {
			b.WriteString(s)
			return b.String(), true
		}
		b.WriteString(s[:i])
		j := strings.Index(s[i+2:], "*/")
		if j < 0 {
			return "", false
		}
		b.WriteByte(' ')
		s = s[i+2+j+2:]
	}
}

// scanTo returns the index of the first byte of stops at or after i outside
// strings and parentheses (and stop >= 0), or stop -1 when there is none or
// a string is unterminated.
func scanTo(s string, i int, stops string) (j, stop int) {
	depth := 0
	for j = i; j < len(s); j++ {
		switch ch := s[j]; {
		case ch == '"' || ch == '\'':
			k := skipString(s, j)
			if k < 0 {
				return j, -1
			}
			j = k
		case ch == '\\':
			j++
		case ch == '(':
			depth++
		case ch == ')':
			if depth > 0 {
				depth--
			}
		case depth == 0 && strings.IndexByte(stops, ch) >= 0:
			return j, strings.IndexByte(stops, ch)
		}
	}
	return len(s), -1 // an escape at the very end steps past it
}

// skipString returns the index of the quote closing the string opening at
// i, or -1 when it is unterminated.
func skipString(s string, i int) int {
	q := s[i]
	for k := i + 1; k < len(s); k++ {
		switch s[k] {
		case '\\':
			k++
		case q:
			return k
		case '\n':
			return -1
		}
	}
	return -1
}

// matchBrace returns the index of the '}' closing the block opening at i, or
// -1 when the block is unbalanced.
func matchBrace(s string, i int) int {
	depth := 0
	for k := i; k < len(s); k++ {
		switch s[k] {
		case '"', '\'':
			e := skipString(s, k)
			if e < 0 {
				return -1
			}
			k = e
		case '\\':
			k++
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return k
			}
		}
	}
	return -1
}

// parseDecls parses a declaration list ("color: red; font-weight: bold").
// Malformed declarations are skipped.
func parseDecls(s string) []decl {
	var out []decl
	for i := 0; i < len(s); {
		j, stop := scanTo(s, i, ";")
		if stop < 0 && j < len(s) {
			return out // an unterminated string ends the list
		}
		if prop, val, ok := strings.Cut(s[i:j], ":"); ok {
			d := decl{prop: strings.ToLower(strings.TrimSpace(prop)), val: strings.TrimSpace(val)}
			if k := strings.LastIndexByte(d.val, '!'); k >= 0 &&
				strings.EqualFold(strings.TrimSpace(d.val[k+1:]), "important") {
				d.val, d.important = strings.TrimSpace(d.val[:k]), true
			}
			if d.prop != "" && d.val != "" {
				out = append(out, d)
			}
		}
		i = j + 1
	}
	return out
}

// parseColor reads a CSS colour value (lower-cased) as a packed style
// colour: a named colour, a hex colour or rgb()/rgba(). Fully transparent
// colours and everything else (currentcolor, inherit, hsl(), var()) report
// false.
func parseColor(v string) (uint32, bool) {
	pack := func(r, g, b int) uint32 { return 1<<24 | uint32(r)<<16 | uint32(g)<<8 | uint32(b) }
	switch {
	case strings.HasPrefix(v, "#"):
		h := v[1:]
		if len(h) == 3 || len(h) == 4 { // short form: every digit doubled
			long := make([]byte, 0, 2*len(h))
			for i := 0; i < len(h); i++ {
				long = append(long, h[i], h[i])
			}
			h = string(long)
		}
		if len(h) != 6 && len(h) != 8 {
			return 0, false
		}
		x, err := strconv.ParseUint(h, 16, 32)
		if err != nil {
			return 0, false
		}
		if len(h) == 8 {
			if x&0xff == 0 {
				return 0, false
			}
			x >>= 8
		}
		return 1<<24 | uint32(x), true
	case strings.HasPrefix(v, "rgb(") || strings.HasPrefix(v, "rgba("):
		open, end := strings.IndexByte(v, '('), strings.LastIndexByte(v, ')')
		if end < open {
			return 0, false
		}
		f := strings.Fields(strings.NewReplacer(",", " ", "/", " ").Replace(v[open+1 : end]))
		if len(f) != 3 && len(f) != 4 {
			return 0, false
		}
		var ch [3]int
		for i := range ch {
			n, ok := channel(f[i], 255)
			if !ok {
				return 0, false
			}
			ch[i] = n
		}
		if len(f) == 4 {
			if a, ok := channel(f[3], 1); !ok || a == 0 {
				return 0, false
			}
		}
		return pack(ch[0], ch[1], ch[2]), true
	}
	if v == "rebeccapurple" { // CSS Color 4, missing from the SVG list
		return pack(0x66, 0x33, 0x99), true
	}
	if c, ok := colornames.Map[v]; ok {
		return pack(int(c.R), int(c.G), int(c.B)), true
	}
	return 0, false
}

// channel reads one rgb() component — a number or a percentage of full —
// clamped to [0, full]; a non-zero alpha (full 1) reads as 1.
func channel(s string, full float64) (int, bool) {
	pct := strings.HasSuffix(s, "%")
	x, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
	if err != nil {
		return 0, false
	}
	if pct {
		x = x * full / 100
	}
	x = min(max(x, 0), full)
	if full == 1 {
		return map[bool]int{true: 1, false: 0}[x > 0], true
	}
	return int(x + 0.5), true
}
