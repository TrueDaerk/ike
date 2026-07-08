package editor

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/excmd"
	"ike/internal/editor/search"
)

// beginSearch enters the command line in search mode for "/" or "?".
func (m *Model) beginSearch(dir search.Direction) {
	m.mode = Command
	m.searching = true
	m.searchDir = dir
	m.cmdline = ""
}

// searchNextRepeat repeats the active search for n/N. reverse flips the stored
// direction (N).
func (m *Model) searchNextRepeat(reverse bool, count int) {
	if m.query.Empty() {
		return
	}
	dir := m.searchDir
	if reverse {
		dir = opposite(dir)
	}
	if p, ok := m.query.Next(m.buf, m.cursor, dir, count); ok {
		m.moveTo(p)
	}
}

func opposite(d search.Direction) search.Direction {
	if d == search.Forward {
		return search.Backward
	}
	return search.Forward
}

// updateCommandLine handles typing on the ":" / "/" / "?" line.
func (m Model) updateCommandLine(key tea.KeyPressMsg) (Model, tea.Cmd) {
	switch {
	case key.Code == tea.KeyEscape:
		m.mode = Normal
		m.cmdline = ""
		m.searching = false
	case key.Code == tea.KeyEnter:
		if m.searching {
			m.commitSearch()
			m.mode = Normal
			m.searching = false
			m.cmdline = ""
			return m, nil
		}
		return m.runExLine()
	case key.Code == tea.KeyBackspace, key.Code == 'h' && key.Mod == tea.ModCtrl:
		if r := []rune(m.cmdline); len(r) > 0 {
			m.cmdline = string(r[:len(r)-1])
		} else {
			m.mode = Normal
			m.searching = false
		}
	case key.Text != "" && key.Mod&(tea.ModCtrl|tea.ModAlt) == 0:
		// Printable input, including a bare space (Text == " ").
		m.cmdline += key.Text
	}
	return m, nil
}

// commitSearch compiles the typed pattern and jumps to the first match. A "\v"
// prefix enables regex (very-magic toggle); otherwise the search is literal.
func (m *Model) commitSearch() {
	pattern, regex := m.cmdline, false
	if strings.HasPrefix(pattern, `\v`) {
		pattern, regex = pattern[2:], true
	}
	m.query = search.Compile(pattern, regex)
	if m.query.Empty() {
		return
	}
	if p, ok := m.query.Next(m.buf, m.cursor, m.searchDir, 1); ok {
		m.moveTo(p)
	}
}

// runExLine parses and executes a ":" command, returning any resulting tea.Cmd.
func (m Model) runExLine() (Model, tea.Cmd) {
	cmd := excmd.Parse(m.cmdline)
	m.mode = Normal
	m.cmdline = ""
	switch cmd.Kind {
	case excmd.Write:
		if c := m.saveGuarded(orDefault(cmd.Arg, m.path)); c != nil {
			return m, c
		}
	case excmd.Quit:
		return m, func() tea.Msg { return CloseMsg{} }
	case excmd.WriteQuit:
		if c := m.saveGuarded(orDefault(cmd.Arg, m.path)); c != nil {
			return m, c // conflict: prompt first, keep the pane open
		}
		return m, func() tea.Msg { return CloseMsg{} }
	case excmd.Edit:
		if cmd.Arg != "" {
			_ = m.Load(cmd.Arg)
		}
	case excmd.Goto:
		line := cmd.Line - 1
		if line > m.buf.LineCount()-1 {
			line = m.buf.LineCount() - 1
		}
		m.moveTo(buffer.Position{Line: line, Col: 0})
	}
	return m, nil
}

// orDefault returns s when non-empty, else def.
func orDefault(s, def string) string {
	if s != "" {
		return s
	}
	return def
}
