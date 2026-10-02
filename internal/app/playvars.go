package app

// playvars.go is the playground's variables line (#2786): jq's `--arg` /
// `--argjson` as a header row under the query, holding `name=value` entries
// (`id=42 name="alice" tags=["a","b"]`) that every run binds as `$name`. A
// value is JSON when it parses as JSON and a string otherwise
// (jqplay.ParseVars); a line that does not parse takes the info row the way a
// compile error does, and `select(.id == $id)` without `id=` on the line
// reports gojq's own "variable not defined".
//
// The row is toggled by playground.variables (default ctrl+alt+b): the first
// press shows and focuses it, a press while it is up but unfocused focuses
// it, and a press from the line itself hides it. Only a shown line binds —
// hiding it keeps the text for the next show but runs the program without
// it, so the header always says what the result was computed with. The
// header grows by the row only while it is shown (playVarsRows), and the
// variables travel with the program into the saved filters and the
// per-source last-program memory. xmq has no `$name`: its variables are
// exported to the CLI's environment instead.

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/jqplay"
)

// TogglePlayVarsMsg is playground.variables: show and focus the variables
// line, focus it when it is up, hide it when it already has the keyboard.
type TogglePlayVarsMsg struct{}

// playVarsExample is the placeholder an empty, unfocused variables line shows.
const playVarsExample = `name=value … e.g. id=42 name="alice"`

// playVarsRows is the height the variables line adds to the header: one row
// while it is shown, none otherwise.
func (m Model) playVarsRows() int {
	if s := m.play; s != nil && s.varsShown {
		return 1
	}
	return 0
}

// playVarsText is the variables line the runs bind: its text while shown, ""
// while hidden.
func (s *playState) playVarsText() string {
	if s.varsShown {
		return s.vars.Text
	}
	return ""
}

// runOpts are the options a run is handed: the toggles plus the shown
// variables line.
func (s *playState) runOpts() jqplay.Options {
	o := s.opts
	o.Vars = s.playVarsText()
	return o
}

// seedVars installs the variables a seed or a picked filter carries; a
// non-empty line is shown so what binds is on screen, an empty one leaves the
// row as it was.
func (s *playState) seedVars(vars string) {
	if vars = strings.TrimSpace(vars); vars == "" {
		return
	}
	s.vars.Set(vars)
	s.varsShown = true
}

// focusPlayVars gives the variables line the keyboard.
// The query line's completion popup closes: it completes typing there.
func (s *playState) focusPlayVars() {
	s.program.Deselect()
	s.setBufFocus(false)
	s.varsFocus, s.comp = true, nil
}

// togglePlayVars is playground.variables. Showing or hiding a line with
// entries changes what the program binds, so either reruns at once; focusing
// an already shown line changes nothing and does not.
func (m *Model) togglePlayVars() tea.Cmd {
	s := m.play
	if s == nil {
		return nil
	}
	if !s.varsShown {
		s.varsShown = true
		s.focusPlayVars()
		s.status, s.statusWarn = "variables: name=value entries bind $name — enter runs, esc returns", false
		m.sizePlayResult()
		if strings.TrimSpace(s.vars.Text) != "" {
			return m.runPlayNow()
		}
		return nil
	}
	if !s.varsFocus {
		s.focusPlayVars()
		return nil
	}
	s.varsShown = false
	s.setBufFocus(false)
	s.status, s.statusWarn = "variables line hidden — its bindings no longer apply", false
	m.sizePlayResult()
	return m.runPlayNow()
}

// updatePlayVarsKey routes a key into the variables line: esc and enter hand
// the keyboard back to the query line (enter runs at once, as it does there),
// tab moves on to the result buffer, and everything else is line editing that
// re-runs debounced. A key the line does not claim resolves against the
// Global scope, so the toggle's own chord hides the line from here.
func (m Model) updatePlayVarsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.play
	switch msg.String() {
	case "esc", "up":
		s.setBufFocus(false)
		return m, nil
	case "enter":
		s.setBufFocus(false)
		return m, m.runPlayNow()
	case "tab":
		s.setBufFocus(true)
		return m, nil
	}
	handled, changed := s.vars.Key(msg)
	if !handled {
		if ok, cmd := m.playGlobalChord(msg); ok {
			return m, cmd
		}
		m.playMissKey()
		return m, nil
	}
	if !changed {
		return m, nil
	}
	return m, tea.Batch(m.schedulePlayEval(), m.playTyped())
}

// playVarsRowY is the content-local row of the variables line: right above
// the info row.
func (m Model) playVarsRowY() int { return m.playInfoRowY() - 1 }

// clickPlayVarsRow focuses the variables line and puts its caret on the
// clicked cell, reporting whether the press was on the line at all.
func (m *Model) clickPlayVarsRow(x, y int) bool {
	s := m.play
	if !s.varsShown || y != m.playVarsRowY() {
		return false
	}
	s.focusPlayVars()
	r, ok := m.lay.Panes[s.paneKey]
	if !ok {
		return true
	}
	if col := x - m.playPrefixW(); col >= 0 {
		s.vars.Cur = playOneLinePos(s.vars.Text, s.vars.Cur, m.playQueryWidth(paneInterior(r.W, paneChromeW)), col)
	}
	return true
}

// playVarsRow renders the variables line under the query: a `$:` label in the
// query label's column — `>` marks it while it has the keyboard — then the
// entries windowed around the caret and coloured by jqplay.VarTokens. The
// label turns Error while the line itself is what fails to parse. An empty
// line without the keyboard shows an example instead.
func (m Model) playVarsRow(width int) string {
	s := m.play
	pal := m.pal()
	arrow, pos := "  ", -1
	if s.varsFocus && m.playFocused() {
		arrow, pos = "> ", s.vars.Cur
	}
	labelStyle := lipgloss.NewStyle().Foreground(pal.Secondary)
	if s.compileBad && strings.HasPrefix(s.runErr, "variables:") {
		labelStyle = lipgloss.NewStyle().Foreground(pal.Error).Bold(true)
	}
	label := labelStyle.Render(arrow + "$:" + strings.Repeat(" ", max(m.playPrefixW()-4, 0)))
	if s.vars.Text == "" && pos < 0 {
		return label + lipgloss.NewStyle().Foreground(pal.Hint).Render(playVarsExample)
	}
	r := s.vars.Runes()
	avail := m.playQueryWidth(width)
	if pos > len(r) {
		pos = len(r)
	}
	start := 0
	if pos >= avail {
		start = pos - avail + 1
	}
	end := min(start+avail, len(r))
	tokens := jqplay.VarTokens(s.vars.Text)
	styles := m.playFieldStyles(pos >= 0 && s.vars.Selected())
	cursor := lipgloss.NewStyle().Reverse(true)
	var b strings.Builder
	b.WriteString(label)
	if start > 0 {
		b.WriteString("…")
	}
	for i := start; i < end; i++ {
		cell := string(r[i])
		if i == pos {
			b.WriteString(cursor.Render(cell))
			continue
		}
		b.WriteString(styles[jqplay.KindAt(tokens, i)].Render(cell))
	}
	if pos >= 0 && pos >= end {
		b.WriteString(cursor.Render(" "))
	}
	if end < len(r) {
		b.WriteString("…")
	}
	return b.String()
}

// playVarsHints are the info row's hints while the variables line has the
// keyboard.
func (m Model) playVarsHints() []string {
	return []string{"enter run", "esc query line", "tab result", m.playCommandChord("playground.variables") + " hide", `name=value · JSON or string`, playHelpHint}
}

// pastePlayVars inserts a paste into the variables line, flattened like the
// query line's, reporting whether it had the keyboard to take it.
func (m *Model) pastePlayVars(text string) (tea.Cmd, bool) {
	s := m.play
	if !s.varsFocus {
		return nil, false
	}
	if !s.vars.Paste(text) {
		return nil, true
	}
	return m.schedulePlayEval(), true
}
