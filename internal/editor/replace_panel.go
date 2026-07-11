package editor

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/search"
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
	field      int // 0 = Find, 1 = Replace
}

// beginReplacePanel opens the panel (editor.replace, cmd+r / leader R). The
// committed literal search seeds the Find field; cursor and viewport are
// captured so Esc restores them exactly, sharing the search-origin plumbing.
func (m *Model) beginReplacePanel() {
	m.replPanel = &replacePanel{}
	if !m.query.Empty() && !m.query.Regex {
		m.replPanel.find = m.query.Pattern
	}
	m.searchOrigin = m.cursor
	m.searchOrigTop, m.searchOrigLft = m.view.Top, m.view.Left
	m.previewPanelFind()
}

// previewPanelFind recompiles the Find field into the live preview query and
// moves to the nearest match from the origin — the same incsearch behavior
// the "/" line has (#255); no match parks back at the origin.
func (m *Model) previewPanelFind() {
	m.preview = search.Compile(parseSearchPattern(m.replPanel.find))
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
func (m *Model) closeReplacePanel(restore bool) {
	m.replPanel = nil
	m.preview = search.Query{}
	if restore {
		m.restoreSearchOrigin()
	}
}

// updateReplacePanel consumes one key while the panel is open.
func (m Model) updateReplacePanel(key tea.KeyPressMsg) (Model, tea.Cmd) {
	p := m.replPanel
	switch {
	case key.Code == tea.KeyEscape:
		m.closeReplacePanel(true)
	case key.Code == tea.KeyTab:
		p.field = 1 - p.field
	case key.Code == tea.KeyEnter:
		return m.runPanelSubstitute("gc")
	case key.Code == 'a' && key.Mod == tea.ModCtrl:
		return m.runPanelSubstitute("g")
	case key.Code == tea.KeyBackspace, key.Code == 'h' && key.Mod == tea.ModCtrl:
		f := m.panelField()
		if r := []rune(*f); len(r) > 0 {
			*f = string(r[:len(r)-1])
			if p.field == 0 {
				m.previewPanelFind()
			}
		}
	case key.Code == 'u' && key.Mod == tea.ModCtrl:
		*m.panelField() = ""
		if p.field == 0 {
			m.previewPanelFind()
		}
	case key.Text != "" && key.Mod&(tea.ModCtrl|tea.ModAlt) == 0:
		*m.panelField() += key.Text
		if p.field == 0 {
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
	find := "Find     " + m.panelInput(p.find, p.field == 0) + tally
	repl := "Replace  " + m.panelInput(p.repl, p.field == 1)
	hint := "[enter] confirm each · [ctrl+a] replace all · [tab] switch field · [esc] cancel"
	return []string{truncRow(find, width), truncRow(repl, width), truncRow(hint, width)}
}

// panelInput renders one field's text, the active one with a cursor block.
func (m Model) panelInput(text string, active bool) string {
	if active {
		return text + "▏"
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
