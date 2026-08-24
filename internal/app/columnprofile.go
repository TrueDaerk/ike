package app

// columnprofile.go is the root model's half of the column profile (#1940).
//
// Two surfaces show the same profile, because the same question — "is this
// column ever null? what values does it take?" — is asked of two kinds of
// table:
//
//   - **The data viewer** (`data.columnProfile`, or `P` in the grid) profiles
//     the focused grid column through the backend's SQL aggregates or its
//     bounded scan. The popup lives inside the pane; this file only routes
//     the command and the pane's copy request.
//   - **A csv/tsv/psv buffer** (`csv.columnProfile`) profiles the caret's
//     column of the table-rendered buffer (#1589). A text buffer has no query
//     engine, so this is the bounded scan of internal/datasrc — capped at
//     datasrc.ProfileLimit rows, and the popup says so when it caps. It runs
//     as a background command like every other scan: a million-line csv must
//     not stall a keystroke.

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"ike/internal/datasrc"
	"ike/internal/dataview"
	"ike/internal/editor/register"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/ui"
)

// DataColumnProfileMsg runs data.columnProfile against the focused data pane.
type DataColumnProfileMsg struct{}

// CSVColumnProfileMsg runs csv.columnProfile against the focused editor's
// table-rendered buffer.
type CSVColumnProfileMsg struct{}

// csvProfileMsg carries one finished csv profile back to the root model.
type csvProfileMsg struct {
	profile datasrc.Profile
	err     error
}

// profileColumn forwards the palette command to the focused data viewer,
// which owns the popup and the cancel path. A pane that is not a data viewer
// (the command is pane-scoped, but a stale palette entry can still fire) is
// simply not profiled.
func (m *Model) profileColumn() tea.Cmd {
	inst := m.focusedContent() // the viewer may live in a tab (#1778)
	if inst == nil || inst.Kind() != pane.KindData {
		return nil
	}
	return inst.Update(dataview.ProfileMsg{})
}

// profileCSVColumn profiles the caret's column of the focused csv/tsv/psv
// buffer. The scan is a background command: the profile of a large file is
// bounded but not free.
func (m *Model) profileCSVColumn() tea.Cmd {
	ed := m.activeEditor()
	if ed == nil {
		return nil
	}
	target, ok := ed.SVProfileTarget()
	if !ok {
		m.host.Notify(host.Warn, "column profile: not a table-rendered csv buffer")
		return nil
	}
	table := filepath.Base(ed.Path())
	if table == "" || table == "." {
		table = "buffer"
	}
	return func() tea.Msg {
		p := datasrc.ProfileCSV(table, target.Name, target.Lines, target.Sep, target.Index, target.Header)
		return csvProfileMsg{profile: p}
	}
}

// showCSVProfile puts a finished csv profile in the floating shell: esc
// closes it, y copies exactly the lines it shows.
func (m *Model) showCSVProfile(msg csvProfileMsg) tea.Cmd {
	if msg.err != nil {
		m.host.Notify(host.Warn, "column profile: "+msg.err.Error())
		return nil
	}
	m.csvProfile = &csvProfileContent{profile: msg.profile, regs: m.regs}
	m.shell.SetContent(m.csvProfile)
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
	return nil
}

// csvProfileContent is the shell content of a csv column profile. It is a
// pointer so its copy key can record the confirmation the body shows.
type csvProfileContent struct {
	profile datasrc.Profile
	copied  bool
	// regs is the app-wide register store, so the copy key can record the
	// profile in the clipboard history (#2061) without a Model reference.
	regs *register.Store
}

// Title implements ui.Content.
func (c *csvProfileContent) Title() string { return "Column Profile — " + c.profile.Column }

// Render implements ui.Content: the shared profile rendering plus the key
// hint, so the popup and the clipboard can never show different numbers.
func (c *csvProfileContent) Render(int) string {
	lines := c.profile.Lines()
	hint := "esc close · y copy"
	if c.copied {
		hint = "esc close · copied"
	}
	return joinV(append(lines, "", hint)...)
}

// HandleKey implements ui.KeyHandler: y copies the profile.
func (c *csvProfileContent) HandleKey(key string) bool {
	if key != "y" && key != "c" {
		return false
	}
	text := c.profile.Text()
	clipboardWrite(text)
	recordClipboardHistory(c.regs, text)
	c.copied = true
	return true
}

// csvProfileOpen reports whether the shell currently shows a csv profile.
func (m Model) csvProfileOpen() bool {
	return m.csvProfile != nil && m.shell.IsOpen() && m.shell.Content() == ui.Content(m.csvProfile)
}
