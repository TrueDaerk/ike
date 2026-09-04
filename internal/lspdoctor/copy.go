package lspdoctor

// copy.go holds the pane's clipboard route (#2487). Telemetry recorded cmd+c
// pressed in the doctor pane with no binding: the report is exactly what a
// user wants to paste into a bug report, and the pane had no way to hand it
// out. The rendering here is deliberately plain — no styling, no width clip —
// so what lands on the clipboard is the whole report as text; the host owns
// the clipboard seam and the confirmation toast, like every other pane copy.

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// CopyMsg asks the host to put the rendered report on the clipboard.
type CopyMsg struct {
	Text string
	What string
}

// CopyKeyCmd is lsp.doctor.copy ('c' / cmd+c): the whole report as plain
// text. It resolves to nil while there is nothing to copy, so the chord is a
// no-op instead of putting an empty string on the clipboard.
func (m *Model) CopyKeyCmd() tea.Cmd {
	text := m.PlainText()
	if text == "" {
		return nil
	}
	msg := CopyMsg{Text: text, What: "LSP Doctor report"}
	return func() tea.Msg { return msg }
}

// PlainText renders the report for the clipboard: the summary header plus
// every row of the flattened report — each server line, its checks, the
// diagnosis, the fix and the re-run verdict — unstyled and unclipped, so no
// ANSI escape and no truncation reaches the paste. It returns "" when no run
// has finished yet.
func (m *Model) PlainText() string {
	if m.report == nil || !m.report.Ran() {
		return ""
	}
	var b strings.Builder
	b.WriteString("LSP Doctor — " + m.summary() + "\n")
	for _, r := range m.rows() {
		b.WriteString(plainGlyph(r.status) + " " + r.text + "\n")
	}
	return b.String()
}

// summary is the header's server / failure counts as plain text, shared by
// the styled header line and the clipboard rendering.
func (m *Model) summary() string {
	n, failing := len(m.report.Results()), 0
	for _, r := range m.report.Results() {
		if r.Failed() {
			failing++
		}
	}
	out := strconv.Itoa(n) + " server"
	if n != 1 {
		out += "s"
	}
	if failing > 0 {
		out += " · " + strconv.Itoa(failing) + " failing"
	}
	return out
}

// plainGlyph is the status marker of a copied row — the same glyphs the pane
// draws, without the colour.
func plainGlyph(s Status) string {
	switch s {
	case StatusWarn:
		return "⚠"
	case StatusFail:
		return "✗"
	case StatusSkip:
		return "·"
	default:
		return "✓"
	}
}
