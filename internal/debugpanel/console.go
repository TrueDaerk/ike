package debugpanel

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/terminal"
)

// The combined debug area (#2190): the panel hosts the debuggee's console —
// the same terminal.Model the run/terminal path renders — behind an internal
// tab bar (Variables │ Console), so the session's whole surface moves,
// resizes and persists as one pane. The terminal model lives here whole, so
// its scrollback, search and selection survive every view switch.

// Tab identifies the panel's active view.
type Tab int

const (
	// TabVariables is the frames/variables/watches view.
	TabVariables Tab = iota
	// TabConsole is the debuggee's output terminal.
	TabConsole
)

// SetTerm installs the debuggee's console terminal, closing any previous
// session's model (a new launch replaces the pipe, runInTerminal swaps in a
// PTY). The tab bar appears with the first terminal, so the body loses one
// row; nil removes the console again.
func (m *Model) SetTerm(t *terminal.Model) {
	if m.term != nil && m.term != t {
		m.term.Close()
	}
	m.term = t
	m.applySize()
	if m.term != nil {
		m.term.SetPalette(m.pal)
		m.term.SetFocused(m.focused && m.tab == TabConsole)
	}
}

// Term returns the embedded console terminal, nil while none is installed.
func (m *Model) Term() *terminal.Model { return m.term }

// HasTerm reports whether a console terminal is installed.
func (m Model) HasTerm() bool { return m.term != nil }

// CloseTerm ends the console terminal's session (pane close/teardown).
func (m *Model) CloseTerm() {
	if m.term != nil {
		m.term.Close()
		m.term = nil
	}
}

// ActiveTab reports the visible view.
func (m Model) ActiveTab() Tab { return m.tab }

// ConsoleActive reports whether the console terminal is the visible view —
// the gate the app's key/mouse routing switches on.
func (m Model) ConsoleActive() bool { return m.term != nil && m.tab == TabConsole }

// SetTab switches the view directly (keys, tab-bar clicks). A user switch is
// remembered so the automatic view selection stops overriding it.
func (m *Model) SetTab(t Tab) {
	m.tabTouched = true
	m.setTab(t)
}

// AutoTab switches the view only while the user has not picked one this
// session: output before the first stop surfaces the console, the first stop
// surfaces the variables.
func (m *Model) AutoTab(t Tab) {
	if m.tabTouched {
		return
	}
	m.setTab(t)
}

// setTab applies a view switch, re-threading focus into the terminal; the
// terminal model itself is untouched, so scrollback, search and selection
// survive (#2190).
func (m *Model) setTab(t Tab) {
	if t == TabConsole && m.term == nil {
		return
	}
	m.tab = t
	if m.term != nil {
		m.term.SetFocused(m.focused && m.tab == TabConsole)
	}
}

// tabBarRows is the tab bar's height: one row once a console exists.
func (m Model) tabBarRows() int {
	if m.term != nil {
		return 1
	}
	return 0
}

// applySize distributes the recorded outer size: the body keeps everything
// under the tab bar, the terminal gets the same interior.
func (m *Model) applySize() {
	m.h = m.outerH - m.tabBarRows()
	if m.h < 0 {
		m.h = 0
	}
	if m.term != nil && m.w > 0 && m.h > 0 {
		m.term.SetSize(m.w, m.h)
	}
}

// tabLabels are the bar's two entries.
func tabLabels() []string { return []string{"Variables", "Console"} }

// tabBar renders the panel's first row, mirroring the issues panel's in-pane
// bar (#2090): the active view accented, the other faint.
func (m Model) tabBar() string {
	pal := m.theme()
	active := lipgloss.NewStyle().Foreground(pal.Accent).Bold(true)
	idle := lipgloss.NewStyle().Foreground(pal.Foreground).Faint(true)
	frame := lipgloss.NewStyle().Foreground(pal.Border)
	var b strings.Builder
	for i, label := range tabLabels() {
		if i > 0 {
			b.WriteString(frame.Render("│"))
		}
		style := idle
		if Tab(i) == m.tab {
			style = active
		}
		b.WriteString(style.Render(" " + label + " "))
	}
	return b.String()
}

// tabBarSpans returns each label's [start, end) column range for click
// resolution.
func tabBarSpans() [][2]int {
	spans := make([][2]int, 0, 2)
	x := 0
	for i, label := range tabLabels() {
		if i > 0 {
			x++ // the │ separator
		}
		w := len(label) + 2 // the label's padding spaces
		spans = append(spans, [2]int{x, x + w})
		x += w
	}
	return spans
}

// TabBarClick resolves a click on the bar row to the view whose label spans x.
func (m *Model) TabBarClick(x int) {
	for i, span := range tabBarSpans() {
		if x >= span[0] && x < span[1] {
			m.SetTab(Tab(i))
			return
		}
	}
}

// consoleKey routes a key while the console view owns the panel. On a pipe
// console (no process reading input) tab/shift+tab return to the variables
// view; everything else — and every key of a PTY debuggee — goes to the
// terminal.
func (m *Model) consoleKey(k tea.KeyPressMsg) tea.Cmd {
	if m.term.IsPipe() {
		switch k.String() {
		case "tab", "shift+tab":
			m.SetTab(TabVariables)
			return nil
		}
	}
	return m.term.Update(k)
}
