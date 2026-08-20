package editor

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/search"
	"ike/internal/ui"
)

// replace_panel.go is the two-field find/replace panel (Epic 0240 phase 2,
// #283): the cmd+r front end over the one substitute engine. The Find field
// drives the incremental-search preview (live highlight, counter, jump to the
// nearest match), and both finish paths build an ordinary ex substitute —
// ctrl+a runs "%s/find/repl/g" (replace all, "N substitutions" report), enter
// runs "%s/find/repl/gc" and hands over to the existing y/n/a/q/l confirm
// flow, which is exactly replace-current / skip / all with one undo unit.
// Esc closes with nothing mutated, restoring cursor and viewport.

// replacePanel is the open panel's state. The editor stays in normal mode
// underneath; the panel consumes keys ahead of the mode machine (like the
// substitute confirm prompt).
type replacePanel struct {
	find, repl string
	// findCur/replCur are the rune cursors inside the two fields (#2002):
	// both are ordinary single-line inputs edited through ui.EditKey, so
	// each keeps its own position across a tab switch.
	findCur, replCur int
	field            int // 0 = Find, 1 = Replace
	// preselect marks a prefilled Find as selected (#292, mirroring the
	// find-in-path overlay's #277): the first typed rune replaces it
	// wholesale, any other key keeps the text and drops the mark.
	preselect bool
}

// beginReplacePanel opens the panel (editor.replace, cmd+r). The
// committed literal search seeds the Find field, else the panel's last use
// does (#292); Replace always starts from the last use. Cursor and viewport
// are captured so Esc restores them exactly, sharing the search-origin
// plumbing.
func (m *Model) beginReplacePanel() {
	m.replPanel = &replacePanel{find: m.panelFind, repl: m.panelRepl}
	if !m.query.Empty() && !m.query.Regex {
		m.replPanel.find = m.query.Pattern
	}
	m.replPanel.findCur = len([]rune(m.replPanel.find))
	m.replPanel.replCur = len([]rune(m.replPanel.repl))
	m.replPanel.preselect = m.replPanel.find != ""
	m.searchOrigin = m.cursor
	m.searchOrigTop, m.searchOrigLft = m.view.Top, m.view.Left
	m.previewPanelFind()
}

// previewPanelFind recompiles the Find field into the live preview query and
// moves to the nearest match from the origin — the same incsearch behavior
// the "/" line has (#255); no match parks back at the origin.
func (m *Model) previewPanelFind() {
	m.preview = search.Compile(m.parseSearchPattern(m.replPanel.find))
	if !m.preview.Empty() {
		if p, ok := m.preview.Next(m.buf, m.searchOrigin, search.Forward, 1); ok {
			m.cursor = p
			m.desiredCol = p.Col
			m.scroll()
			return
		}
	}
	m.restoreSearchOrigin()
}

// closeReplacePanel dismisses the panel; restore puts cursor and viewport
// back at the captured origin (Esc), while a finishing substitute keeps them.
// The fields are remembered for the next open either way (#292).
func (m *Model) closeReplacePanel(restore bool) {
	m.panelFind, m.panelRepl = m.replPanel.find, m.replPanel.repl
	m.replPanel = nil
	m.preview = search.Query{}
	if restore {
		m.restoreSearchOrigin()
	}
}

// updateReplacePanel consumes one key while the panel is open.
func (m Model) updateReplacePanel(key tea.KeyPressMsg) (Model, tea.Cmd) {
	p := m.replPanel
	// A fresh key clears a lingering panel error ("E: empty pattern") and,
	// #277-style, ends the prefill selection — only a typed rune below still
	// sees it (and replaces the field wholesale).
	m.cmdMsg = ""
	pre := p.preselect
	p.preselect = false
	switch {
	case key.Code == tea.KeyEscape:
		m.closeReplacePanel(true)
	case key.Code == tea.KeyTab:
		p.field = 1 - p.field
	case key.Code == tea.KeyEnter:
		return m.runPanelSubstitute("gc")
	case key.Code == 'a' && key.Mod == tea.ModCtrl:
		return m.runPanelSubstitute("g")
	case key.Code == 'u' && key.Mod == tea.ModCtrl:
		// Clear the field — the panel's own chord, ahead of ui.EditKey,
		// which does not bind ctrl+u.
		*m.panelField(), *m.panelCursor() = "", 0
		if p.field == 0 {
			m.previewPanelFind()
		}
	default:
		if key.Code == 'h' && key.Mod == tea.ModCtrl {
			key = tea.KeyPressMsg{Code: tea.KeyBackspace} // ctrl+h is backspace
		}
		if pre && p.field == 0 && key.Text != "" &&
			key.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModSuper|tea.ModMeta) == 0 {
			// Typing replaces the preselected prefill wholesale (#292); the
			// shared editor below then inserts into the emptied field.
			p.find, p.findCur = "", 0
		}
		f, cur := m.panelField(), m.panelCursor()
		out, ncur, handled, changed := ui.EditKey(key, *f, *cur)
		if !handled {
			break
		}
		*f, *cur = out, ncur
		if changed && p.field == 0 {
			m.previewPanelFind()
		}
	}
	return m, nil
}

// panelField returns the active input field.
func (m *Model) panelField() *string {
	if m.replPanel.field == 0 {
		return &m.replPanel.find
	}
	return &m.replPanel.repl
}

// panelCursor returns the active field's rune cursor.
func (m *Model) panelCursor() *int {
	if m.replPanel.field == 0 {
		return &m.replPanel.findCur
	}
	return &m.replPanel.replCur
}

// runPanelSubstitute closes the panel and runs the whole-file substitute with
// the given flags through the ordinary ex path — one engine, one undo unit,
// the usual "N substitutions on M lines" / confirm-flow behavior.
func (m Model) runPanelSubstitute(flags string) (Model, tea.Cmd) {
	p := m.replPanel
	if p.find == "" {
		m.cmdMsg = "E: empty pattern"
		return m, nil
	}
	line, ok := buildSubLine(p.find, p.repl, flags)
	if !ok {
		m.cmdMsg = "E: no usable delimiter for this pattern"
		return m, nil
	}
	m.closeReplacePanel(false)
	m.cmdline = line
	m.cmdCur = len([]rune(line))
	return m.runExLine()
}

// buildSubLine assembles "%s<d>find<d>repl<d>flags" with a delimiter that
// appears in neither field, so no escaping is ever needed. ok=false when
// every candidate collides (pathological input).
func buildSubLine(find, repl, flags string) (string, bool) {
	for _, d := range "/#|@~!+=" {
		if strings.ContainsRune(find, d) || strings.ContainsRune(repl, d) {
			continue
		}
		s := string(d)
		return "%s" + s + find + s + repl + s + flags, true
	}
	return "", false
}

// replacePanelRows renders the panel's bottom rows: the two labelled fields
// (active one carries the cursor block), a live match tally on the Find row,
// and a key-hint line.
func (m Model) replacePanelRows(width int) []string {
	p := m.replPanel
	if p == nil {
		return nil
	}
	tally := ""
	if p.find != "" {
		if n := len(m.preview.AllMatches(m.buf)); n > 0 {
			tally = "  " + strconv.Itoa(n) + " match" + map[bool]string{true: "", false: "es"}[n == 1]
		} else {
			tally = "  no matches"
		}
	}
	find := "Find     " + m.panelInput(p.find, p.findCur, p.field == 0) + tally
	repl := "Replace  " + m.panelInput(p.repl, p.replCur, p.field == 1)
	hint := "[enter] confirm each · [ctrl+a] replace all · [tab] switch field · [esc] cancel"
	if m.cmdMsg != "" {
		// Panel errors ("E: empty pattern") render where the ex line would
		// (#292) — the hint row is the panel's message surface.
		hint = m.cmdMsg
	}
	return []string{truncRow(find, width), truncRow(repl, width), truncRow(hint, width)}
}

// panelInput renders one field's text, the active one with a cursor block.
// A preselected Find prefill renders inverted so it reads as replace-on-type
// (#292).
func (m Model) panelInput(text string, cur int, active bool) string {
	if m.replPanel.preselect && active && m.replPanel.field == 0 && text != "" {
		return lipgloss.NewStyle().Reverse(true).Render(text) + "▏"
	}
	if active {
		// The cursor sits inside the text (#2002), so it has to be visible
		// wherever it is — end-of-text still shows the trailing block.
		if cur >= len([]rune(text)) {
			return text + "▏"
		}
		return ui.CursorView(text, cur)
	}
	return text
}

// truncRow clamps a panel row to the pane width.
func truncRow(s string, width int) string {
	if width <= 0 {
		return s
	}
	if r := []rune(s); len(r) > width {
		return string(r[:width])
	}
	return s
}
