package htmlrender

import "sort"

// sourceMap ties rendered lines to the source bytes they came from, in both
// directions. Every content line records the byte offset of its first word
// (or of the element that produced it: a rule, an empty list item, a heading
// marker); separator blank lines record none and resolve to their nearest
// neighbour. The offsets are not monotonic in the rendered order — a list
// marker's line starts at the <li>, a placeholder table cell sits beside its
// row — so the reverse direction searches an offset-sorted copy.
type sourceMap struct {
	lineSrc    []int // per rendered line: source byte offset, -1 for none
	lineStarts []int // byte offset of each source line start
	srcLen     int
	order      []int // rendered lines with an offset, sorted by (offset, line)
}

func newSourceMap(src []byte, lineSrc []int) sourceMap {
	m := sourceMap{lineSrc: lineSrc, srcLen: len(src), lineStarts: []int{0}}
	for i, b := range src {
		if b == '\n' {
			m.lineStarts = append(m.lineStarts, i+1)
		}
	}
	for i, off := range lineSrc {
		if off >= 0 {
			m.order = append(m.order, i)
		}
	}
	sort.SliceStable(m.order, func(a, b int) bool {
		return lineSrc[m.order[a]] < lineSrc[m.order[b]]
	})
	return m
}

// SourceOffset returns the source byte offset rendered line came from. A line
// without its own offset (a separator blank line) takes the nearest preceding
// line's, or the following one's at the top of the document; ok is false only
// when nothing in the document maps back to the source.
func (m sourceMap) SourceOffset(line int) (off int, ok bool) {
	n := len(m.lineSrc)
	if n == 0 {
		return 0, false
	}
	line = min(max(line, 0), n-1)
	for i := line; i >= 0; i-- {
		if m.lineSrc[i] >= 0 {
			return m.lineSrc[i], true
		}
	}
	for i := line + 1; i < n; i++ {
		if m.lineSrc[i] >= 0 {
			return m.lineSrc[i], true
		}
	}
	return 0, false
}

// SourceLine is SourceOffset as a 0-based source line — where the editor
// caret goes for a rendered line.
func (m sourceMap) SourceLine(line int) (int, bool) {
	off, ok := m.SourceOffset(line)
	if !ok {
		return 0, false
	}
	return m.sourceLineOf(off), true
}

// LineForOffset returns the rendered line showing the content at source byte
// offset off: the first line of the content starting at or nearest before
// off. An offset before any rendered content maps to the first such line.
func (m sourceMap) LineForOffset(off int) (int, bool) {
	if len(m.order) == 0 {
		return 0, false
	}
	k := sort.Search(len(m.order), func(i int) bool { return m.lineSrc[m.order[i]] > off })
	if k == 0 {
		return m.order[0], true
	}
	best := m.lineSrc[m.order[k-1]]
	j := sort.Search(len(m.order), func(i int) bool { return m.lineSrc[m.order[i]] >= best })
	return m.order[j], true
}

// LineForSourceLine returns the rendered line for 0-based source line — the
// cursor-sync lookup. Content starting on that source line wins (the earliest
// of it); a source line without rendered content of its own (markup only,
// hidden content, the inside of a script) maps to the content before it.
func (m sourceMap) LineForSourceLine(srcLine int) (int, bool) {
	if len(m.order) == 0 {
		return 0, false
	}
	srcLine = min(max(srcLine, 0), len(m.lineStarts)-1)
	lo, hi := m.lineStarts[srcLine], m.srcLen+1
	if srcLine+1 < len(m.lineStarts) {
		hi = m.lineStarts[srcLine+1]
	}
	j := sort.Search(len(m.order), func(i int) bool { return m.lineSrc[m.order[i]] >= lo })
	if j < len(m.order) && m.lineSrc[m.order[j]] < hi {
		return m.order[j], true
	}
	return m.LineForOffset(lo)
}

// sourceLineOf converts a byte offset into its 0-based source line.
func (m sourceMap) sourceLineOf(off int) int {
	return sort.Search(len(m.lineStarts), func(i int) bool { return m.lineStarts[i] > off }) - 1
}
