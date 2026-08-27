package editor

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// hovermd.go renders the markdown prose of an LSP hover as terminal text
// (#2147). Fenced code blocks are handled by newHover through the highlight
// layer; everything here is the line-level and inline subset that shows up in
// server documentation: headings, block quotes, list bullets, bold, italic,
// inline code, links and autolinks.
//
// It is deliberately a small hand-written subset rather than a full markdown
// engine: the popup is a handful of lines wide, the input is documentation
// prose, and the cost of a wrong parse (a literal "*" left behind) has to stay
// smaller than the cost of pulling a renderer into the editor's hot path. The
// rules that matter are the ones that keep code-shaped prose intact —
// underscores inside identifiers are never emphasis, an unmatched marker stays
// literal.

// mdStyles are the inline styles the hover prose renders with, resolved from
// the active theme once per hover build.
type mdStyles struct {
	bold   lipgloss.Style
	italic lipgloss.Style
	code   lipgloss.Style
	url    lipgloss.Style
}

// hoverMDStyles resolves the inline styles from the editor's palette: inline
// code borrows the accent tint fenced blocks fall back to, a link's URL is
// dimmed to the border colour so the link text still leads.
func (m Model) hoverMDStyles() mdStyles {
	return mdStyles{
		bold:   lipgloss.NewStyle().Bold(true),
		italic: lipgloss.NewStyle().Italic(true),
		code:   lipgloss.NewStyle().Foreground(m.theme().Accent),
		url:    lipgloss.NewStyle().Foreground(m.theme().Border),
	}
}

// renderMarkdownLine renders one markdown prose line: the block prefix
// (heading, quote, list bullet) becomes plain chrome, the rest goes through
// the inline renderer. A line with no markdown in it comes back unchanged and
// unstyled.
func renderMarkdownLine(line string, st mdStyles) string {
	indent, body := splitIndent(line)
	prefix := ""
	switch {
	case isHeading(body):
		hashes := len(body) - len(strings.TrimLeft(body, "#"))
		body = strings.TrimSpace(strings.Trim(body[hashes:], "#"))
		return indent + st.bold.Render(renderMarkdownInline(body, st))
	case strings.HasPrefix(body, "> "):
		prefix, body = "│ ", body[2:]
	case strings.HasPrefix(body, "- "), strings.HasPrefix(body, "* "), strings.HasPrefix(body, "+ "):
		prefix, body = "• ", body[2:]
	}
	return indent + prefix + renderMarkdownInline(body, st)
}

// isHeading reports whether a line starts an ATX heading ("## Title"): one to
// six hashes followed by a space, the shape servers use for doc sections.
func isHeading(s string) bool {
	n := len(s) - len(strings.TrimLeft(s, "#"))
	return n >= 1 && n <= 6 && strings.HasPrefix(s[n:], " ")
}

// splitIndent peels a line's leading spaces off, so nested list items keep
// their depth while their marker is rewritten.
func splitIndent(line string) (indent, body string) {
	trimmed := strings.TrimLeft(line, " ")
	return line[:len(line)-len(trimmed)], trimmed
}

// renderMarkdownInline strips inline markdown syntax and styles what it marked
// up: `code`, **bold**, *italic*, [text](url) (the URL kept, dimmed, after the
// text) and <autolinks>. A marker with no partner stays literal — half-typed
// emphasis in a doc string must not swallow the rest of the line — and a
// backslash escape passes the next rune through untouched.
func renderMarkdownInline(s string, st mdStyles) string {
	r := []rune(s)
	var b strings.Builder
	for i := 0; i < len(r); {
		switch {
		case r[i] == '\\' && i+1 < len(r) && isMDPunct(r[i+1]):
			b.WriteRune(r[i+1])
			i += 2
		case r[i] == '`':
			if end, text, ok := codeSpan(r, i); ok {
				b.WriteString(st.code.Render(text))
				i = end
				continue
			}
			b.WriteRune(r[i])
			i++
		case r[i] == '[' || (r[i] == '!' && i+1 < len(r) && r[i+1] == '['):
			if end, out, ok := mdLink(r, i, st); ok {
				b.WriteString(out)
				i = end
				continue
			}
			b.WriteRune(r[i])
			i++
		case r[i] == '<':
			if end, url, ok := autolink(r, i); ok {
				b.WriteString(st.url.Render(url))
				i = end
				continue
			}
			b.WriteRune(r[i])
			i++
		case r[i] == '*' || r[i] == '_':
			if end, out, ok := emphasis(r, i, st); ok {
				b.WriteString(out)
				i = end
				continue
			}
			b.WriteRune(r[i])
			i++
		default:
			b.WriteRune(r[i])
			i++
		}
	}
	return b.String()
}

// isMDPunct reports whether a rune is escapable markdown punctuation, so
// "\*not emphasis\*" reads as written.
func isMDPunct(r rune) bool { return strings.ContainsRune("\\`*_[]()#+-.!<>|", r) }

// codeSpan matches a `code span` (or a double-backtick fence around a span
// that itself contains a backtick) opening
// at i, returning the index just past it and the literal text inside.
func codeSpan(r []rune, i int) (int, string, bool) {
	open := 0
	for i+open < len(r) && r[i+open] == '`' {
		open++
	}
	for j := i + open; j+open <= len(r); j++ {
		if r[j] != '`' {
			continue
		}
		run := 0
		for j+run < len(r) && r[j+run] == '`' {
			run++
		}
		if run == open {
			return j + open, string(r[i+open : j]), true
		}
		j += run - 1
	}
	return 0, "", false
}

// mdLink matches "[text](url)" (and the "![alt](url)" image form) at i. The
// link text renders inline, the URL follows it dimmed in parentheses — a
// terminal popup cannot be clicked, so the address has to be readable. An
// image keeps only its alt text: there is nothing to show and no target worth
// spelling out.
func mdLink(r []rune, i int, st mdStyles) (int, string, bool) {
	image := r[i] == '!'
	open := i
	if image {
		open++
	}
	closeIdx := matchBracket(r, open)
	if closeIdx < 0 || closeIdx+1 >= len(r) || r[closeIdx+1] != '(' {
		return 0, "", false
	}
	end := matchParen(r, closeIdx+1)
	if end < 0 {
		return 0, "", false
	}
	url := strings.TrimSpace(string(r[closeIdx+2 : end]))
	if bracketed := strings.HasPrefix(url, "<") && strings.HasSuffix(url, ">"); bracketed {
		url = url[1 : len(url)-1]
	} else if strings.ContainsAny(url, " \t") {
		// A bare link destination cannot contain whitespace. Enforcing that
		// keeps code-shaped prose — "func F[T any](v T)" — out of the link
		// parser, where it would otherwise lose its brackets.
		return 0, "", false
	}
	text := renderMarkdownInline(string(r[open+1:closeIdx]), st)
	if image {
		return end + 1, text, true
	}
	if text == "" {
		return end + 1, st.url.Render(url), true
	}
	if url == "" {
		return end + 1, text, true
	}
	return end + 1, text + " " + st.url.Render("("+url+")"), true
}

// matchBracket returns the index of the "]" closing the "[" at i, honouring
// nesting and backslash escapes; -1 when the bracket is never closed.
func matchBracket(r []rune, i int) int {
	depth := 0
	for j := i; j < len(r); j++ {
		switch r[j] {
		case '\\':
			j++
		case '[':
			depth++
		case ']':
			if depth--; depth == 0 {
				return j
			}
		}
	}
	return -1
}

// matchParen is matchBracket for the "(url)" half of a link.
func matchParen(r []rune, i int) int {
	depth := 0
	for j := i; j < len(r); j++ {
		switch r[j] {
		case '\\':
			j++
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				return j
			}
		}
	}
	return -1
}

// autolink matches "<https://example.com>" at i — the bare-URL form servers
// use in doc strings — and returns the address without its angle brackets.
// Anything that is not a URL (an HTML tag, a Go type parameter list) is left
// alone.
func autolink(r []rune, i int) (int, string, bool) {
	for j := i + 1; j < len(r); j++ {
		if r[j] == ' ' || r[j] == '<' {
			return 0, "", false
		}
		if r[j] != '>' {
			continue
		}
		url := string(r[i+1 : j])
		if !strings.Contains(url, "://") && !strings.HasPrefix(url, "mailto:") {
			return 0, "", false
		}
		return j + 1, url, true
	}
	return 0, "", false
}

// emphasis matches a *…* / **…** (or _…_ / __…__) run opening at i and returns
// the styled text plus the index just past the closing marker.
//
// The delimiter rules are the pragmatic subset: an opener may not be followed
// by a space (so "a * b" is arithmetic, not emphasis) and a closer may not be
// preceded by one. Underscores additionally have to sit on a word boundary,
// which is what keeps snake_case identifiers — everywhere in server
// documentation — out of the emphasis parser.
func emphasis(r []rune, i int, st mdStyles) (int, string, bool) {
	marker := r[i]
	n := 1
	if i+1 < len(r) && r[i+1] == marker {
		n = 2
	}
	if marker == '_' && !wordBoundary(r, i-1) {
		return 0, "", false
	}
	start := i + n
	if start >= len(r) || r[start] == ' ' {
		return 0, "", false
	}
	for j := start; j < len(r); j++ {
		if r[j] == '\\' {
			j++
			continue
		}
		if r[j] != marker || !runOf(r, j, marker, n) {
			continue
		}
		if r[j-1] == ' ' {
			continue
		}
		if marker == '_' && !wordBoundary(r, j+n) {
			continue
		}
		inner := renderMarkdownInline(string(r[start:j]), st)
		style := st.italic
		if n == 2 {
			style = st.bold
		}
		return j + n, style.Render(inner), true
	}
	return 0, "", false
}

// runOf reports whether the marker run starting at j is exactly n long — so a
// "**" closer is not mistaken for the "*" one, or the reverse.
func runOf(r []rune, j int, marker rune, n int) bool {
	run := 0
	for j+run < len(r) && r[j+run] == marker {
		run++
	}
	return run == n
}

// wordBoundary reports whether index i (which may sit outside the slice) is
// not a word character — the condition an underscore emphasis marker must
// meet on both sides.
func wordBoundary(r []rune, i int) bool {
	if i < 0 || i >= len(r) {
		return true
	}
	c := r[i]
	return !(c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z')
}
