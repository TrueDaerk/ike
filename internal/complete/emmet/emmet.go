// Package emmet is the Emmet-style completion source (Roadmap 0410, #856) —
// a deliberate subset: CSS property shorthands (m10 → margin: 10px;) and
// HTML tag snippets (div → <div>…</div> with tabstops). Full Emmet
// abbreviations (ul>li*3) contain non-identifier characters the popup's
// identifier-replace accept path cannot span, so they are out of scope; the
// subset covers the high-frequency muscle memory.
//
// Items are snippets (#846): expansion tabstops place the cursor inside the
// tag / at the value; the item Detail previews the expansion in the popup.
package emmet

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"ike/internal/complete"
	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// cssProps maps a shorthand to its property; a trailing number in the typed
// abbreviation becomes a px value ("m10" → "margin: 10px;").
var cssProps = map[string]string{
	"m": "margin", "mt": "margin-top", "mr": "margin-right", "mb": "margin-bottom", "ml": "margin-left",
	"p": "padding", "pt": "padding-top", "pr": "padding-right", "pb": "padding-bottom", "pl": "padding-left",
	"w": "width", "h": "height", "fz": "font-size", "fw": "font-weight", "lh": "line-height",
	"bg": "background", "c": "color", "d": "display", "pos": "position", "ta": "text-align",
	"td": "text-decoration", "bd": "border", "br": "border-radius", "op": "opacity", "z": "z-index",
}

// cssFixed are complete shorthands expanding to a full declaration.
var cssFixed = map[string]string{
	"df":  "display: flex;",
	"db":  "display: block;",
	"dib": "display: inline-block;",
	"dn":  "display: none;",
	"posa": "position: absolute;",
	"posr": "position: relative;",
	"posf": "position: fixed;",
}

// htmlTags are the tags offered as snippets; void/special tags carry their
// own snippet shape.
var htmlTags = []string{
	"a", "article", "aside", "body", "button", "div", "footer", "form",
	"h1", "h2", "h3", "h4", "h5", "h6", "head", "header", "html", "img",
	"input", "label", "li", "link", "main", "nav", "ol", "option", "p",
	"section", "select", "span", "table", "tbody", "td", "textarea", "th",
	"thead", "title", "tr", "ul",
}

// htmlSpecial overrides the generic <tag>$1</tag> snippet.
var htmlSpecial = map[string]string{
	"a":     `<a href="$1">$2</a>`,
	"img":   `<img src="$1" alt="$2">`,
	"input": `<input type="$1" name="$2">`,
	"link":  `<link rel="stylesheet" href="$1">`,
	"ul":    "<ul>\n\t<li>$1</li>\n</ul>",
	"ol":    "<ol>\n\t<li>$1</li>\n</ol>",
}

var cssAbbrevRe = regexp.MustCompile(`^([a-z]+)([0-9]*)$`)

// Source implements complete.Source and complete.EventObserver (buffer text
// for the abbreviation under the cursor and the HTML attribute exclusion).
type Source struct {
	texts *textStore
}

// New returns the Emmet source.
func New() *Source { return &Source{texts: newTextStore()} }

// Name implements complete.Source.
func (s *Source) Name() string { return "emmet" }

// Priority implements complete.Source: above the word echo, below symbols
// and the server.
func (s *Source) Priority() int { return ilsp.PriorityEmmet }

// Observe implements complete.EventObserver.
func (s *Source) Observe(ev host.EditorEvent) { s.texts.observe(ev) }

// Complete implements complete.Source.
func (s *Source) Complete(_ context.Context, req complete.Request) ([]ilsp.CompletionItem, error) {
	line := s.texts.lineAt(req.Path, req.Line)
	abbrev := identifierPrefix(line, req.Col)
	if abbrev == "" {
		return nil, nil
	}
	switch strings.ToLower(filepath.Ext(req.Path)) {
	case ".css", ".scss", ".less":
		return cssItems(abbrev), nil
	case ".html", ".htm", ".xhtml":
		if insideAttrValue(line, req.Col) {
			return nil, nil
		}
		return htmlItems(abbrev), nil
	}
	return nil, nil
}

// cssItems expands a CSS shorthand abbreviation.
func cssItems(abbrev string) []ilsp.CompletionItem {
	lower := strings.ToLower(abbrev)
	if exp, ok := cssFixed[lower]; ok {
		return []ilsp.CompletionItem{item(abbrev, exp+"$0", exp)}
	}
	m := cssAbbrevRe.FindStringSubmatch(lower)
	if m == nil {
		return nil
	}
	prop, ok := cssProps[m[1]]
	if !ok {
		return nil
	}
	if m[2] != "" {
		val := m[2] + "px"
		if m[2] == "0" {
			val = "0"
		}
		exp := prop + ": " + val + ";"
		return []ilsp.CompletionItem{item(abbrev, exp+"$0", exp)}
	}
	exp := prop + ": $1;"
	return []ilsp.CompletionItem{item(abbrev, exp+"$0", prop+": …;")}
}

// htmlItems offers tag snippets prefixed by the abbreviation.
func htmlItems(abbrev string) []ilsp.CompletionItem {
	lower := strings.ToLower(abbrev)
	var items []ilsp.CompletionItem
	for _, tag := range htmlTags {
		if !strings.HasPrefix(tag, lower) {
			continue
		}
		snippet, ok := htmlSpecial[tag]
		if !ok {
			snippet = "<" + tag + ">$1</" + tag + ">"
		}
		items = append(items, item(tag, snippet+"$0", oneLine(snippet)))
	}
	return items
}

// item builds one snippet completion; the Detail previews the expansion and
// FilterText keeps the popup's fuzzy filter matching what the user typed.
func item(label, snippet, preview string) ilsp.CompletionItem {
	return ilsp.CompletionItem{
		Label:      label,
		FilterText: label,
		InsertText: snippet,
		IsSnippet:  true,
		Detail:     "⌁ " + preview,
		Kind:       protocol.KindSnippet,
		SortText:   label,
	}
}

// oneLine flattens a multi-line snippet preview.
func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", "")
	for _, ph := range []string{"$1", "$2", "$0"} {
		s = strings.ReplaceAll(s, ph, "…")
	}
	return s
}

// --- buffer text bookkeeping ---

type textStore struct {
	mu    sync.RWMutex
	texts map[string]string
}

func newTextStore() *textStore { return &textStore{texts: map[string]string{}} }

func (t *textStore) observe(ev host.EditorEvent) {
	if ev.Kind != host.EditorChange {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if ev.Large {
		delete(t.texts, ev.Path)
		return
	}
	t.texts[ev.Path] = ev.Text
}

func (t *textStore) lineAt(path string, line int) string {
	t.mu.RLock()
	text := t.texts[path]
	t.mu.RUnlock()
	lines := strings.Split(text, "\n")
	if line < 0 || line >= len(lines) {
		return ""
	}
	return lines[line]
}

var attrRe = regexp.MustCompile(`\s[a-zA-Z-]+\s*=\s*("[^"]*|'[^']*)$`)

// insideAttrValue reports whether col sits inside an unclosed attribute
// value — tag snippets make no sense there (#853's CSS-class source owns
// class=/id=).
func insideAttrValue(line string, col int) bool {
	runes := []rune(line)
	if col > len(runes) {
		col = len(runes)
	}
	return attrRe.MatchString(string(runes[:col]))
}

// identifierPrefix is the partial identifier ending at col.
func identifierPrefix(line string, col int) string {
	runes := []rune(line)
	if col > len(runes) {
		col = len(runes)
	}
	start := col
	for start > 0 && (runes[start-1] == '_' || unicode.IsLetter(runes[start-1]) || unicode.IsDigit(runes[start-1])) {
		start--
	}
	return string(runes[start:col])
}
