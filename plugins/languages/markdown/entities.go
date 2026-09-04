package langmarkdown

// entities.go decodes HTML character references in Markdown prose (#2345):
// CommonMark resolves `&amp;` and `&#x2026;` in text exactly as HTML does,
// so the rendered reading is the honest one. Code is the exception the spec
// carves out — entities stay literal in code spans and code blocks — so the
// scan skips fenced blocks (``` / ~~~), indented code lines and inline
// `code spans` before decoding what remains.

import (
	"strings"

	"ike/internal/escapes"
	"ike/internal/lang"
)

// entitySpans produces the entity stand-ins for a Markdown buffer.
func entitySpans(lines []string) []lang.Span {
	var out []lang.Span
	fence := "" // the marker of an open fenced code block
	for li, line := range lines {
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence = trimmed[:3]
			continue
		}
		if strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
			continue // an indented code line keeps its entities literal
		}
		runes := []rune(line)
		code := codeSpanRanges(runes)
		for _, s := range escapes.EntityLineSpans(li, line, escapes.EntityHTML) {
			if !overlapsRange(code, s.StartCol, s.EndCol) {
				out = append(out, s)
			}
		}
	}
	return out
}

// codeSpanRanges collects the inline code spans of a line: a backtick run of
// length n opens one, the next run of exactly n closes it (CommonMark's
// matching rule, without the spec's trimming — the bounds only gate the
// entity scan).
func codeSpanRanges(runes []rune) [][2]int {
	var out [][2]int
	for i := 0; i < len(runes); i++ {
		if runes[i] != '`' {
			continue
		}
		n := runLen(runes, i)
		for j := i + n; j < len(runes); j++ {
			if runes[j] != '`' {
				continue
			}
			m := runLen(runes, j)
			if m == n {
				out = append(out, [2]int{i, j + m})
				i = j + m - 1
				break
			}
			j += m - 1
		}
	}
	return out
}

// runLen returns the length of the backtick run starting at i.
func runLen(runes []rune, i int) int {
	j := i
	for j < len(runes) && runes[j] == '`' {
		j++
	}
	return j - i
}

// overlapsRange reports whether [start, end) intersects one of the ranges.
func overlapsRange(ranges [][2]int, start, end int) bool {
	for _, r := range ranges {
		if start < r[1] && end > r[0] {
			return true
		}
	}
	return false
}
