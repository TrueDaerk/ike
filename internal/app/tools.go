package app

import (
	"image/color"
	"strings"

	"ike/internal/config"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/plugin"
	"ike/internal/theme"
)

// tools.go implements the custom TUI tool panes (#741): [[tools.custom]]
// config entries become palette commands ("tool.<name>") that open a pane
// running the configured program (lazygit, htop, k9s, …). The pane reuses the
// terminal machinery but is chromed and persisted as a tool, its exit closes
// the pane, and re-invoking the command focuses the existing pane (toggle
// semantics like the other tool windows).

// ToolOpenMsg asks the root model to open (or focus) the tool pane for the
// named [[tools.custom]] entry. Dispatched by the tool.<name> commands.
type ToolOpenMsg struct{ Name string }

// toolCommands builds one palette command per configured tool. It runs on
// every registry query (Capabilities is lazy), so a config reload adding or
// removing tools re-shapes the command set without re-registration.
func toolCommands() []plugin.Command {
	c := config.Get()
	if c == nil {
		return nil
	}
	var cmds []plugin.Command
	for _, t := range c.Tools.Custom {
		if t.Name == "" || t.Command == "" {
			continue
		}
		cmds = append(cmds, appCommand(
			"tool."+toolSlug(t.Name),
			"Tool: "+t.Name,
			ToolOpenMsg{Name: t.Name},
		))
	}
	return cmds
}

// toolSlug renders a tool name as a command-id suffix: lower-case, runs of
// non-alphanumerics collapse to one dash.
func toolSlug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// toolEntry resolves a configured tool by name.
func toolEntry(name string) (config.ToolEntry, bool) {
	c := config.Get()
	if c == nil {
		return config.ToolEntry{}, false
	}
	for _, t := range c.Tools.Custom {
		if t.Name == name {
			return t, true
		}
	}
	return config.ToolEntry{}, false
}

// toolPane returns the pane instance hosting the named tool, nil when none.
func (m Model) toolPane(name string) *pane.Instance {
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst != nil && inst.Kind() == pane.KindTerminal && inst.Terminal().Tool() == name {
			return inst
		}
	}
	return nil
}

// openTool is the tool.<name> state machine (#741), mirroring
// terminal.toggle: no pane → spawn one at the configured placement; pane
// exists but is not focused → focus it; focused → return focus to the
// remembered pane.
func (m *Model) openTool(name string) {
	if inst := m.toolPane(name); inst != nil {
		if m.activeWS().Panes.Focused() != inst.Key() {
			m.activeWS().ReturnFocus = m.activeWS().Panes.Focused()
			m.setFocus(inst.Key())
			return
		}
		target := m.activeWS().ReturnFocus
		if target == "" || !m.activeWS().Panes.Has(target) {
			target = m.activeEditorKey()
		}
		if target == "" || !m.activeWS().Panes.Has(target) {
			target = pane.ExplorerKey
		}
		m.setFocus(target)
		return
	}
	entry, ok := toolEntry(name)
	if !ok {
		return
	}
	target := m.activeEditorKey()
	if target == "" {
		target = m.activeWS().Panes.Focused()
	}
	if target == "" || m.activeWS().Tree == nil {
		return
	}
	zone := layout.ZoneBottom
	if entry.Placement == "right" {
		zone = layout.ZoneRight
	}
	dir := entry.Cwd
	if dir == "" {
		dir = "."
	}
	argv := append([]string{entry.Command}, entry.Args...)
	m.activeWS().ReturnFocus = m.activeWS().Panes.Focused()
	key := m.activeWS().Panes.AddTool(entry.Name, argv, dir, toolSpawnEnv(m.pal()), m.host.Send)
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, key, zone)
	if !ok {
		m.activeWS().Panes.Close(key)
		return
	}
	m.activeWS().Tree = tree
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// toolSpawnEnv is the environment overlay for tool processes: the toolchain
// overlay every terminal gets, plus the IKE_THEME_* variables so a tool whose
// config can reference environment values follows the IDE theme (#741). The
// variables are documented in the wiki; IKE never rewrites a tool's own
// config files.
func toolSpawnEnv(pal *theme.Palette) []string {
	env := terminalEnv()
	if pal == nil {
		return env
	}
	dark := "false"
	if pal.Dark {
		dark = "true"
	}
	env = append(env,
		"IKE_THEME_NAME="+pal.Name,
		"IKE_THEME_DARK="+dark,
		"IKE_THEME_BACKGROUND="+hexColor(pal.Background),
		"IKE_THEME_FOREGROUND="+hexColor(pal.Foreground),
		"IKE_THEME_ACCENT="+hexColor(pal.Accent),
		"IKE_THEME_SELECTION="+hexColor(pal.Selection),
		"IKE_THEME_BORDER="+hexColor(pal.Border),
		"IKE_THEME_SUCCESS="+hexColor(pal.Success),
		"IKE_THEME_WARNING="+hexColor(pal.Warning),
		"IKE_THEME_ERROR="+hexColor(pal.Error),
		"IKE_THEME_INFO="+hexColor(pal.Info),
	)
	return env
}

// hexColor renders a palette color as #rrggbb, "" for nil.
func hexColor(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	const digits = "0123456789abcdef"
	out := []byte{'#', 0, 0, 0, 0, 0, 0}
	for i, v := range []uint32{r >> 8, g >> 8, b >> 8} {
		out[1+i*2] = digits[v>>4]
		out[2+i*2] = digits[v&0xf]
	}
	return string(out)
}
