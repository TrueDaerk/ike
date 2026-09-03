package app

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/host"
	"ike/internal/regextest"
	"ike/internal/ui"
)

// regextester.go is the UI half of the regex tester (#1937), JetBrains'
// "Check RegExp" for IKE: a floating dialog with a pattern line and a
// multi-line test-text area, re-evaluated on every keystroke, with all
// matches highlighted in the text and the capture groups of the selected
// match listed underneath. The evaluation core — matching, groups, span
// mapping, quoting, history — is internal/regextest; this file owns the
// fields, the cursor, the rendering and the key routing, exactly like the
// other shell dialogs (clone_prompt.go, #1349).
//
// Two deliberate choices:
//
//   - The test text is a plain []string with a (line, col) cursor rather than
//     a full editor pane. The tester is scratch space for a pattern, not a
//     buffer: no undo, no vim modes, no file behind it.
//   - Evaluation runs inline for a screenful of text and off the event loop
//     (a tea.Cmd, generation-stamped) past regextest.AsyncThreshold, so a
//     megabyte pasted into the area cannot stall a keystroke. RE2 itself is
//     linear-time, so there is no backtracking blowup to defend against.

// regexFocusPattern / regexFocusText index the two inputs; tab moves between
// them.
const (
	regexFocusPattern = iota
	regexFocusText
)

// regexTextRows is how many rows of the test text are shown at once. The
// area scrolls with the cursor; the shell would otherwise grow past the
// terminal on a large prefill.
const regexTextRows = 12

// regexGroupRows caps the listed capture groups of the selected match — a
// pattern with dozens of groups would push the hints off the dialog.
const regexGroupRows = 12

// regexTesterState is the open tester. text is the test area as lines with a
// rune cursor at (line, col); top is the first shown row. result is the last
// evaluation of pattern against the text, sel the match whose groups are
// listed. hist is the per-session pattern history, histIdx the position while
// browsing it (-1 = editing a live pattern). quote is the copy format
// ctrl+y uses, gen stamps async evaluations so a stale result is dropped, and
// status carries the transient confirmation line.
type regexTesterState struct {
	pattern ui.Field
	text    []string
	line    int
	col     int
	top     int
	focus   int
	result  regextest.Result
	sel     int
	hist    regextest.History
	histIdx int
	quote   regextest.QuoteFormat
	gen     int
	pending bool
	status  string
}

// regexTesterContent implements ui.Content (not ModelContent) so the body
// learns the shell's content width budget and can clip long lines to it.
type regexTesterContent struct{ m Model }

// Title implements ui.Content.
func (c regexTesterContent) Title() string { return "Regex Tester" }

// Render implements ui.Content.
func (c regexTesterContent) Render(width int) string { return c.m.regexTesterBody(width) }

// startRegexTester opens the tester, prefilling the test text from the
// focused editor's visual selection when there is one.
func (m *Model) startRegexTester() tea.Cmd {
	s := &regexTesterState{histIdx: -1, text: []string{""}, hist: m.regexHistory}
	if ed := m.activeEditor(); ed != nil {
		if sel, ok := ed.SelectionText(); ok && sel != "" {
			s.text = strings.Split(strings.TrimSuffix(sel, "\n"), "\n")
			s.focus = regexFocusPattern
		}
	}
	m.regexTester = s
	m.shell.SetContent(regexTesterContent{m: *m})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
	return m.evaluateRegex()
}

// regexTesterOpen reports whether the shell currently shows the tester.
func (m Model) regexTesterOpen() bool { return m.regexTester != nil && m.shell.IsOpen() }

// closeRegexTester records the pattern in the session history and clears the
// dialog. The history outlives the dialog, so reopening offers the last
// patterns again.
func (m *Model) closeRegexTester() {
	if s := m.regexTester; s != nil {
		s.hist.Add(strings.TrimSpace(s.pattern.Text))
		m.regexHistory = s.hist
	}
	m.regexTester = nil
	m.shell.Close()
}

// regexText joins the test area back into one string.
func (s *regexTesterState) regexText() string { return strings.Join(s.text, "\n") }

// regexEvalDoneMsg carries an off-loop evaluation back to the model; gen
// drops results that a newer keystroke already superseded.
type regexEvalDoneMsg struct {
	gen int
	res regextest.Result
}

// evaluateRegex re-runs the pattern over the test text: inline for a normal
// screenful, as a tea.Cmd past regextest.AsyncThreshold.
func (m *Model) evaluateRegex() tea.Cmd {
	s := m.regexTester
	if s == nil {
		return nil
	}
	pattern, text := s.pattern.Text, s.regexText()
	if len(text) <= regextest.AsyncThreshold {
		s.pending = false
		s.gen++
		m.setRegexResult(regextest.Evaluate(pattern, text))
		return nil
	}
	s.gen++
	s.pending = true
	gen := s.gen
	return func() tea.Msg {
		return regexEvalDoneMsg{gen: gen, res: regextest.Evaluate(pattern, text)}
	}
}

// finishRegexEval installs an off-loop result unless a newer one is pending.
func (m *Model) finishRegexEval(msg regexEvalDoneMsg) {
	s := m.regexTester
	if s == nil || msg.gen != s.gen {
		return
	}
	s.pending = false
	m.setRegexResult(msg.res)
}

// setRegexResult stores a fresh result, clamping the selected match.
func (m *Model) setRegexResult(res regextest.Result) {
	s := m.regexTester
	if s == nil {
		return
	}
	s.result = res
	if s.sel >= len(res.Matches) {
		s.sel = 0
	}
}

// updateRegexTester consumes every key while the tester is open.
func (m Model) updateRegexTester(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.regexTester
	if s == nil {
		return m, nil
	}
	s.status = ""
	switch msg.String() {
	case "esc":
		m.closeRegexTester()
		return m, nil
	case "tab", "shift+tab":
		s.focus = (s.focus + 1) % 2
		return m, nil
	case "ctrl+n":
		m.stepRegexMatch(1)
		return m, nil
	case "ctrl+p":
		m.stepRegexMatch(-1)
		return m, nil
	case "ctrl+o":
		s.quote = regextest.QuoteFormats[(int(s.quote)+1)%len(regextest.QuoteFormats)]
		return m, nil
	case "ctrl+y":
		m.copyRegexPattern()
		return m, nil
	}
	if s.focus == regexFocusPattern {
		return m, m.regexPatternKey(msg)
	}
	return m, m.regexTextKey(msg)
}

// regexPatternKey edits the pattern line; up/down browse the session history
// instead of moving a cursor, and enter hands focus to the test area.
func (m *Model) regexPatternKey(msg tea.KeyPressMsg) tea.Cmd {
	s := m.regexTester
	switch msg.String() {
	case "up":
		return m.stepRegexHistory(1)
	case "down":
		return m.stepRegexHistory(-1)
	case "enter":
		s.hist.Add(strings.TrimSpace(s.pattern.Text))
		s.histIdx = -1
		s.focus = regexFocusText
		return nil
	}
	handled, changed := s.pattern.Key(msg)
	if !handled {
		return nil
	}
	if !changed {
		return nil
	}
	s.histIdx = -1
	return m.evaluateRegex()
}

// stepRegexHistory walks the session pattern history (delta +1 = older).
func (m *Model) stepRegexHistory(delta int) tea.Cmd {
	s := m.regexTester
	next := s.histIdx + delta
	if next < -1 {
		next = -1
	}
	if next >= s.hist.Len() {
		return nil
	}
	if next == -1 {
		s.histIdx = -1
		s.pattern.Clear()
		return m.evaluateRegex()
	}
	pattern, ok := s.hist.At(next)
	if !ok {
		return nil
	}
	s.histIdx = next
	s.pattern.Set(pattern)
	return m.evaluateRegex()
}

// stepRegexMatch moves the selection whose groups are listed, wrapping.
func (m *Model) stepRegexMatch(delta int) {
	s := m.regexTester
	n := len(s.result.Matches)
	if n == 0 {
		return
	}
	s.sel = ((s.sel+delta)%n + n) % n
}

// copyRegexPattern writes the pattern to the clipboard in the selected quote
// format (ctrl+o cycles it).
func (m *Model) copyRegexPattern() {
	s := m.regexTester
	if s.pattern.Empty() {
		s.status = "nothing to copy — the pattern is empty"
		return
	}
	quoted := regextest.Quote(s.pattern.Text, s.quote)
	m.copyToClipboard(quoted)
	s.status = "copied " + s.quote.String() + ": " + quoted
	m.host.Notify(host.Info, "copied regex as "+s.quote.String()+" literal")
}

// regexTextKey edits the multi-line test area: line motions and enter/
// backspace joins here, everything else through the shared line editor.
func (m *Model) regexTextKey(msg tea.KeyPressMsg) tea.Cmd {
	s := m.regexTester
	s.clampCursor()
	switch msg.String() {
	case "up":
		if s.line > 0 {
			s.line--
			s.clampCursor()
		}
		return nil
	case "down":
		if s.line < len(s.text)-1 {
			s.line++
			s.clampCursor()
		}
		return nil
	case "enter":
		r := []rune(s.text[s.line])
		head, tail := string(r[:s.col]), string(r[s.col:])
		rest := append([]string{tail}, s.text[s.line+1:]...)
		s.text = append(append(s.text[:s.line:s.line], head), rest...)
		s.line, s.col = s.line+1, 0
		return m.evaluateRegex()
	case "backspace":
		if s.col == 0 && s.line > 0 {
			prev := []rune(s.text[s.line-1])
			s.text[s.line-1] += s.text[s.line]
			s.text = append(s.text[:s.line], s.text[s.line+1:]...)
			s.line, s.col = s.line-1, len(prev)
			return m.evaluateRegex()
		}
	case "delete":
		if s.col == len([]rune(s.text[s.line])) && s.line < len(s.text)-1 {
			s.text[s.line] += s.text[s.line+1]
			s.text = append(s.text[:s.line+1], s.text[s.line+2:]...)
			return m.evaluateRegex()
		}
	}
	out, pos, handled, changed := ui.EditKey(msg, s.text[s.line], s.col)
	if !handled {
		return nil
	}
	s.text[s.line], s.col = out, pos
	if !changed {
		return nil
	}
	return m.evaluateRegex()
}

// clampCursor keeps the cursor inside the text after a line change.
func (s *regexTesterState) clampCursor() {
	if len(s.text) == 0 {
		s.text = []string{""}
	}
	if s.line < 0 {
		s.line = 0
	}
	if s.line >= len(s.text) {
		s.line = len(s.text) - 1
	}
	if n := len([]rune(s.text[s.line])); s.col > n {
		s.col = n
	}
	if s.col < 0 {
		s.col = 0
	}
}

// pasteRegexTester inserts a paste into the focused field: flattened into the
// pattern line, kept multi-line in the test area (a log excerpt is exactly
// what the area is for).
func (m *Model) pasteRegexTester(text string) (tea.Cmd, bool) {
	s := m.regexTester
	if s == nil || text == "" {
		return nil, false
	}
	if s.focus == regexFocusPattern {
		if !s.pattern.Paste(text) {
			return nil, false
		}
		s.histIdx = -1
		return m.evaluateRegex(), true
	}
	s.clampCursor()
	r := []rune(s.text[s.line])
	head, tail := string(r[:s.col]), string(r[s.col:])
	ins := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	ins[0] = head + ins[0]
	last := len(ins) - 1
	s.col = len([]rune(ins[last]))
	ins[last] += tail
	rest := append(append([]string{}, ins...), s.text[s.line+1:]...)
	s.text = append(s.text[:s.line:s.line], rest...)
	s.line += last
	return m.evaluateRegex(), true
}

// regexTesterBody renders the whole dialog into the shell's width budget.
func (m Model) regexTesterBody(width int) string {
	s := m.regexTester
	if s == nil {
		return ""
	}
	if width < 20 {
		width = 20
	}
	pal := m.pal()
	hint := lipgloss.NewStyle().Foreground(pal.Hint)
	var b strings.Builder

	b.WriteString(m.regexPatternRow(width))
	b.WriteString("\n")
	if s.result.Err != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(pal.Error).Render("E: "+s.result.Err) + "\n")
	}
	b.WriteString(hint.Render("Go regexp (RE2) semantics — no backreferences or lookaround; (?i) (?m) (?s) supported") + "\n\n")

	b.WriteString(hint.Render("Test text") + "\n")
	b.WriteString(m.regexTextArea(width))
	b.WriteString("\n")
	b.WriteString(m.regexSummary() + "\n\n")
	b.WriteString(m.regexGroupList(width))
	if s.status != "" {
		b.WriteString("\n" + lipgloss.NewStyle().Foreground(pal.Success).Render(clip(s.status, width)))
	}
	b.WriteString("\n" + hint.Render(clip("tab switch field · ctrl+n/p select match · ↑/↓ pattern history · esc close", width)))
	b.WriteString("\n" + hint.Render(clip("ctrl+y copy pattern as "+s.quote.String()+" literal · ctrl+o next quote format", width)))
	return b.String()
}

// regexPatternRow renders the pattern field, windowed around its cursor.
func (m Model) regexPatternRow(width int) string {
	s := m.regexTester
	avail := width - 12
	if avail < 10 {
		avail = 10
	}
	if s.focus == regexFocusPattern {
		return "> Pattern: " + windowedInput(s.pattern.Text, s.pattern.Cur, avail)
	}
	return "  Pattern: " + windowedPlain(s.pattern.Text, avail)
}

// regexTextArea renders the visible rows of the test text with every match
// highlighted — the selected match in the full selection colors, the others
// muted — and the cursor drawn when the area is focused.
func (m Model) regexTextArea(width int) string {
	s := m.regexTester
	s.clampCursor()
	s.scrollToCursor()
	spans := regextest.LineSpans(s.regexText(), s.result.Matches)
	byLine := map[int][]regextest.Span{}
	for _, sp := range spans {
		byLine[sp.Line] = append(byLine[sp.Line], sp)
	}
	avail := width - 6
	if avail < 10 {
		avail = 10
	}
	var b strings.Builder
	end := min(s.top+regexTextRows, len(s.text))
	for i := s.top; i < end; i++ {
		b.WriteString("  " + m.regexTextRow(i, byLine[i], avail) + "\n")
	}
	if more := len(s.text) - end; more > 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(m.pal().Hint).
			Render("  … "+strconv.Itoa(more)+" more line(s)") + "\n")
	}
	return b.String()
}

// scrollToCursor keeps the cursor row inside the visible window.
func (s *regexTesterState) scrollToCursor() {
	if s.line < s.top {
		s.top = s.line
	}
	if s.line >= s.top+regexTextRows {
		s.top = s.line - regexTextRows + 1
	}
	if s.top > len(s.text)-1 {
		s.top = len(s.text) - 1
	}
	if s.top < 0 {
		s.top = 0
	}
}

// regexTextRow renders one text line: a horizontal window (the cursor's line
// scrolls with it), match highlighting per rune, and the cursor cell.
func (m Model) regexTextRow(line int, spans []regextest.Span, width int) string {
	s := m.regexTester
	pal := m.pal()
	r := []rune(s.text[line])
	off := 0
	if line == s.line && s.col >= width {
		off = s.col - width + 1
	}
	if off > len(r) {
		off = len(r)
	}
	end := min(off+width, len(r))
	selected := lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText)
	other := lipgloss.NewStyle().Background(pal.SelectionMuted)
	cursor := lipgloss.NewStyle().Reverse(true)
	var b strings.Builder
	if off > 0 {
		b.WriteString("…")
	}
	for i := off; i < end; i++ {
		cell := string(r[i])
		switch {
		case line == s.line && i == s.col && s.focus == regexFocusText:
			cell = cursor.Render(cell)
		case matchAt(spans, i) == s.sel && matchAt(spans, i) >= 0:
			cell = selected.Render(cell)
		case matchAt(spans, i) >= 0:
			cell = other.Render(cell)
		}
		b.WriteString(cell)
	}
	if line == s.line && s.col >= end && s.focus == regexFocusText {
		b.WriteString(cursor.Render(" "))
	}
	if end < len(r) {
		b.WriteString("…")
	}
	return b.String()
}

// matchAt reports which match covers rune column col on a line, or -1.
func matchAt(spans []regextest.Span, col int) int {
	for _, sp := range spans {
		if col >= sp.Start && col < sp.End {
			return sp.Match
		}
	}
	return -1
}

// regexSummary is the match-count line: the count, the selected match, and
// the caveats (evaluation in flight, capped scan).
func (m Model) regexSummary() string {
	s := m.regexTester
	pal := m.pal()
	if s.result.Err != "" {
		return lipgloss.NewStyle().Foreground(pal.Error).Render("no matches — the pattern does not compile")
	}
	if s.pending {
		return lipgloss.NewStyle().Foreground(pal.Hint).Render("evaluating…")
	}
	n := len(s.result.Matches)
	if n == 0 {
		if s.pattern.Empty() {
			return lipgloss.NewStyle().Foreground(pal.Hint).Render("no pattern yet")
		}
		return lipgloss.NewStyle().Foreground(pal.Warning).Render("no matches")
	}
	out := fmt.Sprintf("%d match(es) — showing groups of #%d", n, s.sel+1)
	if s.result.Truncated {
		out += fmt.Sprintf(" (stopped at %d)", regextest.MaxMatches)
	}
	return lipgloss.NewStyle().Foreground(pal.Success).Render(out)
}

// regexGroupList lists the capture groups of the selected match: index, name
// when the group is named, and value. Group 0 is the whole match.
func (m Model) regexGroupList(width int) string {
	s := m.regexTester
	pal := m.pal()
	hint := lipgloss.NewStyle().Foreground(pal.Hint)
	if s.sel >= len(s.result.Matches) {
		return hint.Render("Groups: —")
	}
	groups := s.result.Matches[s.sel].Groups
	var b strings.Builder
	b.WriteString(hint.Render("Groups") + "\n")
	shown := min(len(groups), regexGroupRows)
	for _, g := range groups[:shown] {
		label := strconv.Itoa(g.Index)
		if g.Name != "" {
			label += " " + g.Name
		}
		if g.Index == 0 {
			label += " (whole match)"
		}
		value := strconv.Quote(g.Value)
		if !g.Set {
			value = "(no match)"
		}
		b.WriteString("  " + clip(fmt.Sprintf("%-24s %s", label, value), width-2) + "\n")
	}
	if more := len(groups) - shown; more > 0 {
		b.WriteString(hint.Render("  … "+strconv.Itoa(more)+" more group(s)") + "\n")
	}
	return b.String()
}

// clip truncates a plain (unstyled) line to width runes with an ellipsis.
func clip(s string, width int) string {
	if width < 4 {
		width = 4
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width-1]) + "…"
}
