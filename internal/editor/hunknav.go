package editor

import (
	"sort"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/motion"
)

// hunknav.go implements git hunk navigation (#1170): ]c / [c move the cursor
// between the change hunks the gutter marks (#464) describe. A hunk is a
// maximal run of consecutive marked lines — kind changes inside a run do not
// split it, matching what revertHunk treats as one unit and what the gutter
// shows as one block; a LineDeleted row with unmarked neighbours is its own
// single-row hunk. Motion only: no undo involvement and no nav-history
// entries (the diagnostics precedent).

// hunkStarts returns the first line of every hunk, ascending.
func (m *Model) hunkStarts() []int {
	if len(m.gitMarks) == 0 {
		return nil
	}
	lines := make([]int, 0, len(m.gitMarks))
	for ln := range m.gitMarks {
		lines = append(lines, ln)
	}
	sort.Ints(lines)
	starts := make([]int, 0, 4)
	prev := -2
	for _, ln := range lines {
		if ln != prev+1 {
			starts = append(starts, ln)
		}
		prev = ln
	}
	return starts
}

// hunkJump moves to the next/previous hunk start — strictly past the cursor
// line in the walk direction with wrap, the diagnosticJump/conflictJump
// family's semantics — and lands on the line's first non-blank column.
func (m *Model) hunkJump(forward bool) tea.Cmd {
	starts := m.hunkStarts()
	if len(starts) == 0 {
		return notice("no changes in this file")
	}
	pick, found := starts[0], false
	if forward {
		for _, s := range starts {
			if s > m.cursor.Line {
				pick, found = s, true
				break
			}
		}
	} else {
		pick = starts[len(starts)-1]
		for i := len(starts) - 1; i >= 0; i-- {
			if starts[i] < m.cursor.Line {
				pick, found = starts[i], true
				break
			}
		}
	}
	wrapped := ""
	if !found {
		wrapped = " (wrapped)"
	}
	m.SetCursor(pick, 0)
	m.cursor = motion.FirstNonBlank(m.buf, m.cursor, 1).Pos
	m.desiredCol = m.cursor.Col
	m.scroll()
	n := 0
	for i, s := range starts {
		if s == pick {
			n = i + 1
			break
		}
	}
	return notice("change " + strconv.Itoa(n) + "/" + strconv.Itoa(len(starts)) + wrapped)
}
