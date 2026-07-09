package editor

import (
	"strings"

	"ike/internal/editor/buffer"
	"ike/internal/editor/excmd"
	"ike/internal/editor/history"
	"ike/internal/editor/motion"
	"ike/internal/editor/operator"
)

// exDelete runs ":[range]d [reg]" — delete the range's lines into a register
// (unnamed by default), as one undo unit, leaving the cursor on the line that
// takes the range's place (matching "dd").
func (m Model) exDelete(cmd excmd.Command) Model {
	start, end, err := cmd.Range.Resolve(m.exResolver(), m.cursor.Line)
	if err != "" {
		m.cmdMsg = "E: " + err
		return m
	}
	reg := exRegister(cmd.Args)
	target := operator.LineTarget(start, end)
	m.mutate(func(rec *history.Recorder) buffer.Position {
		return operator.Delete(m.buf, rec, m.regs, reg, target)
	})
	return m
}

// exYank runs ":[range]y [reg]" — yank the range's lines into a register. Like
// vim, the cursor does not move.
func (m Model) exYank(cmd excmd.Command) Model {
	start, end, err := cmd.Range.Resolve(m.exResolver(), m.cursor.Line)
	if err != "" {
		m.cmdMsg = "E: " + err
		return m
	}
	operator.Yank(m.buf, m.regs, exRegister(cmd.Args), operator.LineTarget(start, end))
	return m
}

// exIndent runs ":[range]>" / ":[range]<". shift's sign is the direction and its
// magnitude the repeat count (":>>" shifts twice). Reuses the normal-mode indent
// unit/dedent logic; the whole range is one undo unit and the cursor lands on
// the range's last line at its first non-blank, matching vim.
func (m Model) exIndent(cmd excmd.Command, shift int) Model {
	start, end, err := cmd.Range.Resolve(m.exResolver(), m.cursor.Line)
	if err != "" {
		m.cmdMsg = "E: " + err
		return m
	}
	dir, count := 1, shift
	if shift < 0 {
		dir, count = -1, -shift
	}
	unit := m.tabText()
	m.mutate(func(rec *history.Recorder) buffer.Position {
		for l := start; l <= end && l <= m.buf.LineCount()-1; l++ {
			for k := 0; k < count; k++ {
				if dir > 0 {
					if m.buf.Line(l) != "" {
						rec.Apply(buffer.Insert(buffer.Position{Line: l, Col: 0}, unit))
					}
				} else if n := dedentCols(m.buf.Line(l), m.tabWidth); n > 0 {
					rec.Apply(buffer.Delete(buffer.Range{Start: buffer.Position{Line: l, Col: 0}, End: buffer.Position{Line: l, Col: n}}))
				}
			}
		}
		last := end
		if hi := m.buf.LineCount() - 1; last > hi {
			last = hi
		}
		return motion.FirstNonBlank(m.buf, buffer.Position{Line: last, Col: 0}, 1).Pos
	})
	return m
}

// exRegister reads an optional register name from an ex command's argument (the
// first non-space rune); 0 selects the unnamed register.
func exRegister(args string) rune {
	for _, r := range strings.TrimSpace(args) {
		return r
	}
	return 0
}

// isRun reports whether s is a non-empty run of the single byte c.
func isRun(s string, c byte) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] != c {
			return false
		}
	}
	return true
}
