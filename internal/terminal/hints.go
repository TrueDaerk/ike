package terminal

// hints.go — keyboard link hints (#2254), the keyboard route to the file:line
// references cmd+click already opens (#1168, links.go).
//
// `cmd+shift+l` while a terminal is focused enters hint mode: every reference
// on the *visible* rows that resolves to an existing file gets a short label
// stamped over its first cell (a, s, d, f … the home row first), the bottom
// row turns into a prompt, and typing one label opens that file:line through
// the app's ordinary open funnel. esc — or any key that is not a label —
// leaves the mode without opening anything, so nothing leaks into the shell
// mid-mode.
//
// Why labels and not next/prev-with-enter: a compiler run prints a screenful
// of references at once and the interesting one is rarely the newest, so
// stepping would cost O(n) keys where a label costs one; the labels also make
// the set of *live* (existing-file) references visible at a glance, which
// stepping cannot show without moving. The mode is transient — one keystroke
// opens or cancels — so covering a row with the prompt is cheap.
//
// Performance posture is #1168's, unchanged: the existence os.Stat runs at
// ACTIVATION time only — once per visible reference when the chord is
// pressed, never per render. Rendering hint labels is a pure string splice
// over rows that were already scanned for the underline affordance.

import (
	"image/color"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// hintLabels are the label characters in typing order: the home row first,
// then the rest of the alphabet. Single characters only — one keystroke per
// jump; references past the last label stay unlabelled (and clickable).
const hintLabels = "asdfghjklqwertyuiopzxcvbnm"

// linkHint is one labelled reference on the visible viewport: the label, the
// pane-local cell the label is stamped on, and the already-resolved target
// (absolute path, 0-based line/column) ready for openPathAt.
type linkHint struct {
	label      byte
	row, col   int
	path       string
	line, tcol int
}

// linkHints is the open hint mode: the labelled references of the viewport as
// it looked when the mode opened. Held behind a pointer so value-receiver
// View copies share it, like the scrollback search.
type linkHints struct {
	items []linkHint
}

// Hinting reports whether link hint mode is open.
func (m Model) Hinting() bool { return m.hints != nil }

// StartLinkHints opens hint mode over the visible references, reporting
// whether it took the chord. It stays out of the way — leaving the chord with
// the child — under an alt-screen or mouse-reporting child (vim, lazygit bind
// their own chords), and reports false when no visible reference resolves to
// an existing file: a mode with nothing to pick would only have to be
// dismissed again.
func (m *Model) StartLinkHints() bool {
	if m.sess == nil || m.sess.AltScreen() || m.sess.WantsMouse() || m.search != nil {
		return false
	}
	items := m.collectHints()
	if len(items) == 0 {
		return false
	}
	m.hints = &linkHints{items: items}
	return true
}

// StopLinkHints closes hint mode (a focus change or a resize invalidates the
// captured viewport).
func (m *Model) StopLinkHints() { m.hints = nil }

// collectHints scans the rows currently on screen — scrollback window
// included — for references, resolves each against the session cwd and
// labels the survivors top-down, left-to-right. The bottom row is skipped:
// the prompt line takes it while the mode is open (and in a scrolled view it
// already carries the scrollback marker).
func (m Model) collectHints() []linkHint {
	if m.sess == nil || m.h < 2 {
		return nil
	}
	sb := m.sess.ScrollbackLen()
	first := sb - clamp(m.scroll, 0, sb)
	cwd := m.sess.Cwd()
	var out []linkHint
	for row := 0; row < m.h-1 && len(out) < len(hintLabels); row++ {
		v := first + row
		if v >= sb+m.h {
			break
		}
		for _, l := range scanLinks(m.sess.LineText(v)) {
			p, ok := resolveLink(l, cwd)
			if !ok {
				continue // the activation-time stat gate, as for cmd+click
			}
			c := 0
			if l.col > 0 {
				c = l.col - 1
			}
			out = append(out, linkHint{
				label: hintLabels[len(out)],
				row:   row,
				col:   l.start,
				path:  p,
				line:  l.line - 1,
				tcol:  c,
			})
			if len(out) == len(hintLabels) {
				break
			}
		}
	}
	return out
}

// LinkHintKey feeds one key to open hint mode. It always consumes the key —
// mid-mode nothing may reach the shell — and reports the picked target when
// the key was a label: esc and every other key just close the mode.
func (m *Model) LinkHintKey(msg tea.KeyPressMsg) (path string, line, col int, ok bool) {
	h := m.hints
	m.hints = nil
	if h == nil || msg.Code == tea.KeyEscape {
		return "", 0, 0, false
	}
	if len(msg.Text) != 1 {
		return "", 0, 0, false
	}
	for _, it := range h.items {
		if it.label == msg.Text[0] {
			return it.path, it.line, it.tcol, true
		}
	}
	return "", 0, 0, false
}

// hintView stamps the labels over the rendered view and replaces its bottom
// row with the mode prompt.
func (m Model) hintView(view string) string {
	h := m.hints
	if h == nil {
		return view
	}
	rows := strings.Split(view, "\n")
	for len(rows) < m.h {
		rows = append(rows, "")
	}
	style := m.hintStyle()
	for _, it := range h.items {
		if it.row < 0 || it.row >= len(rows) {
			continue
		}
		rows[it.row] = stampCell(rows[it.row], it.col, style.Render(string(rune(it.label))))
	}
	rows[len(rows)-1] = m.hintLine(len(h.items))
	return strings.Join(rows, "\n")
}

// hintStyle is the label chrome: the theme's warning colour as a background
// with the terminal background as ink — loud enough to read as an overlay
// rather than as output. Themeless models fall back to bright yellow.
func (m Model) hintStyle() lipgloss.Style {
	st := lipgloss.NewStyle().Bold(true)
	if m.pal != nil {
		return st.Foreground(m.pal.Background).Background(m.pal.Warning)
	}
	return st.Foreground(lipgloss.Color("0")).Background(lipgloss.Color("11"))
}

// hintLine is the prompt on the pane's bottom row while the mode is open,
// mirroring the search field's shape.
func (m Model) hintLine(n int) string {
	var dim color.Color = lipgloss.Color("245")
	if m.pal != nil {
		dim = m.pal.InlayHint
	}
	text := " " + strconv.Itoa(n) + " link"
	if n != 1 {
		text += "s"
	}
	text += " — type a label to open, esc to cancel"
	if n == len(hintLabels) {
		text = " first " + strconv.Itoa(n) + " links — type a label to open, esc to cancel"
	}
	line := m.hintStyle().Render("HINT") +
		lipgloss.NewStyle().Foreground(dim).Render(text)
	w := m.w
	if w < 1 {
		w = 1
	}
	return ansi.Truncate(line, w, "…")
}

// stampCell replaces the glyph at column col of an ANSI-styled line with a
// pre-styled replacement, padding when the line ends before the column. A
// wide glyph under the label is replaced whole, like the cursor splice does.
func stampCell(line string, col int, styled string) string {
	done := false
	out, visible := forEachGlyph(line, func(cluster string, c, w int) string {
		if !done && col >= c && col < c+w {
			done = true
			return styled
		}
		return cluster
	})
	if done {
		return out
	}
	var b strings.Builder
	b.WriteString(out)
	if pad := col - visible; pad > 0 {
		b.WriteString(strings.Repeat(" ", pad))
	}
	b.WriteString(styled)
	return b.String()
}
