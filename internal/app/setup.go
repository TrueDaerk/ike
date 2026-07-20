package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/lang"
	"ike/internal/theme"
	"ike/internal/ui"
)

// setup.go is the post-tour setup flow (#713): finishing the Welcome Tour
// chains a queue of one-shot setup dialogs through the floating shell —
// theme picker, LSP server picker (the onboarding dialog, force-opened
// regardless of lsp.onboarded), a read-only toolchain summary, and the
// tool-pane setup dialog (#751–#753, tools_setup.go). Escaping
// the tour mid-way skips the flow; the first-run LSP onboarding then still
// queues behind it as before (#658).

// setupSteps is the flow order.
var setupSteps = []string{"theme", "lsp", "toolchain", "tools"}

// themePickState is the open theme-picker dialog: the registered theme
// names, the cursor, and the theme active when the dialog opened (restored
// on esc).
type themePickState struct {
	names  []string
	cursor int
	orig   string
}

// toolchainInfoState is the open toolchain summary: one row per language
// with a toolchain capability.
type toolchainInfoState struct {
	rows []toolchainInfoRow
}

type toolchainInfoRow struct {
	id     string
	path   string
	source string
}

// startSetupFlow begins the post-tour setup chain. The first-run LSP pending
// flag is cleared — the flow's own forced dialog replaces it, so it cannot
// pop a second time afterwards.
func (m *Model) startSetupFlow() {
	m.setupQueue = append([]string(nil), setupSteps...)
	m.onboardingPending = false
	m.advanceSetup()
}

// advanceSetup opens the next queued step, skipping steps with nothing to
// show. A drained queue ends the flow.
func (m *Model) advanceSetup() {
	for len(m.setupQueue) > 0 {
		step := m.setupQueue[0]
		m.setupQueue = m.setupQueue[1:]
		switch step {
		case "theme":
			if m.openThemePick() {
				return
			}
		case "lsp":
			if m.openOnboardingDialog() {
				return
			}
		case "toolchain":
			if m.openToolchainInfo() {
				return
			}
		case "tools":
			if m.openToolSetup() {
				return
			}
		}
	}
}

// --- theme picker ---

// openThemePick shows the theme chooser; false when no theme is registered.
func (m *Model) openThemePick() bool {
	names := themeNames(m.reg)
	if len(names) == 0 {
		return false
	}
	current := theme.DefaultName
	if c := config.Get(); c != nil && c.Theme.Name != "" {
		current = c.Theme.Name
	}
	cursor := 0
	for i, n := range names {
		if n == current {
			cursor = i
		}
	}
	m.themePick = &themePickState{names: names, cursor: cursor, orig: current}
	m.shell.SetContent(ui.ModelContent{Heading: "Choose a theme", Body: m.themePickBody})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
	return true
}

// themePickOpen reports whether the theme picker is showing.
func (m Model) themePickOpen() bool { return m.themePick != nil && m.shell.IsOpen() }

// themePickBody renders the theme list with the cursor.
func (m Model) themePickBody() string {
	tp := m.themePick
	var b strings.Builder
	b.WriteString("Pick a color scheme — moving previews it live (also later in\n")
	b.WriteString("Settings → Appearance).\n\n")
	for i, n := range tp.names {
		marker := "  "
		if i == tp.cursor {
			marker = "▸ "
		}
		mark := ""
		if n == tp.orig {
			mark = "  (current)"
		}
		b.WriteString(marker + n + mark + "\n")
	}
	b.WriteString("\n  [j/k] move   [enter] keep   [esc] keep the previous theme")
	return b.String()
}

// previewTheme applies a theme by name without persisting it.
func (m *Model) previewTheme(name string) {
	sel, _ := theme.Select(name, m.reg.Themes())
	m.applyTheme(theme.NewPalette(sel))
}

// updateThemePick handles a key while the theme picker is open. Moving the
// cursor previews the highlighted theme immediately; enter persists it (the
// same user-scope theme.name write as Settings → Appearance), esc restores
// the theme active before the dialog. Both continue the setup flow.
func (m Model) updateThemePick(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	tp := m.themePick
	switch msg.String() {
	case "j", "down":
		if tp.cursor < len(tp.names)-1 {
			tp.cursor++
			m.previewTheme(tp.names[tp.cursor])
		}
	case "k", "up":
		if tp.cursor > 0 {
			tp.cursor--
			m.previewTheme(tp.names[tp.cursor])
		}
	case "enter":
		cmd := m.selectTheme(tp.names[tp.cursor])
		m.themePick = nil
		m.shell.Close()
		m.advanceSetup()
		return m, cmd
	case "esc":
		m.previewTheme(tp.orig)
		m.themePick = nil
		m.shell.Close()
		m.advanceSetup()
	}
	return m, nil
}

// --- toolchain summary ---

// openToolchainInfo shows the read-only toolchain check; false when no
// registered language carries a toolchain capability.
func (m *Model) openToolchainInfo() bool {
	var rows []toolchainInfoRow
	for _, l := range lang.All() {
		if l.Toolchain == nil {
			continue
		}
		explicit := ""
		if c := config.Get(); c != nil {
			explicit = c.Lang[l.ID]["interpreter"]
		}
		path, source := lang.Interpreter(l.ID, ".", explicit)
		rows = append(rows, toolchainInfoRow{id: l.ID, path: path, source: source})
	}
	if len(rows) == 0 {
		return false
	}
	m.toolchainInfo = &toolchainInfoState{rows: rows}
	m.shell.SetContent(ui.ModelContent{Heading: "Toolchain check", Body: m.toolchainInfoBody})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
	return true
}

// toolchainInfoOpen reports whether the toolchain summary is showing.
func (m Model) toolchainInfoOpen() bool { return m.toolchainInfo != nil && m.shell.IsOpen() }

// toolchainInfoBody renders the per-language interpreter resolution.
func (m Model) toolchainInfoBody() string {
	var b strings.Builder
	b.WriteString("Detected toolchains for this project (change them any time in\n")
	b.WriteString("Settings → Toolchains):\n\n")
	for _, r := range m.toolchainInfo.rows {
		switch {
		case r.path != "":
			b.WriteString("  ✓ " + r.id + " — " + r.path + " (" + r.source + ")\n")
		default:
			b.WriteString("  ✗ " + r.id + " — not found\n")
		}
	}
	b.WriteString("\n  [enter/esc] done")
	return b.String()
}

// updateToolchainInfo closes the summary on enter/esc; every other key is
// swallowed by the modal.
func (m Model) updateToolchainInfo(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "esc", "q":
		m.toolchainInfo = nil
		m.shell.Close()
		m.advanceSetup()
	}
	return m, nil
}
