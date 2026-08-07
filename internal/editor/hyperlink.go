package editor

// hyperlink.go makes URLs in a buffer real terminal links (#1655): the render
// loop wraps every display cell of a detected URL in its own OSC 8 open/close
// pair, so cmd/ctrl+click opens the browser in terminals that support the
// sequence (Ghostty, iTerm2, kitty, WezTerm) and terminals without support
// ignore it by spec. The sequences are zero-width — one buffer rune stays one
// display cell — so cursor positioning, width math and click mapping (#1469's
// invariant) are untouched. Wrapping per cell rather than per run means any
// later splice of the rendered row (overlay floats truncate with ansi.Cut)
// can only ever fall between complete pairs, never inside one; an `id`
// parameter shared by the whole span tells the terminal the cells form one
// link, so hover-highlighting still covers the full URL. Detection is
// per-line and rides the line cache; editor.hyperlinks turns it off.

import "strings"

// linkSpan is one clickable range on a line: the rune columns [start, end)
// whose cells carry url as their OSC 8 target.
type linkSpan struct {
	start, end int
	url        string
}

// lineLinks returns the clickable spans of one line's runes, nil when the
// feature is off. Called per rendered span; the result is not cached — the
// scan is a single pass and the rendered body itself is memoized (#614).
func (m Model) lineLinks(runes []rune) []linkSpan {
	if !m.hyperlinks || len(runes) == 0 {
		return nil
	}
	return scanLinks(runes)
}

// linkAt returns the span covering col. Markdown label spans are scanned
// first, so a label that itself looks like a URL keeps its link target.
func linkAt(spans []linkSpan, col int) (linkSpan, bool) {
	for _, s := range spans {
		if col >= s.start && col < s.end {
			return s, true
		}
	}
	return linkSpan{}, false
}

// scanLinks finds the clickable ranges of a line: Markdown link labels carry
// their destination (#1655), then bare http(s) URLs carry themselves.
func scanLinks(runes []rune) []linkSpan {
	spans := appendMarkdownLinks(nil, runes)
	return appendPlainURLs(spans, runes)
}

// appendMarkdownLinks adds a span per inline link `[label](target)` (images
// included — the label span simply starts after the bracket), attaching the
// target to the label so the rendered text is clickable even while the
// destination is concealed (#881). Only targets with a URI scheme are used:
// OSC 8 needs an absolute URI, so a relative path target adds nothing.
func appendMarkdownLinks(spans []linkSpan, runes []rune) []linkSpan {
	for i := 1; i < len(runes); i++ {
		if runes[i] != '(' || runes[i-1] != ']' {
			continue
		}
		lb := matchOpenBracket(runes, i-1)
		if lb < 0 {
			continue
		}
		rb := matchCloseParen(runes, i)
		if rb < 0 {
			continue
		}
		dest := runes[i+1 : rb]
		// `[label](url "title")`: the target ends at the first space.
		for sp, r := range dest {
			if r == ' ' {
				dest = dest[:sp]
				break
			}
		}
		// `[label](<url>)`: unwrap the angle brackets.
		if len(dest) >= 2 && dest[0] == '<' && dest[len(dest)-1] == '>' {
			dest = dest[1 : len(dest)-1]
		}
		if !isLinkTarget(dest) {
			continue
		}
		if lb+1 < i-1 {
			spans = append(spans, linkSpan{start: lb + 1, end: i - 1, url: string(dest)})
		}
		i = rb
	}
	return spans
}

// appendPlainURLs adds a span per bare http(s) URL, trailing punctuation
// trimmed the way prose expects: `https://x.com/a.` drops the period,
// `.../Go_(language)` keeps its balanced parenthesis.
func appendPlainURLs(spans []linkSpan, runes []rune) []linkSpan {
	for i := 0; i < len(runes); i++ {
		n := schemeLen(runes[i:])
		if n == 0 {
			continue
		}
		// A scheme starts at a word boundary: `xhttps://` is not a URL.
		if i > 0 && isAlnumRune(runes[i-1]) {
			continue
		}
		end := i + n
		for end < len(runes) && isURLRune(runes[end]) {
			end++
		}
		end = trimURLEnd(runes, i, end)
		if end > i+n {
			spans = append(spans, linkSpan{start: i, end: end, url: string(runes[i:end])})
			i = end
		} else {
			i += n
		}
	}
	return spans
}

// schemeLen returns the length of the http:// or https:// prefix at the start
// of runes (case-insensitive), or 0.
func schemeLen(runes []rune) int {
	for _, scheme := range []string{"https://", "http://"} {
		if len(runes) < len(scheme) {
			continue
		}
		match := true
		for j, c := range scheme {
			r := runes[j]
			if 'A' <= r && r <= 'Z' {
				r += 'a' - 'A'
			}
			if r != c {
				match = false
				break
			}
		}
		if match {
			return len(scheme)
		}
	}
	return 0
}

// isLinkTarget reports whether a Markdown link destination is usable as an
// OSC 8 URI: an absolute URI with a known scheme, every rune URL-safe (which
// also keeps ESC/BEL out of the emitted sequence).
func isLinkTarget(dest []rune) bool {
	s := strings.ToLower(string(dest))
	var rest int
	switch {
	case strings.HasPrefix(s, "https://"):
		rest = len("https://")
	case strings.HasPrefix(s, "http://"):
		rest = len("http://")
	case strings.HasPrefix(s, "mailto:"):
		rest = len("mailto:")
	case strings.HasPrefix(s, "file://"):
		rest = len("file://")
	default:
		return false
	}
	if len(dest) <= rest {
		return false
	}
	for _, r := range dest {
		if !isURLRune(r) {
			return false
		}
	}
	return true
}

// isURLRune reports whether r can appear inside a URL: printable ASCII minus
// the runes that conventionally delimit one in prose or markup. Control bytes
// and non-ASCII fail, so a sequence built from a scanned URL can never carry
// a terminator byte.
func isURLRune(r rune) bool {
	if r <= ' ' || r >= 0x7f {
		return false
	}
	switch r {
	case '<', '>', '"', '`':
		return false
	}
	return true
}

// trimURLEnd drops trailing prose punctuation from the URL at [start, end):
// sentence-final marks unconditionally, a closer (`)`, `]`, `}`) only while
// it has no matching opener inside the URL.
func trimURLEnd(runes []rune, start, end int) int {
	for end > start {
		switch r := runes[end-1]; r {
		case '.', ',', ';', ':', '!', '?', '\'', '"':
			end--
		case ')', ']', '}':
			opener := map[rune]rune{')': '(', ']': '[', '}': '{'}[r]
			depth := 0
			for _, u := range runes[start:end] {
				switch u {
				case opener:
					depth++
				case r:
					depth--
				}
			}
			if depth >= 0 {
				return end
			}
			end--
		default:
			return end
		}
	}
	return end
}

// matchOpenBracket walks left from the `]` at pos to its matching `[`,
// or -1 when the bracket never opens on this line.
func matchOpenBracket(runes []rune, pos int) int {
	depth := 0
	for j := pos; j >= 0; j-- {
		switch runes[j] {
		case ']':
			depth++
		case '[':
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
}

// matchCloseParen walks right from the `(` at pos to its matching `)`,
// or -1 when the destination never closes on this line.
func matchCloseParen(runes []rune, pos int) int {
	depth := 0
	for j := pos; j < len(runes); j++ {
		switch runes[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
}
