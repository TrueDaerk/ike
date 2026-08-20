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
//
// An item's continuation lines — the plain text lines indented deeper than
// its marker — are padded the same display-only way (#1975), so they line up
// with the item's text instead of keeping their bare source indent:
//
//	  • My multiline bullet
//	    point
//
// The pad conceals the continuation's leading whitespace behind a run of
// spaces as wide as the item's text column, so the caret in that indent
// reveals the raw source just like it does on a marker.

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
// empty for the unordered markers. tabbed marks an indent containing a tab —
// its rune count is not its display width, so continuation padding stays off
// such an item. conts collects the item's continuation lines, kept because an
// ordered item's text column is only known once its run is laid out.
type mdListItem struct {
	line       int
	start, end int
	indent     int
	num        string
	delim      string
	tabbed     bool
	conts      []mdListCont
}

// mdListCont is one continuation line of a list item: the line and the rune
// count of its own leading whitespace, which the pad stands in for.
type mdListCont struct {
	line   int
	indent int
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
// conceal range per item line plus one per continuation line. Ordered items
// are grouped into runs — the consecutive items sharing one source indent,
// continuation text and nested lists included — and every run's numbers are
// padded to its widest number, right-aligned. Continuation lines are padded
// to the display column their item's text starts in, which for an ordered
// item is only known once its run is flushed. Fenced code blocks are skipped:
// their content is source, not markdown.
func detectListRanges(lines []string) map[int]concealRange {
	out := make(map[int]concealRange)
	// The open ordered runs, outermost first — one per indent level, so a
	// nested list aligns on its own widest number.
	var runs [][]*mdListItem
	// The open items, outermost first: a non-item line indented deeper than
	// the innermost of them is that item's continuation.
	var open []*mdListItem
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
				// The run's aligned width is the item's text column: every
				// number renders mdListIndent+width+delim cells wide.
				padConts(out, it, it.indent+mdListIndent+width+len(it.delim)+1)
			}
		}
	}
	// popOpen closes every open item whose marker sits at or right of indent.
	popOpen := func(indent int) {
		for len(open) > 0 && open[len(open)-1].indent >= indent {
			open = open[:len(open)-1]
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
			ind := leadingIndent(line)
			flush(ind)
			popOpen(ind)
			if isCodeFence(line) {
				inFence = true
				continue // a fenced block is source: no marker, no padding
			}
			if len(open) > 0 {
				cur := open[len(open)-1]
				cont := mdListCont{line: i, indent: ind}
				if cur.num == "" {
					// An unordered stand-in is a fixed two cells plus the
					// bullet, so its text column is known right away.
					padCont(out, cur, cont, cur.indent+mdListIndent+len([]rune(mdBullet))+1)
				} else {
					cur.conts = append(cur.conts, cont)
				}
			}
			continue
		}
		popOpen(it.indent)
		open = append(open, &it)
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
			runs[len(runs)-1] = append(runs[len(runs)-1], &it)
			continue
		}
		runs = append(runs, []*mdListItem{&it})
	}
	flush(-1)
	return out
}

// padConts emits the pads of it's continuation lines, whose text renders in
// display column target.
func padConts(out map[int]concealRange, it *mdListItem, target int) {
	for _, c := range it.conts {
		padCont(out, it, c, target)
	}
}

// padCont conceals c's leading whitespace behind a run of target spaces, so
// the continuation line renders flush with its item's text. It stays out of
// the way when the pad would pull text left (the line already reaches that
// column) or when a tab in the item's indent makes its rune count lie about
// the display column.
func padCont(out map[int]concealRange, it *mdListItem, c mdListCont, target int) {
	if it.tabbed || c.indent <= 0 || target <= c.indent {
		return
	}
	out[c.line] = concealRange{start: 0, end: c.indent, repl: strings.Repeat(" ", target)}
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
	it := mdListItem{line: line, start: i, indent: i, tabbed: hasTabIndent(runes[:i])}
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

// hasTabIndent reports whether an indent holds a tab, whose display width is
// not one cell — the continuation pads count rune columns, so they skip those
// items rather than misalign them.
func hasTabIndent(indent []rune) bool {
	for _, r := range indent {
		if r == '\t' {
			return true
		}
	}
	return false
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
