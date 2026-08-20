package editor

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/concealexplain"
	"ike/internal/editor/buffer"
	"ike/internal/epochtime"
	"ike/internal/lang"
	"ike/internal/numhint"
	"ike/internal/secret"
)

// explainconceal.go is the explain & override popover for concealed and masked
// values (#1998). Conceal families are heuristics — a field name mapped to a
// unit (#1685), a built-in key word, the shape of the digits, a secret key
// pattern (#1712) — and until now a misfire was invisible from the buffer: a
// byte count drawn as a date, an over-masked assignment, a credential left
// readable. The popover answers the question on the spot ("which rule fired,
// what did it decide") and lets the answer be corrected without leaving the
// file: one key writes a rule into the very settings the heuristics already
// read (editor.number_hint_units, editor.secret_masking_keys), so the fix is
// the same entry the Settings UI lists and edits.
//
// The provenance itself is not computed here: internal/concealexplain resolves
// a span back to the rule that produced it, out of the hint sources' own
// bookkeeping. This file is the caret → span → popover half, plus the keys.

// explainState is the open popover: the resolved explanation, the span it was
// opened on, and whether the family that owns the span is currently drawing.
type explainState struct {
	ex      concealexplain.Explanation
	line    int
	capture string
	drawing bool
}

// ConcealRuleMsg asks the app to persist a rule the popover produced (#1998).
// Setting names the existing store — editor.number_hint_units or
// editor.secret_masking_keys — Entry is the line to write and Pattern its
// left-hand side, so an entry already covering the same key is replaced rather
// than shadowed (earlier entries win in both stores).
type ConcealRuleMsg struct {
	Setting string
	Entry   string
	Pattern string
	Note    string
}

// explainConceal opens the popover for the value at the caret: the conceal
// stand-in the caret sits on or next to (#1686's one-column widening, so a
// caret appended to a literal still explains it), else the plain value under
// the caret — "why is this *not* masked" is the same question.
func (m *Model) explainConceal() tea.Cmd {
	line := m.cursor.Line
	if line >= m.buf.LineCount() {
		return notice("nothing to explain here")
	}
	text := m.buf.Line(line)
	req := concealexplain.Request{Line: text, Col: m.cursor.Col, Lang: m.langID()}
	st := &explainState{line: line}
	if capture, r, ok := m.concealAtCaret(); ok {
		req.Start, req.End, req.Capture, req.Display = r.start, r.end, capture, r.repl
		st.capture, st.drawing = capture, m.decodeOn(capture)
	}
	ex, ok := concealexplain.Explain(req)
	if !ok {
		return notice("no value under the caret to explain")
	}
	st.ex = ex
	m.explain = st
	return nil
}

// explainCaptures are the stand-in families the popover speaks for: the four
// number-readability families (#1627, #1701 emits into the same ones), the
// decoded epoch timestamps (#1618) and the secret masks (#1623). The other
// stand-ins — escape decodings, cron schedules, PEM summaries — rest on the
// text itself rather than on a field-name heuristic, so there is no rule to
// name and no classification to correct. They are skipped, and a caret on one
// explains the plain value under it instead.
var explainCaptures = []string{
	numhint.SizeCapture,
	numhint.DurationCapture,
	numhint.GroupCapture,
	numhint.RadixCapture,
	epochtime.Capture,
	secret.Capture,
}

// concealAtCaret returns the stand-in the caret sits on or directly next to,
// with its capture. Families are searched in the fixed order above, so an
// overlap resolves the way rendering does, and a family switched off in this
// view still answers — a stand-in that is not drawing is exactly the case a
// user asks about.
func (m Model) concealAtCaret() (string, concealRange, bool) {
	line, col := m.cursor.Line, m.cursor.Col
	for _, c := range explainCaptures {
		for _, r := range m.decodes[c][line] {
			if col >= r.start-1 && col <= r.end {
				return c, r, true
			}
		}
	}
	return "", concealRange{}, false
}

// langID resolves the buffer's language id for the explain path, which needs
// it to evaluate a constant's right-hand side in the right flavour.
func (m Model) langID() string {
	if m.path == "" {
		return ""
	}
	if l, ok := lang.ByPath(m.path); ok {
		return l.ID
	}
	return ""
}

// ExplainOpen reports whether the explain popover is showing.
func (m Model) ExplainOpen() bool { return m.explain != nil }

// ExplainAnchor returns the buffer-relative cell the popover anchors to: the
// start of the explained value, so the box points at what it talks about.
func (m Model) ExplainAnchor() (col, line int) {
	if m.explain == nil {
		return m.cursor.Col, m.cursor.Line
	}
	return m.explain.ex.Start, m.explain.line
}

// dismissExplain closes the popover.
func (m *Model) dismissExplain() { m.explain = nil }

// explainKey handles a key while the popover is open. It returns true when the
// key was consumed; any other key closes the popover and returns false so
// normal dispatch handles it, like the peek popup (#1154).
func (m *Model) explainKey(key tea.KeyPressMsg) (bool, tea.Cmd) {
	st := m.explain
	r, hasRune := firstRune(key)
	switch {
	case key.Code == tea.KeyEscape:
		m.dismissExplain()
		return true, nil
	case hasRune && r == 'q':
		m.dismissExplain()
		return true, nil
	case hasRune && r == 'r':
		// Reveal (#1686): the caret inside the span *is* the reveal, so the
		// popover simply puts it there and gets out of the way.
		m.revealExplained()
		return true, nil
	case hasRune && st.ex.Kind == concealexplain.KindSecret && (r == 'm' || r == 'u'):
		return true, m.secretRule(r == 'm')
	case hasRune && st.ex.Kind == concealexplain.KindNumber:
		if c, ok := concealexplain.ChoiceFor(r); ok {
			return true, m.unitRule(c.Unit)
		}
		if r == 'a' && st.ex.Unit != "" {
			// Pin the reading the heuristic chose, so the same field keeps it
			// when the heuristic's input changes.
			return true, m.unitRule(st.ex.Unit)
		}
	}
	m.dismissExplain()
	return false, nil
}

// revealExplained moves the caret into the explained span and closes the
// popover: the positional reveal (#1594) then shows the raw value in place.
func (m *Model) revealExplained() {
	st := m.explain
	m.dismissExplain()
	m.moveTo(buffer.Position{Line: st.line, Col: st.ex.Start})
	m.scroll()
}

// unitRule persists the field rule pinning the explained value's field to
// unit, through the app (the editor writes no config of its own).
func (m *Model) unitRule(unit string) tea.Cmd {
	st := m.explain
	key := st.ex.Key
	m.dismissExplain()
	if key == "" {
		return notice("no field name to write a rule for")
	}
	entry := concealexplain.UnitEntry(key, unit)
	return func() tea.Msg {
		return ConcealRuleMsg{
			Setting: concealexplain.UnitsSetting,
			Entry:   entry,
			Pattern: concealexplain.EntryPattern(entry),
			Note:    "number hints: " + entry,
		}
	}
}

// secretRule persists the masking rule for the explained value's key: mask it
// always, or never.
func (m *Model) secretRule(mask bool) tea.Cmd {
	st := m.explain
	key := st.ex.Key
	m.dismissExplain()
	if key == "" {
		return notice("no key name to write a rule for")
	}
	entry := concealexplain.SecretEntry(key)
	note := "secret masking: always mask " + key
	if !mask {
		entry = concealexplain.SecretExemptEntry(key)
		note = "secret masking: never mask " + key
	}
	return func() tea.Msg {
		return ConcealRuleMsg{
			Setting: concealexplain.SecretSetting,
			Entry:   entry,
			Pattern: concealexplain.EntryPattern(entry),
			Note:    note,
		}
	}
}

// ExplainView renders the popover: what the value is, what draws instead,
// which rule decided that, and the one-key overrides.
func (m Model) ExplainView() string {
	st := m.explain
	if st == nil {
		return ""
	}
	th := m.theme()
	head := lipgloss.NewStyle().Foreground(th.Accent).Bold(true)
	dim := lipgloss.NewStyle().Foreground(th.Border)
	label := lipgloss.NewStyle().Foreground(th.Hint)
	width := m.popupMaxWidth()
	var rows []string
	rows = append(rows, head.Render(explainTitle(st)))
	rows = append(rows, dim.Render(strings.Repeat("─", width)))
	field := func(name, value string) {
		if value == "" {
			return
		}
		for i, l := range wrapPlain(value, width-9) {
			tag := name
			if i > 0 {
				tag = ""
			}
			rows = append(rows, label.Render(fmt.Sprintf("%-8s", tag))+l)
		}
	}
	field("value", st.ex.Raw)
	field("shows", st.ex.Display)
	field("field", st.ex.Key)
	field("rule", st.ex.Rule)
	field("reading", st.ex.Reading)
	if st.ex.Family != "" && !st.drawing && st.capture != "" {
		field("note", "editor."+st.ex.Family+" is off here, so nothing draws")
	}
	rows = append(rows, dim.Render(strings.Repeat("─", width)))
	for _, l := range m.explainActions(st) {
		rows = append(rows, ansi.Truncate(l, width, "…"))
	}
	return m.popupFrame().Padding(0, 1).Render(strings.Join(rows, "\n"))
}

// explainTitle names what the popover is about — the family that drew the
// span, or the fact that nothing did.
func explainTitle(st *explainState) string {
	if st.ex.Kind == concealexplain.KindSecret {
		if st.ex.Masked {
			return "Masked value — secret masking"
		}
		return "Unmasked value — secret masking"
	}
	if st.capture == "" {
		return "Plain value — no conceal"
	}
	return "Concealed value — editor." + st.ex.Family
}

// explainActions lists the one-key overrides for the popover's kind.
func (m Model) explainActions(st *explainState) []string {
	th := m.theme()
	key := lipgloss.NewStyle().Foreground(th.Accent).Bold(true)
	label := lipgloss.NewStyle().Foreground(th.Hint)
	item := func(k, text string) string { return key.Render(k) + " " + text }
	if st.ex.Kind == concealexplain.KindSecret {
		return []string{
			item("r", "reveal") + label.Render("   esc close"),
			item("m", "always mask this key") + label.Render(secretHint(st, true)),
			item("u", "never mask this key") + label.Render(secretHint(st, false)),
		}
	}
	rows := []string{item("r", "reveal") + label.Render("   esc close")}
	if st.ex.Unit != "" {
		rows = append(rows, item("a", "pin this reading as a field rule ")+label.Render("("+concealexplain.UnitEntry(st.ex.Key, st.ex.Unit)+")"))
	}
	rows = append(rows, label.Render("reclassify "+entryPreview(st.ex.Key)+" as:"))
	// The reclassify grid lists the mapping words themselves, not prose: they
	// are what lands in editor.number_hint_units, and they fit three to a row
	// where the readings would truncate.
	var row []string
	choices := concealexplain.Choices()
	for i, c := range choices {
		cell := item(string(c.Key), c.Unit)
		if pad := explainCellWidth - 2 - lipgloss.Width(c.Unit); pad > 0 && i%3 != 2 {
			cell += strings.Repeat(" ", pad)
		}
		row = append(row, cell)
		if len(row) == 3 || i == len(choices)-1 {
			rows = append(rows, "  "+strings.Join(row, ""))
			row = nil
		}
	}
	return rows
}

// explainCellWidth is the column width of the reclassify grid, sized to the
// longest mapping word plus its key and a gap.
const explainCellWidth = 16

// entryPreview names the field a reclassification writes a rule for, in the
// lowercase form the mapping matches in.
func entryPreview(key string) string {
	if key == "" {
		return "the field"
	}
	return strings.ToLower(key)
}

// secretHint spells out the entry a masking key writes, so the popover shows
// what lands in the settings before it lands there.
func secretHint(st *explainState, mask bool) string {
	if st.ex.Key == "" {
		return ""
	}
	if mask {
		return "   (" + concealexplain.SecretEntry(st.ex.Key) + ")"
	}
	return "   (" + concealexplain.SecretExemptEntry(st.ex.Key) + ")"
}

// wrapPlain breaks text into rows of at most width cells on word boundaries —
// the popover's rule sentences are prose, not code, so they wrap rather than
// truncate.
func wrapPlain(text string, width int) []string {
	if width < 8 {
		width = 8
	}
	var out []string
	line := ""
	for _, w := range strings.Fields(text) {
		switch {
		case line == "":
			line = w
		case lipgloss.Width(line)+1+lipgloss.Width(w) <= width:
			line += " " + w
		default:
			out = append(out, line)
			line = w
		}
	}
	if line != "" {
		out = append(out, line)
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}
