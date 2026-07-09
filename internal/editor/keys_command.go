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
	m.cmdMsg = ""
	if cmd.Err != "" {
		m.cmdMsg = "E: " + cmd.Err
		return m, nil
	}

	// A bare range with no command name jumps to the last line of the range.
	if cmd.Name == "" {
		if cmd.Range.Count == 0 {
			return m, nil
		}
		_, end, err := cmd.Range.Resolve(m.exResolver(), m.cursor.Line)
		if err != "" {
			m.cmdMsg = "E: " + err
			return m, nil
		}
		m.moveTo(buffer.Position{Line: end, Col: 0})
		return m, nil
	}

	switch cmd.Name {
	case "w", "write":
		if c := m.saveGuarded(orDefault(cmd.Args, m.path)); c != nil {
			return m, c
		}
	case "q", "quit":
		return m, func() tea.Msg { return CloseMsg{} }
	case "wq", "x", "xit":
		if c := m.saveGuarded(orDefault(cmd.Args, m.path)); c != nil {
			return m, c // conflict: prompt first, keep the pane open
		}
		return m, func() tea.Msg { return CloseMsg{} }
	case "e", "edit":
		if cmd.Args != "" {
			_ = m.Load(cmd.Args)
		}
	case "g", "global", "v", "vglobal":
		m.cmdMsg = "E: :" + cmd.Name + " is not implemented yet"
	case "s", "substitute":
		return m.substitute(cmd), nil
	default:
		m.cmdMsg = "E: not an editor command: " + cmd.Name
	}
	return m, nil
}

// exResolver captures the editor state the ex range resolver consults: the
// cursor line, the last visual selection bounds, and a line-search hook for
// pattern addresses.
func (m Model) exResolver() excmd.Resolver {
	return excmd.Resolver{
		Buf:         m.buf,
		Current:     m.cursor.Line,
		VisualStart: m.visualStart,
		VisualEnd:   m.visualEnd,
		Search:      m.exSearchLine,
	}
}

// exSearchLine finds the next line (0-based) matching pat as a regex, searching
// from the line after/before `from` and wrapping around the buffer ends. It
// backs the "/pat/" and "?pat?" ex addresses.
func (m Model) exSearchLine(pat string, from int, forward bool) (int, bool) {
	q := search.Compile(pat, true)
	if q.Empty() {
		return 0, false
	}
	n := m.buf.LineCount()
	for i := 1; i <= n; i++ {
		var l int
		if forward {
			l = (from + i) % n
		} else {
			l = ((from-i)%n + n) % n
		}
		if len(q.LineMatches(m.buf, l)) > 0 {
			return l, true
		}
	}
	return 0, false
}

// orDefault returns s when non-empty, else def.
func orDefault(s, def string) string {
	if s != "" {
		return s
	}
	return def
}
