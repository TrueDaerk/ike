package editor

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/highlight"
)

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
	if !m.stickyScroll || m.view.Top <= 0 {
		return nil
	}
	// Separator-delimited files pin their title row (#1589): the first line
	// stays visible while the body scrolls, riding the ordinary sticky
	// machinery (rendering, click remap, unhideCursor).
	if m.svActive() && m.view.Height() > 1 && m.buf.LineCount() > 0 {
		return []int{0}
	}
	// Large-file mode ships no Tree-sitter parse, so scopes stay empty — but
	// gate explicitly (#1910) so headers can never pin from stale scope data,
	// e.g. after a same-path reload that crossed the size limit.
	if m.InsightOff() {
		return nil
	}
	if len(m.scopes) == 0 {
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
	// The fixed-point loop below scans every scope up to stickyDepth times,
	// and View, the scroll paths and the mouse map all ask per frame (#2187):
	// memoize it against everything it reads. The guards above stay outside
	// the memo — they are field checks, and keeping them there means a
	// large-file or separator-view pane never touches the cache at all.
	key := stickyKey{top: m.view.Top, max: max, lines: m.buf.LineCount(), scopeEpoch: m.scopeEpoch, docVersion: m.docVersion}
	if c := m.stickyCache; c != nil && c.valid && c.key == key {
		return c.lines
	}
	var lines []int
	for {
		ref := m.view.Top + len(lines)
		if ref >= m.buf.LineCount() {
			break
		}
		enclosing := m.enclosingHeaders(ref, max)
		if len(enclosing) <= len(lines) {
			lines = enclosing
			break
		}
		lines = enclosing
	}
	if c := m.stickyCache; c != nil {
		*c = stickyStore{key: key, lines: lines, valid: true}
	}
	return lines
}

// stickyKey is everything stickyLines' fixed-point loop reads: the viewport
// top and the depth cap (which folds in the pane height and stickyDepth), the
// buffer line count, and the two versions the scopes move with — scopeEpoch
// for a parse landing or a reset, docVersion for an edit that shifted the
// lines the stale scopes still point at.
type stickyKey struct {
	top        int
	max        int
	lines      int
	scopeEpoch int
	docVersion int
}

// stickyStore memoizes the pinned header lines across Model value copies
// (#2187); the single-threaded update loop is the only writer.
type stickyStore struct {
	key   stickyKey
	lines []int
	valid bool
}

// setScopes replaces the sticky-scroll scopes and invalidates the header memo
// keyed on them. Every scopes write funnels through here.
func (m *Model) setScopes(s []highlight.Scope) {
	m.scopes = s
	m.scopeEpoch++
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

// stickySeparate fills the right padding of the last pinned row with a faint
// dashed rule (#1910), the subtle separator marking where the headers end and
// the scrolling body begins. A row whose content already reaches the text
// width renders unchanged — the rule is a hint, not a reserved column.
func (m Model) stickySeparate(row string, textWidth int) string {
	w := ansi.StringWidth(strings.TrimRight(ansi.Strip(row), " "))
	if w+2 >= textWidth {
		return row
	}
	body := ansi.Truncate(row, w+1, "")
	if pad := w + 1 - ansi.StringWidth(body); pad > 0 {
		body += strings.Repeat(" ", pad)
	}
	rule := strings.Repeat("╌", textWidth-w-1)
	return body + lipgloss.NewStyle().Faint(true).Render(rule)
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
