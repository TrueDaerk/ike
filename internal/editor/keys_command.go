package editor

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/excmd"
	"ike/internal/editor/search"
)

// beginSearch enters the command line in search mode for "/" or "?", capturing
// the cursor and viewport so an Esc restores them exactly (#255).
func (m *Model) beginSearch(dir search.Direction) {
	m.mode = Command
	m.searching = true
	m.searchDir = dir
	m.cmdline = ""
	m.preview = search.Query{}
	m.searchOrigin = m.cursor
	m.searchOrigTop, m.searchOrigLft = m.view.Top, m.view.Left
}

// searchNextRepeat repeats the active search for n/N. reverse flips the stored
// direction (N). Wrapping past a buffer end leaves a "search wrapped" hint on
// the ex line (#255).
func (m *Model) searchNextRepeat(reverse bool, count int) {
	if m.query.Empty() {
		return
	}
	dir := m.searchDir
	if reverse {
		dir = opposite(dir)
	}
	if p, ok := m.query.Next(m.buf, m.cursor, dir, count); ok {
		if wrapped(m.cursor, p, dir) {
			m.cmdMsg = "search wrapped"
		}
		m.hlActive = true
		m.jumpTo(p) // n/N landings are jumps (Roadmap 0220)
	}
}

// wrapped reports whether a search landing at p from `from` crossed a buffer
// end: a forward match behind the cursor (or on it) wrapped to the top, a
// backward match ahead of it wrapped to the bottom.
func wrapped(from, p buffer.Position, dir search.Direction) bool {
	if dir == search.Forward {
		return !from.Before(p)
	}
	return !p.Before(from)
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
		if m.searching {
			m.cancelSearch()
		}
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
			if m.searching {
				m.searchPreview()
			}
		} else {
			if m.searching {
				m.cancelSearch()
			}
			m.mode = Normal
			m.searching = false
		}
	case key.Text != "" && key.Mod&(tea.ModCtrl|tea.ModAlt) == 0:
		// Printable input, including a bare space (Text == " ").
		m.cmdline += key.Text
		if m.searching {
			m.searchPreview()
		}
	}
	return m, nil
}

// parseSearchPattern splits the typed line into pattern and regex flag: a "\v"
// prefix enables regex (very-magic toggle); otherwise the search is literal.
func parseSearchPattern(line string) (string, bool) {
	if strings.HasPrefix(line, `\v`) {
		return line[2:], true
	}
	return line, false
}

// searchPreview recompiles the half-typed pattern and moves to the nearest
// match from the search origin, vim's incsearch (#255). No match (or an empty
// pattern) parks the cursor back at the origin; nothing lands on the nav
// stack — only the committed jump does.
func (m *Model) searchPreview() {
	m.preview = search.Compile(parseSearchPattern(m.cmdline))
	if !m.preview.Empty() {
		if p, ok := m.preview.Next(m.buf, m.searchOrigin, m.searchDir, 1); ok {
			m.cursor = p
			m.desiredCol = p.Col
			m.scroll()
			return
		}
	}
	m.restoreSearchOrigin()
}

// cancelSearch abandons the search line: cursor and viewport return exactly to
// where they were when the search opened; the previously committed query (and
// its n/N state) is untouched.
func (m *Model) cancelSearch() {
	m.preview = search.Query{}
	m.restoreSearchOrigin()
}

// restoreSearchOrigin puts cursor and viewport back at their captured state.
func (m *Model) restoreSearchOrigin() {
	m.cursor = m.buf.ClampCursor(m.searchOrigin)
	m.desiredCol = m.cursor.Col
	m.view.Top, m.view.Left = m.searchOrigTop, m.searchOrigLft
	m.scroll()
}

// commitSearch installs the previewed pattern as the active query and jumps to
// the first match from the origin. Zero matches leave a "no matches" report on
// the ex line and restore the origin.
func (m *Model) commitSearch() {
	m.preview = search.Compile(parseSearchPattern(m.cmdline))
	m.query = m.preview
	m.preview = search.Query{}
	if m.query.Empty() {
		m.restoreSearchOrigin()
		return
	}
	p, ok := m.query.Next(m.buf, m.searchOrigin, m.searchDir, 1)
	if !ok {
		m.hlActive = false
		m.cmdMsg = "no matches: " + m.query.Pattern
		m.restoreSearchOrigin()
		return
	}
	m.hlActive = true
	if wrapped(m.searchOrigin, p, m.searchDir) {
		m.cmdMsg = "search wrapped"
	}
	m.cursor = m.searchOrigin // the jump departs from the origin, not the preview
	m.jumpTo(p)               // the initial /-search landing is a jump (Roadmap 0220)
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
		return m, func() tea.Msg { return CloseMsg{Force: cmd.Bang} }
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
	case "d", "delete":
		return m.exDelete(cmd), nil
	case "y", "yank":
		return m.exYank(cmd), nil
	default:
		switch {
		case isRun(cmd.Name, '>'):
			return m.exIndent(cmd, len(cmd.Name)), nil
		case isRun(cmd.Name, '<'):
			return m.exIndent(cmd, -len(cmd.Name)), nil
		}
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
