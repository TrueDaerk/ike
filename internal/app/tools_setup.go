package app

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/toolcatalog"
	"ike/internal/ui"
)

// tools_setup.go is the tool-pane setup dialog (#751–#753): a checkbox list
// of curated TUI tools (lazygit, lazydocker, sqlit, …) offered as the last
// post-tour setup step and re-runnable any time via the tools.setup palette
// command. Confirming writes the [[tools.custom]] entries at user scope (the
// tool.<name> palette commands appear immediately) and installs missing
// binaries through the catalog recipes, with results toasted. Entries whose
// requirement gate fails (lazydocker without Docker) are not offered; entries
// already configured are not offered again.

// ShowToolSetupMsg opens the dialog outside the setup flow (tools.setup).
type ShowToolSetupMsg struct{}

// setupCatalog lists the offerable catalog entries; a seam for tests.
var setupCatalog = toolcatalog.Offered

// toolSetupState is the open dialog: the offered entries with their install
// state and checkbox, and the cursor.
type toolSetupState struct {
	rows   []toolSetupRow
	cursor int
}

type toolSetupRow struct {
	entry     toolcatalog.Entry
	installed bool
	checked   bool
}

// openToolSetup shows the dialog; false when every offerable tool is already
// configured (or the catalog is empty on this system). Already-installed
// tools start checked — adding them is a pure config write; missing ones
// start unchecked so an install is always an explicit choice.
func (m *Model) openToolSetup() bool {
	configured := map[string]bool{}
	if c := config.Get(); c != nil {
		for _, t := range c.Tools.Custom {
			configured[t.Name] = true
		}
	}
	var rows []toolSetupRow
	for _, e := range setupCatalog() {
		if configured[e.Name] {
			continue
		}
		installed := e.Installed()
		rows = append(rows, toolSetupRow{entry: e, installed: installed, checked: installed})
	}
	if len(rows) == 0 {
		return false
	}
	m.toolSetup = &toolSetupState{rows: rows}
	m.shell.SetContent(ui.ModelContent{Heading: "Set up tool panes", Body: m.toolSetupBody})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
	return true
}

// toolSetupOpen reports whether the dialog is showing.
func (m Model) toolSetupOpen() bool { return m.toolSetup != nil && m.shell.IsOpen() }

// toolSetupBody renders the checkbox list with install-state markers.
func (m Model) toolSetupBody() string {
	ts := m.toolSetup
	var b strings.Builder
	b.WriteString("IKE can embed TUI tools as panes (each becomes a tool.<name>\n")
	b.WriteString("palette command). Pick the ones you want — missing binaries are\n")
	b.WriteString("installed for you. Manage them later in Settings → Tools.\n\n")
	for i, r := range ts.rows {
		marker := "  "
		if i == ts.cursor {
			marker = "▸ "
		}
		box := "[ ]"
		if r.checked {
			box = "[x]"
		}
		state := ""
		switch {
		case r.installed:
			state = "installed"
		default:
			if argv, ok := r.entry.InstallArgv(); ok {
				state = "installs via " + strings.Join(argv, " ")
			} else {
				state = "no installer found"
			}
		}
		b.WriteString(marker + box + " " + r.entry.Name + " — " + r.entry.Description +
			" (" + state + ")\n")
	}
	b.WriteString("\n")
	b.WriteString("  [space] toggle   [a] all   [n] none   [j/k] move   [enter] set up checked   [esc] skip")
	return b.String()
}

// updateToolSetup consumes every key while the dialog is open, mirroring the
// LSP onboarding dialog: space toggles, a/n check/uncheck all, enter commits,
// esc skips; everything else is swallowed by the modal.
func (m Model) updateToolSetup(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	ts := m.toolSetup
	switch msg.String() {
	case "j", "down":
		if ts.cursor < len(ts.rows)-1 {
			ts.cursor++
		}
	case "k", "up":
		if ts.cursor > 0 {
			ts.cursor--
		}
	case "space", " ":
		ts.rows[ts.cursor].checked = !ts.rows[ts.cursor].checked
	case "a":
		for i := range ts.rows {
			ts.rows[i].checked = true
		}
	case "n":
		for i := range ts.rows {
			ts.rows[i].checked = false
		}
	case "enter":
		return m.toolSetupConfirm()
	case "esc":
		return m.closeToolSetup(), nil
	}
	return m, nil
}

// toolSetupConfirm writes the checked entries into tools.custom at user scope
// (inside the returned command, #123 rules), reloads so the tool.<name>
// commands appear immediately, and kicks one install per checked tool whose
// binary is missing (batched — the installs do not read the config, so they
// need not wait for the write). Install results arrive as toolcatalog.InstallResultMsg
// toasts; a failed install keeps the config entry — the tool works as soon as
// the binary is installed by hand.
func (m Model) toolSetupConfirm() (tea.Model, tea.Cmd) {
	ts, opts := m.toolSetup, m.cfgOpts
	var picked []toolcatalog.Entry
	var installs []tea.Cmd
	for _, r := range ts.rows {
		if !r.checked {
			continue
		}
		picked = append(picked, r.entry)
		if !r.installed {
			installs = append(installs, toolcatalog.Install(r.entry))
		}
	}
	tm := m.closeToolSetup()
	if len(picked) == 0 {
		return tm, nil
	}
	write := func() tea.Msg {
		var entries []config.ToolEntry
		if c := config.Get(); c != nil {
			entries = append(entries, c.Tools.Custom...)
		}
		for _, e := range picked {
			entries = append(entries, config.ToolEntry{
				Name:      e.Name,
				Command:   e.Command,
				Args:      e.Args,
				Placement: e.Placement,
			})
		}
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
		var diags []config.Diagnostic
		if err := config.WriteKey(opts, config.UserScope, "tools.custom", toolEntriesRaw(entries)); err != nil {
			diags = append(diags, config.Diagnostic{Field: "tools.custom", Message: err.Error()})
		}
		c, loadDiags := config.Load(opts)
		return config.ConfigReloadedMsg{Config: c, Diags: append(loadDiags, diags...)}
	}
	if len(installs) == 0 {
		return tm, write
	}
	return tm, tea.Batch(append([]tea.Cmd{write}, installs...)...)
}

// toolEntriesRaw renders tool entries for the TOML write, omitting empties
// (the settings Tools page shape).
func toolEntriesRaw(entries []config.ToolEntry) []map[string]any {
	raw := make([]map[string]any, len(entries))
	for i, e := range entries {
		r := map[string]any{"name": e.Name, "command": e.Command}
		if len(e.Args) > 0 {
			r["args"] = e.Args
		}
		if e.Cwd != "" {
			r["cwd"] = e.Cwd
		}
		if e.Placement != "" {
			r["placement"] = e.Placement
		}
		raw[i] = r
	}
	return raw
}

// closeToolSetup dismisses the dialog and continues the post-tour setup flow
// (a no-op outside it).
func (m Model) closeToolSetup() tea.Model {
	m.toolSetup = nil
	m.shell.Close()
	m.advanceSetup()
	return m
}
