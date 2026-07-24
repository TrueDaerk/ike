// Package vcspanel is the persistent VCS tool window (Roadmap 0330, #480;
// slimmed in #750): a bottom-split pane with a read-only list of changed
// files. Activating a row opens the file's diff against HEAD. The panel reads
// the shared vcs.Snapshot threaded in by the root model and never runs git
// itself. Git *workflow* (staging, commits, branches, log) is delegated to
// custom TUI tool panes (#741) — lazygit ships as the preconfigured example.
package vcspanel

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/theme"
	"ike/internal/vcs"
)

// Model is the tool window state. Value type with pointer-receiver mutators,
// embedded in a pane.Instance like the diff viewer.
type Model struct {
	width   int
	height  int
	focused bool
	pal     *theme.Palette

	snap *vcs.Snapshot // shared status snapshot; nil = not a git repo

	// Changes list (#483, slimmed in #750): the changed files.
	chRows   []Row
	chCursor int
	chTop    int

	// Double-click detection (#514): activating a row (diff) needs a second
	// click on the same row within doubleClickWindow; now is injectable so
	// tests control the clock.
	lastClickRow int
	lastClickAt  time.Time
	now          func() time.Time
}

// New returns a closed-over-nothing panel.
func New(pal *theme.Palette) Model {
	return Model{pal: pal, lastClickRow: -1, now: time.Now}
}

// SetSize records the interior content size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// SetFocused marks the panel focused (header highlight).
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-threads the active theme.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// SetVCS threads the current status snapshot in; the root model calls it on
// every refresh, mirroring the explorer. The rows re-derive from it.
func (m *Model) SetVCS(snap *vcs.Snapshot) {
	m.snap = snap
	m.rebuildChanges()
}

// theme resolves the palette with the shared default fallback.
func (m *Model) theme() *theme.Palette {
	if m.pal != nil {
		return m.pal
	}
	return theme.DefaultPalette()
}

// Update handles one message while the panel exists; only key presses reach
// it unfocused-filtered by the pane layer.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.updateChanges(msg)
	}
	return nil
}

// View renders the header plus the changes list.
func (m *Model) View() string {
	body := m.viewBody()
	return m.header() + "\n" + body
}

// viewBody renders the list, degrading to the non-repo placeholder.
func (m *Model) viewBody() string {
	if m.snap == nil {
		return lipgloss.NewStyle().Faint(true).Render("not a git repository")
	}
	return m.viewChanges()
}

// header renders the panel caption plus the branch.
func (m *Model) header() string {
	pal := m.theme()
	s := lipgloss.NewStyle().Foreground(pal.Accent).Bold(true)
	if m.focused {
		s = s.Underline(true)
	}
	line := " " + s.Render("Changes")
	if m.snap != nil && m.snap.Branch != "" {
		branch := lipgloss.NewStyle().Faint(true).Render("⎇ " + m.snap.Branch)
		line += "   " + branch
	}
	return line
}

// bodyHeight is the room below the header line.
func (m *Model) bodyHeight() int {
	h := m.height - 1
	if h < 1 {
		h = 1
	}
	return h
}

// clip bounds one rendered line to the panel width.
func (m *Model) clip(s string) string {
	if m.width > 0 && len([]rune(s)) > m.width {
		return string([]rune(s)[:m.width-1]) + "…"
	}
	return s
}
