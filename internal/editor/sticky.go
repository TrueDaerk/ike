package editor

// sticky.go implements sticky scroll (#168): the header lines of the
// declarations enclosing the first visible line are pinned as the top rows of
// the pane, JetBrains/VSCode-style. The scopes come from the same Tree-sitter
// parse that produces the highlight spans (highlight.HighlightScoped); this
// file only decides which headers pin for the current viewport and keeps the
// cursor from hiding behind them.

// stickyLines returns the buffer lines pinned at the top of the view,
// outermost first, capped at stickyDepth (innermost win when capped — the
// nearest context is the most useful one). Empty when the feature is off, the
// view is at the top, or no scope encloses the first content line.
//
// The pinned rows cover the first rows of the viewport, so the reference line
// — the first buffer line still visible below them — moves down as headers
// are added. That makes the count a fixed point: pinning k rows may pull the
// reference line into one more scope. The loop grows k until it stabilises;
// it always terminates because k only grows and is capped.
func (m Model) stickyLines() []int {
	if !m.stickyScroll || m.view.Top <= 0 || len(m.scopes) == 0 {
		return nil
	}
	max := m.stickyDepth
	// Never eat the whole viewport: keep at least one content row.
	if h := m.view.Height() - 1; max > h {
		max = h
	}
	if max <= 0 {
		return nil
	}
	var lines []int
	for {
		ref := m.view.Top + len(lines)
		if ref >= m.buf.LineCount() {
			return lines
		}
		enclosing := m.enclosingHeaders(ref, max)
		if len(enclosing) <= len(lines) {
			return enclosing
		}
		lines = enclosing
	}
}

// enclosingHeaders returns the header lines of the scopes containing line,
// outermost first, keeping only the innermost max entries.
func (m Model) enclosingHeaders(line, max int) []int {
	var out []int
	prev := -1
	for _, s := range m.scopes {
		if s.HeaderLine < line && line <= s.EndLine && s.HeaderLine != prev {
			out = append(out, s.HeaderLine)
			prev = s.HeaderLine
		}
	}
	if len(out) > max {
		out = out[len(out)-max:]
	}
	return out
}

// stickyCount is len(stickyLines) without building the row content; used by
// scroll and mouse handling.
func (m Model) stickyCount() int { return len(m.stickyLines()) }

// unhideCursor scrolls further up when the cursor line would be covered by
// the pinned header rows, so the cursor cell always stays visible. Each pass
// moves Top up, which can only shrink the sticky set, so the loop converges;
// the bound is a safety net.
func (m *Model) unhideCursor() {
	for i := 0; i < m.stickyDepth+2; i++ {
		n := m.stickyCount()
		if n == 0 || m.cursor.Line >= m.view.Top+n {
			return
		}
		top := m.cursor.Line - n
		if top < 0 {
			top = 0
		}
		if top >= m.view.Top {
			return
		}
		m.view.Top = top
	}
}
