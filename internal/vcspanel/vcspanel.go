// Package vcspanel is the persistent VCS tool window (Roadmap 0330, #480):
// a bottom-split pane with two tabs — Changes (staging list + commit) and
// Log (commit history) — JetBrains' Commit/Git tool windows scaled to the
// terminal. The panel reads the shared vcs.Snapshot threaded in by the root
// model and never runs git itself; every git interaction stays an async
// command in internal/vcs.
package vcspanel

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/theme"
	"ike/internal/vcs"
)

// Tab selects the visible view.
type Tab int

const (
	TabChanges Tab = iota
	TabLog
)

// Model is the tool window state. Value type with pointer-receiver mutators,
// embedded in a pane.Instance like the diff viewer.
type Model struct {
	width   int
	height  int
	focused bool
	pal     *theme.Palette
	tab     Tab

	snap *vcs.Snapshot // shared status snapshot; nil = not a git repo

	// Changes view (#483): the staging rows plus the message draft shared
	// with the modal commit dialog.
	chRows   []Row
	chCursor int
	chTop    int
	msgFocus bool
	draft    *vcs.MessageDraft

	// Log view (#484): windowed history, flattened with the expanded
	// commit's files; loaded lazily through the root model.
	logEntries   []vcs.LogEntry
	logRows      []logRow
	logCursor    int
	logTop       int
	logHasMore   bool
	logLoading   bool
	logErr       string
	expandedHash string
	details      *vcs.ShowMsg
}

// New returns a closed-over-nothing panel showing the Changes tab.
func New(pal *theme.Palette) Model {
	return Model{pal: pal, draft: &vcs.MessageDraft{}}
}

// SetDraft swaps in the shared commit message draft (#483) so the panel and
// the modal dialog edit the same text.
func (m *Model) SetDraft(d *vcs.MessageDraft) {
	if d != nil {
		m.draft = d
	}
}

// SetSize records the interior content size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// SetFocused marks the panel focused (header highlight).
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetPalette re-threads the active theme.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// SetVCS threads the current status snapshot in; the root model calls it on
// every refresh, mirroring the explorer. The Changes rows re-derive from it.
func (m *Model) SetVCS(snap *vcs.Snapshot) {
	m.snap = snap
	m.rebuildChanges()
}

// ActiveTab reports the visible view (tests, persistence).
func (m *Model) ActiveTab() Tab { return m.tab }

// SetTab selects the visible view without side effects — the layout restore
// uses it (#504); the log's first load rides EnsureLogLoaded once the
// snapshot exists.
func (m *Model) SetTab(t Tab) {
	if t == TabChanges || t == TabLog {
		m.tab = t
	}
}

// EnsureLogLoaded requests the log's first window when the Log tab is
// visible and empty (#504): a restored Log view loads as soon as the initial
// status snapshot arrives instead of waiting for a key-driven tab entry.
func (m *Model) EnsureLogLoaded() tea.Cmd {
	if m.tab != TabLog {
		return nil
	}
	return m.ensureLogLoaded()
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
		return m.handleKey(msg)
	}
	return nil
}

// handleKey drives the tab header; view-specific keys land in the active
// view's handler. While the message field holds focus every key belongs to
// it — the tab-switch digits must stay typeable.
func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.tab == TabChanges && m.msgFocus {
		return m.updateChanges(msg)
	}
	switch msg.String() {
	case "1":
		m.tab = TabChanges
		return nil
	case "2":
		m.tab = TabLog
		return m.ensureLogLoaded()
	case "tab":
		m.tab = (m.tab + 1) % 2
		if m.tab == TabLog {
			return m.ensureLogLoaded()
		}
		return nil
	}
	switch m.tab {
	case TabChanges:
		return m.updateChanges(msg)
	default:
		return m.updateLog(msg)
	}
}

// View renders the tab header plus the active view's body.
func (m *Model) View() string {
	body := m.viewBody()
	return m.header() + "\n" + body
}

// viewBody picks the active view, degrading to the non-repo placeholder.
func (m *Model) viewBody() string {
	if m.snap == nil {
		return lipgloss.NewStyle().Faint(true).Render("not a git repository")
	}
	if m.tab == TabChanges {
		return m.viewChanges()
	}
	return m.viewLog()
}

// header renders the two tab labels, the active one accented.
func (m *Model) header() string {
	pal := m.theme()
	labels := []string{"1 Changes", "2 Log"}
	var parts []string
	for i, l := range labels {
		s := lipgloss.NewStyle().Foreground(pal.Secondary)
		if Tab(i) == m.tab {
			s = lipgloss.NewStyle().Foreground(pal.Accent).Bold(true)
			if m.focused {
				s = s.Underline(true)
			}
		}
		parts = append(parts, s.Render(l))
	}
	line := " " + strings.Join(parts, "  │  ")
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
