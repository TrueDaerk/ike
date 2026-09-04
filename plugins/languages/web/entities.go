package langweb

// entities.go decodes HTML character references in JSX text (#2345): React
// renders the `&amp;` in `<i>Fish &amp; Chips</i>` as the character it
// names, so the buffer should read the same way. Outside JSX text an
// `&amp;`-shaped run is code (`a &amp;&amp; b` is not — `&&` is no entity —
// but a string literal could hold one), so the scan is deliberately narrow:
// only stretches between a `>` and the next `<` on the same line decode,
// with string literals skipped before the `>` counts and `{…}` expression
// islands cut out of the stretch. A reference in code would additionally
// have to be a *valid* entity to decode at all, so the residual false-fire
// window is a literal `&name;` sitting between comparison operators — text
// nobody writes.

import (
	"ike/internal/escapes"
	"ike/internal/lang"
)

// jsxEntitySpans produces the entity stand-ins for the JSX text of a JS/TS
// buffer.
func jsxEntitySpans(lines []string) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		runes := []rune(line)
		for i := 0; i < len(runes); i++ {
			switch runes[i] {
			case '\'', '"', '`':
				i = skipStringLit(runes, i)
			case '>':
				// An arrow (`=>`), a comparison (`>=`) or a shift is not a
				// tag close.
				if i > 0 && (runes[i-1] == '=' || runes[i-1] == '-' || runes[i-1] == '>') {
					continue
				}
				if i+1 < len(runes) && runes[i+1] == '=' {
					continue
				}
				lt := -1
				for j := i + 1; j < len(runes); j++ {
					if runes[j] == '<' {
						lt = j
						break
					}
				}
				if lt < 0 {
					continue // an unbounded stretch is code, not JSX text
				}
				out = appendJSXText(out, li, runes, i+1, lt)
				i = lt - 1
			}
		}
	}
	return out
}

// appendJSXText decodes the references in [from, to), cutting `{…}`
// expression islands out first — an entity inside braces sits in code.
func appendJSXText(out []lang.Span, li int, runes []rune, from, to int) []lang.Span {
	start := from
	for i := from; i < to; i++ {
		if runes[i] != '{' {
			continue
		}
		out = appendTextEntities(out, li, runes, start, i)
		depth := 1
		for i++; i < to && depth > 0; i++ {
			switch runes[i] {
			case '{':
				depth++
			case '}':
				depth--
			}
		}
		start = i
		i--
	}
	return appendTextEntities(out, li, runes, start, to)
}

// appendTextEntities decodes the references in the text stretch [from, to).
func appendTextEntities(out []lang.Span, li int, runes []rune, from, to int) []lang.Span {
	if from >= to {
		return out
	}
	for _, s := range escapes.EntityLineSpans(li, string(runes[from:to]), escapes.EntityHTML) {
		s.StartCol += from
		s.EndCol += from
		out = append(out, s)
	}
	return out
}

// skipStringLit returns the index of the closing quote of the literal
// opening at i — or the line end when it never closes.
func skipStringLit(runes []rune, i int) int {
	q := runes[i]
	for i++; i < len(runes); i++ {
		if runes[i] == '\\' {
			i++
			continue
		}
		if runes[i] == q {
			return i
		}
	}
	return len(runes)
}
