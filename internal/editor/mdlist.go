package editor

import (
	"strings"

	"ike/internal/highlight"
)

// mdlist.go renders Markdown list markers (#1966), display-only like the rest
// of the markdown layer: every marker becomes a dynamic conceal range (#1594)
// whose stand-in is the indented marker, so lists read like rendered text
// without the buffer ever changing. An unordered `-`, `*` or `+` renders as a
// two-cell indent plus a bullet; an ordered `1.` renders its number
// right-aligned to the widest number of its own list, so a list crossing the
// 9 → 10 width boundary keeps its dots in one column:
//
//	  1. first
//	  9. ninth
//	 10. tenth
//
// Lists nest by source indent: a marker's stand-in keeps the item's own
// indent and adds the two cells in front of it, and each indent level's
// ordered run aligns on its own widest number. The caret on a marker reveals
// the raw source like any other conceal range (lineConcealRanges).

// mdListIndent is the display indent every list marker renders behind — the
// bullet or number sits two cells in from the item's own source indent.
const mdListIndent = 2

// mdBullet is the stand-in glyph for the unordered markers -, * and +.
const mdBullet = "•"

// mdListState caches the per-line marker stand-ins per document version. A
// pointer field on Model, like mdTables, so the value copies each Update
// returns share it.
type mdListState struct {
	valid   bool
	version int
	ranges  map[int]concealRange
}

// mdListItem is one detected list item: its line, the [start, end) rune
// columns of the marker itself, the item's source indent (= start) and, for
// an ordered item, the marker's digits plus its `.`/`)` delimiter. num is
// empty for the unordered markers.
type mdListItem struct {
	line       int
	start, end int
	indent     int
	num        string
	delim      string
}

// mdListConcealRanges returns the marker stand-in of line, if it carries one:
// a dynamic conceal range feeding the ordinary cell loop, exactly like the sv
// separator padding (#1589). Empty when markdown rendering is off here or the
// buffer is not markdown.
func (m Model) mdListConcealRanges(line int) []concealRange {
	if !m.mdRenderOn() || m.mdLists == nil {
		return nil
	}
	if r, ok := m.listRanges()[line]; ok {
		return []concealRange{r}
	}
	return nil
}

// listRanges returns the document's marker stand-ins keyed by line,
// recomputing only when the document version moved (like tableBlocks).
func (m Model) listRanges() map[int]concealRange {
	st := m.mdLists
	if st.valid && st.version == m.docVersion {
		return st.ranges
	}
	st.ranges = nil
	if highlight.Lang(m.path) == "markdown" {
		st.ranges = detectListRanges(m.buf.Lines())
	}
	st.version, st.valid = m.docVersion, true
	return st.ranges
}

// detectListRanges scans the document for list markers and returns one
// conceal range per item line. Ordered items are grouped into runs — the
// consecutive items sharing one source indent, continuation text and nested
// lists included — and every run's numbers are padded to its widest number,
// right-aligned. Fenced code blocks are skipped: their content is source, not
// markdown.
func detectListRanges(lines []string) map[int]concealRange {
	out := make(map[int]concealRange)
	// The open ordered runs, outermost first — one per indent level, so a
	// nested list aligns on its own widest number.
	var runs [][]mdListItem
	// flush lays out and emits every open run indented at or deeper than
	// indent; -1 closes all of them.
	flush := func(indent int) {
		for len(runs) > 0 {
			run := runs[len(runs)-1]
			if indent >= 0 && run[0].indent < indent {
				return
			}
			runs = runs[:len(runs)-1]
			width := 0
			for _, it := range run {
				if len(it.num) > width {
					width = len(it.num)
				}
			}
			for _, it := range run {
				pad := strings.Repeat(" ", mdListIndent+width-len(it.num))
				out[it.line] = concealRange{start: it.start, end: it.end, repl: pad + it.num + it.delim}
			}
		}
	}
	inFence := false
	for i, line := range lines {
		if inFence {
			if isCodeFence(line) {
				inFence = false
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue // a blank line does not end a list (loose lists)
		}
		it, ok := parseListItem(i, line)
		if !ok {
			// Text at or left of an open run's indent ends it; deeper text is
			// the current item's continuation.
			flush(leadingIndent(line))
			if isCodeFence(line) {
				inFence = true
			}
			continue
		}
		if it.num == "" {
			flush(it.indent) // an unordered item ends the ordered runs it replaces
			out[it.line] = concealRange{
				start: it.start, end: it.end,
				repl: strings.Repeat(" ", mdListIndent) + mdBullet,
			}
			continue
		}
		flush(it.indent + 1) // keep a run at this indent open, close the deeper ones
		if len(runs) > 0 && runs[len(runs)-1][0].indent == it.indent {
			runs[len(runs)-1] = append(runs[len(runs)-1], it)
			continue
		}
		runs = append(runs, []mdListItem{it})
	}
	flush(-1)
	return out
}

// parseListItem parses line's list marker: an optional indent, then `-`, `*`
// or `+`, or digits followed by `.` or `)`, and whitespace (or the line end —
// an empty item is still an item) after it. ok=false for every other line,
// thematic breaks (`---`, `***`) included: those are rules, not lists.
func parseListItem(line int, text string) (mdListItem, bool) {
	if mdThematicBreak(text) {
		return mdListItem{}, false
	}
	runes := []rune(text)
	i := leadingIndent(text)
	if i >= len(runes) {
		return mdListItem{}, false
	}
	it := mdListItem{line: line, start: i, indent: i}
	switch runes[i] {
	case '-', '*', '+':
		it.end = i + 1
	default:
		j := i
		for j < len(runes) && runes[j] >= '0' && runes[j] <= '9' {
			j++
		}
		if j == i || j >= len(runes) || (runes[j] != '.' && runes[j] != ')') {
			return mdListItem{}, false
		}
		it.num, it.delim, it.end = string(runes[i:j]), string(runes[j]), j+1
	}
	if it.end < len(runes) && runes[it.end] != ' ' && runes[it.end] != '\t' {
		return mdListItem{}, false
	}
	return it, true
}

// isCodeFence reports whether the line opens or closes a fenced code block.
func isCodeFence(text string) bool {
	t := strings.TrimLeft(text, " \t")
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

// leadingIndent counts a line's leading space and tab runes.
func leadingIndent(text string) int {
	n := 0
	for _, r := range text {
		if r != ' ' && r != '\t' {
			break
		}
		n++
	}
	return n
}

// mdThematicBreak reports whether the line is a horizontal rule: three or
// more of the same -, * or _ marker, spaces allowed between them. CommonMark
// resolves those ahead of list items, so `- - -` never renders as a bullet.
// (isThematicBreak in lsp_state.go answers the stricter no-spaces question
// for hover prose.)
func mdThematicBreak(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	marker, n := rune(0), 0
	for _, r := range t {
		switch {
		case r == ' ' || r == '\t':
		case r == '-' || r == '*' || r == '_':
			if marker == 0 {
				marker = r
			} else if r != marker {
				return false
			}
			n++
		default:
			return false
		}
	}
	return n >= 3
}
