package undotree

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"ike/internal/diff"
)

// Source supplies the buffer text the overlay diffs against: the live content
// and any historical state (#2143). *editor.Model implements it; tests use a
// stub. A nil source simply renders no preview.
type Source interface {
	// Text is the current buffer content.
	Text() string
	// HistoryContentAt reconstructs the content at a history state; the
	// second result is false when that state is gone (pruned).
	HistoryContentAt(seq int) (string, bool)
}

// previewContext is how many unchanged lines surround a change in the preview.
const previewContext = 1

// SetSource installs the buffer the preview diffs against and drops the cache.
func (m *Model) SetSource(src Source) {
	m.src = src
	m.cache = nil
}

// previewHeight is the preview window under the list: a quarter of the screen,
// bounded so a tiny terminal still shows the list and a tall one does not turn
// the overlay into a diff viewer.
func (m *Model) previewHeight() int {
	h := m.height / 4
	if h < 3 {
		h = 3
	}
	if h > 10 {
		h = 10
	}
	return h
}

// preview returns the rendered diff lines for the selected node, memoized per
// seq — View runs on every keystroke and the reconstruction replays edits.
func (m *Model) preview(seq, height int) []string {
	if m.src == nil {
		return nil
	}
	if lines, ok := m.cache[seq]; ok {
		return clipPreview(lines, height)
	}
	target, ok := m.src.HistoryContentAt(seq)
	if !ok {
		return []string{"(state no longer available)"}
	}
	lines := diffLines(m.src.Text(), target)
	if m.cache == nil {
		m.cache = make(map[int][]string)
	}
	m.cache[seq] = lines
	return clipPreview(lines, height)
}

// clipPreview trims the rendered diff to the window, reporting the remainder.
func clipPreview(lines []string, height int) []string {
	if height < 1 || len(lines) <= height {
		return lines
	}
	out := make([]string, 0, height)
	out = append(out, lines[:height-1]...)
	return append(out, "… "+strconv.Itoa(len(lines)-height+1)+" more lines")
}

// diffLines renders the inline (unified) diff from the current buffer to the
// selected state: "-" lines disappear when jumping there, "+" lines appear.
// Only the changed regions plus previewContext lines around them are kept,
// with a "@@" marker between non-adjacent regions.
func diffLines(current, target string) []string {
	if current == target {
		return []string{"(identical to the current buffer)"}
	}
	res := diff.Compute(current, target)
	if len(res.Hunks) == 0 {
		return []string{"(identical to the current buffer)"}
	}

	keep := make([]bool, len(res.Rows))
	for _, h := range res.Hunks {
		for i := h.Start - previewContext; i < h.End+previewContext; i++ {
			if i >= 0 && i < len(res.Rows) {
				keep[i] = true
			}
		}
	}

	var out []string
	gap := false
	for i, r := range res.Rows {
		if !keep[i] {
			gap = true
			continue
		}
		if gap && len(out) > 0 {
			out = append(out, "@@")
		}
		gap = false
		switch r.Kind {
		case diff.RowSame:
			out = append(out, " "+r.Left)
		case diff.RowRemoved:
			out = append(out, "-"+r.Left)
		case diff.RowAdded:
			out = append(out, "+"+r.Right)
		case diff.RowChanged:
			out = append(out, "-"+r.Left, "+"+r.Right)
		}
	}
	return out
}

// renderPreview styles one preview line by its diff prefix.
func (m *Model) renderPreview(line string, width int) string {
	pal := m.theme()
	st := lipgloss.NewStyle().MaxWidth(width)
	switch {
	case len(line) == 0:
		return ""
	case line[0] == '+':
		st = st.Foreground(pal.Success)
	case line[0] == '-':
		st = st.Foreground(pal.Error)
	case line[0] == '@' || line[0] == '(' || strings.HasPrefix(line, "…"):
		// Hunk gaps, status notes and the truncation marker are chrome.
		st = st.Faint(true)
	}
	return st.Render(line)
}
