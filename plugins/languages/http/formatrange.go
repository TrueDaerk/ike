package langhttp

import (
	"errors"
	"strings"
)

// formatrange.go: reformat-selection for the built-in .http formatter. The
// selection widens to the smallest unit the formatter handles — one request
// body when the selection lies entirely inside it, otherwise every request
// block (the lines between ### separators) it touches. Everything outside
// those units, including the ### separator lines, stays byte-identical.

// blockSpan is one request block's 0-based inclusive line span; body is the
// absolute line of the first body content line, -1 when the block has none.
type blockSpan struct{ first, last, body int }

// blockSpans splits lines into the ### separated blocks formatHTTP formats.
func blockSpans(lines []string) []blockSpan {
	var spans []blockSpan
	start := 0
	for i := 0; i <= len(lines); i++ {
		if i < len(lines) && !strings.HasPrefix(lines[i], "###") {
			continue
		}
		if i > start {
			b := bodyStart(lines[start:i])
			if b >= 0 {
				b += start
			}
			spans = append(spans, blockSpan{first: start, last: i - 1, body: b})
		}
		start = i + 1
	}
	return spans
}

// bodyStart returns the index of the first body content line in one block,
// following the same request-line / continuation / header walk as
// formatBlock; -1 when the block has no body formatBlock would treat as one.
func bodyStart(lines []string) int {
	i := 0
	for i < len(lines) && (strings.TrimSpace(lines[i]) == "" || isCommentLine(lines[i]) || isVarDefLine(lines[i])) {
		i++
	}
	if i == len(lines) {
		return -1
	}
	if _, _, _, ok := splitRequestLine(lines[i]); !ok {
		return -1
	}
	i++
	for i < len(lines) {
		if isCommentLine(lines[i]) {
			i++
			continue
		}
		t := strings.TrimSpace(lines[i])
		if t == "" || (!strings.HasPrefix(t, "?") && !strings.HasPrefix(t, "&")) {
			break
		}
		i++
	}
	for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
		i++
	}
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i == len(lines) {
		return -1
	}
	return i
}

// formatRangeHTTP formats the units overlapping [startLine, endLine]
// (0-based inclusive) and splices them back; everything else is untouched.
func formatRangeHTTP(src string, startLine, endLine int, indent string) (string, error) {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	lines := strings.Split(src, "\n")
	spans := blockSpans(lines)

	var touched []blockSpan
	for _, sp := range spans {
		if sp.last >= startLine && sp.first <= endLine {
			touched = append(touched, sp)
		}
	}
	if len(touched) == 0 {
		return src, nil
	}

	// back to front so earlier line numbers stay valid
	for i := len(touched) - 1; i >= 0; i-- {
		sp := touched[i]
		var out []string
		if len(touched) == 1 && sp.body >= 0 && startLine >= sp.body && endLine <= sp.last {
			out = formatBody(lines[sp.body : sp.last+1])
			lines = splice(lines, sp.body, sp.last, out)
			continue
		}
		out = formatBlock(lines[sp.first:sp.last+1], indent)
		lines = splice(lines, sp.first, sp.last, out)
	}

	formatted := strings.Join(lines, "\n")
	if !requestsEqual(src, formatted) {
		return "", errors.New("reformat aborted: output would change the parsed requests")
	}
	return formatted, nil
}

// splice replaces lines[first:last+1] with repl.
func splice(lines []string, first, last int, repl []string) []string {
	out := make([]string, 0, len(lines)-(last-first+1)+len(repl))
	out = append(out, lines[:first]...)
	out = append(out, repl...)
	return append(out, lines[last+1:]...)
}
