package app

import (
	"image/color"
	"strings"

	"ike/internal/config"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/plugin"
	"ike/internal/terminal"
	"ike/internal/theme"
)

// tools.go implements the custom TUI tool panes (#741): [[tools.custom]]
// config entries become palette commands ("tool.<name>") that open a pane
// running the configured program (lazygit, htop, k9s, …). The pane reuses the
// terminal machinery but is chromed and persisted as a tool, its exit closes
// the pane, and re-invoking the command focuses the existing pane (toggle
// semantics like the other tool windows).

// ToolOpenMsg asks the root model to open (or focus) the tool pane for the
// named [[tools.custom]] entry. Dispatched by the tool.<name> commands; New
// (the tool.<name>.new command, #835) forces a fresh instance for tools
// configured with multiple = true.
type ToolOpenMsg struct {
	Name string
	New  bool
}

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
		// Validation drops multiple on global entries (#1890); the extra
		// check keeps a hand-set config from minting a .new command anyway.
		if t.Multiple && !t.Global {
			cmds = append(cmds, appCommand(
				"tool."+toolSlug(t.Name)+".new",
				"Tool: "+t.Name+" (New Instance)",
				ToolOpenMsg{Name: t.Name, New: true},
			))
		}
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

// toolPane returns the dedicated pane instance hosting the named tool, nil
// when none (tab-hosted instances are found via toolLocations).
func (m Model) toolPane(name string) *pane.Instance {
	for _, loc := range m.toolLocations(name) {
		if loc.tab < 0 {
			return m.activeWS().Panes.Get(loc.key)
		}
	}
	return nil
}

// toolLoc is one live instance of a tool: the pane hosting it and, for an
// editor-hosted terminal tab, the tab index (-1 for a dedicated pane).
type toolLoc struct {
	key string
	tab int
}

// toolLocations finds every live instance of the named tool in the active
// workspace: dedicated tool panes and editor-hosted tool tabs alike (#835) —
// a tool moved into a tab list (center drop, #708) must still be found by
// the toggle, or the single-instance rule silently breaks into a second
// spawn.
func (m Model) toolLocations(name string) []toolLoc {
	var out []toolLoc
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil {
			continue
		}
		switch inst.Kind() {
		case pane.KindTerminal:
			if inst.Terminal().Tool() == name {
				out = append(out, toolLoc{key: key, tab: -1})
			}
		case pane.KindEditor:
			for i := 0; i < inst.TabCount(); i++ {
				if t := inst.TabTerminal(i); t != nil && t.Tool() == name {
					out = append(out, toolLoc{key: key, tab: i})
				}
			}
		}
	}
	return out
}

// toolPaneTitle chromes a tool session (#741): ⚙ NAME, no shell machinery.
// The Run tool (#1905) appends the running configuration — which program is
// running is exactly what its pane is there to show.
func toolPaneTitle(t *terminal.Model) string {
	title := "⚙ " + strings.ToUpper(t.Tool())
	if t.Tool() == runToolName && t.Label() != "" {
		title += " — " + t.Label()
	}
	return title
}

// toolSpawn describes one tool session about to be placed: the identity the
// pane carries plus the process behind it. It is the seam the placement code
// works on, so the built-in Run tool (#1905) — whose command comes from a run
// configuration, not from [[tools.custom]] — reaches the same slot / home /
// adaptive rules as a configured tool.
type toolSpawn struct {
	name  string   // tool identity (terminal.Model.SetTool)
	label string   // chrome label; "" keeps the tool name
	argv  []string // program and arguments
	dir   string   // working directory
	env   []string // environment overlay
}

// customToolSpawn describes the session of a [[tools.custom]] entry.
func (m *Model) customToolSpawn(entry config.ToolEntry) toolSpawn {
	dir := entry.Cwd
	if dir == "" {
		dir = "."
	}
	return toolSpawn{
		name: entry.Name,
		argv: append([]string{entry.Command}, entry.Args...),
		dir:  dir,
		env:  toolSpawnEnv(m.pal()),
	}
}

// addToolPane spawns sp as a dedicated tool pane and returns its key.
func (m *Model) addToolPane(sp toolSpawn) string {
	key := m.activeWS().Panes.AddTool(sp.name, sp.argv, sp.dir, sp.env, m.host.Send)
	if sp.label != "" {
		m.activeWS().Panes.Get(key).Terminal().SetLabel(sp.label)
	}
	return key
}

// newToolTab spawns sp as a pane-less session, ready to host as a tab (#836).
func (m *Model) newToolTab(sp toolSpawn) terminal.Model {
	t := m.activeWS().Panes.NewToolSession(sp.name, sp.argv, sp.dir, sp.env, m.host.Send)
	if sp.label != "" {
		t.SetLabel(sp.label)
	}
	return t
}

// placeTool spawns sp at its place and focuses it, in the precedence every
// tool pane follows: a slot assignment (#1897) pins it to the template
// position, else the home position named by placement (#1889), else the
// adaptive split off the active editor (#1588). Reports whether a pane
// materialized; nothing is spawned when it does not.
func (m *Model) placeTool(sp toolSpawn, placement string) bool {
	ws := m.activeWS()
	target := m.activeEditorKey()
	if target == "" {
		target = ws.Panes.Focused()
	}
	if target == "" || ws.Tree == nil {
		return false
	}
	ws.ReturnFocus = ws.Panes.Focused()
	if tpl, slot := assignedSlot(sp.name); slot != "" {
		return m.openToolAtSlot(sp, tpl, slot)
	}
	if zone, ok := toolHomeZone(placement); ok {
		return m.openToolAtHome(sp, zone)
	}
	return m.openToolAdaptive(sp, target)
}

// openToolAdaptive spawns sp as a dedicated pane split off target at the
// adaptive placement (auxZone, #1588) — the fallback for a tool with neither
// a slot assignment nor a home position.
func (m *Model) openToolAdaptive(sp toolSpawn, target string) bool {
	ws := m.activeWS()
	key := m.addToolPane(sp)
	tree, ok := layout.SplitLeaf(ws.Tree, target, key, m.auxZone(target))
	if !ok {
		ws.Panes.Close(key)
		return false
	}
	ws.Tree = tree
	m.setFocus(key)
	m.rememberTool(sp.name, key)
	m.layout()
	saveLayout(ws.Tree, ws.Panes)
	return true
}

// openTool is the tool.<name> state machine (#741), mirroring
// terminal.toggle: no instance → spawn one at the configured home position
// (placement, #1889) or, without one, at the adaptive placement (auxZone,
// #1588); instance exists but is not focused → focus it; focused →
// return focus to the remembered pane. fresh (tool.<name>.new, #835) skips the toggle and
// spawns another instance — only honored for entries with multiple = true,
// so a stale binding cannot break a single-instance tool.
func (m *Model) openTool(name string, fresh bool) {
	entry, ok := toolEntry(name)
	if !ok {
		return
	}
	// A global tool (#1890) is strictly single-instance; validation already
	// rejects global+multiple, this catches a stale binding.
	if fresh && (!entry.Multiple || entry.Global) {
		fresh = false
	}
	// tool.<name> is an explicit (re)open: forget a recorded close (#1903) so
	// the tool restores and follows switches again.
	if entry.Global && m.ws != nil {
		m.ws.ClearGlobalToolClosed(entry.Name)
	}
	if !fresh {
		if locs := m.toolLocations(name); len(locs) > 0 {
			m.toggleTool(name, locs)
			return
		}
	}
	// A global tool not in this workspace may be running detached (#1890):
	// re-attach the live session instead of spawning a second process.
	if entry.Global {
		if term, ok := m.ws.TakeGlobalTool(entry.Name); ok {
			m.attachGlobalTool(entry, term)
			return
		}
	}
	// A slot assignment (#1897) beats the #1889 home position, which beats the
	// adaptive split — placeTool owns that precedence for every tool.
	m.placeTool(m.customToolSpawn(entry), entry.Placement)
}

// toolHomeZone maps a [[tools.custom]] placement value (#1889) to the dock
// zone of the tool's home position; ok=false (empty or unknown value) keeps
// the adaptive auxZone heuristic.
func toolHomeZone(placement string) (layout.Zone, bool) {
	switch placement {
	case "left":
		return layout.ZoneLeft, true
	case "right":
		return layout.ZoneRight, true
	case "top":
		return layout.ZoneTop, true
	case "bottom":
		return layout.ZoneBottom, true
	}
	return 0, false
}

// toolDockShare is the workspace share a tool docked at its home edge takes
// along the dock axis — the tool-window-strip proportion of #811's dock cap.
const toolDockShare = 0.3

// dockOccupant resolves the pane occupying the home dock slot at zone's
// workspace edge, "" when the slot counts as free. The leaf pinned at the
// edge only occupies the slot when it is a tool window itself (isToolKind:
// explorer, terminals/tools, the singleton panels) or a tab host already
// carrying a terminal/tool tab — an editor showing documents there is main
// content, not a dock, and a fresh open docks beside it instead of tabbing
// into it.
func (m *Model) dockOccupant(zone layout.Zone) string {
	key := layout.EdgeLeaf(m.activeWS().Tree, zone)
	if key == "" {
		return ""
	}
	inst := m.activeWS().Panes.Get(key)
	if inst == nil {
		return ""
	}
	if isToolKind(inst.Kind()) {
		return key
	}
	if inst.Kind() == pane.KindEditor {
		for i := 0; i < inst.TabCount(); i++ {
			if inst.TabTerminal(i) != nil {
				return key
			}
		}
	}
	return ""
}

// dockShareZone is how a second occupant shares a dock at zone: side docks
// stack vertically (occupant above, newcomer below), top/bottom strips split
// side by side.
func dockShareZone(zone layout.Zone) layout.Zone {
	if zone == layout.ZoneTop || zone == layout.ZoneBottom {
		return layout.ZoneRight
	}
	return layout.ZoneBottom
}

// dockNewPane puts the already-registered tool pane key at its home dock
// (#1889), the shared tail of every home-position open: beside occupant when
// the edge slot is taken by a pane that cannot host tabs, beside the layout's
// own tool region when the workspace edge is no slot at all (#2191), and
// full-span against the edge when neither holds. Reports ok; the tree is
// unchanged on failure and the caller drops the pane.
func (m *Model) dockNewPane(key string, zone layout.Zone, occupant string) bool {
	ws := m.activeWS()
	if occupant == "" {
		occupant = m.toolRegionLeaf(zone)
	}
	if occupant == "" {
		ws.Tree = layout.DockNew(ws.Tree, key, zone, toolDockShare)
		return true
	}
	tree, ok := layout.SplitLeaf(ws.Tree, occupant, key, dockShareZone(zone))
	if !ok {
		return false
	}
	ws.Tree = tree
	return true
}

// toolRegionLeaf resolves the dock slot *inside* the layout when the
// workspace edge is not one (#2191): a nested arrangement — an editor column
// beside the explorer, an editor area inside a bigger split — keeps its tool
// strip in the region around the editor, and "the bottom of everything" is
// then not where tool output belongs. The editor's ancestor path is walked
// outwards (layout.Hops) and the first sibling region on zone's side that
// holds nothing but tool windows is the strip; its own edge leaf
// (layout.EdgeLeafIn, so an already subdivided strip still names a
// neighbour) is the pane the newcomer splits off. No such region — a plain
// editor beside the editor, or none at all — falls back to the full-span
// workspace dock. A region pane is only ever split beside, never tabbed
// into: joining an unrelated tool's or shell's tab list is exactly what
// #1905 forbids.
func (m *Model) toolRegionLeaf(zone layout.Zone) string {
	ws := m.activeWS()
	if ws.Tree == nil {
		return ""
	}
	anchor := m.activeEditorKey()
	if anchor == "" {
		anchor = ws.Panes.Focused()
	}
	for _, hop := range layout.Hops(ws.Tree, anchor) {
		if hop.Zone != zone || !m.isToolRegion(hop.Sibling) {
			continue
		}
		return layout.EdgeLeafIn(hop.Sibling, zone)
	}
	return ""
}

// isToolRegion reports whether every leaf of the region is a tool window —
// a tool-kind pane or a host carrying nothing but tool tabs (#1989). One
// editor among them makes the region editor area, not a tool strip, so a
// tool must not dock into it.
func (m *Model) isToolRegion(n layout.Node) bool {
	leaves := layout.Leaves(n)
	if len(leaves) == 0 {
		return false
	}
	for _, key := range leaves {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil {
			return false
		}
		if isToolKind(inst.Kind()) || toolTabHost(inst) {
			continue
		}
		return false
	}
	return true
}

// openToolAtHome spawns the tool at its configured home position (#1889), the
// dock slot on the workspace edge named by placement. A slot occupied by a
// tab-capable pane adds the tool as a focused tab there instead of forcing
// another split; any other occupant (explorer, singleton tool windows) shares
// the dock via a perpendicular split; a free slot docks the pane into the
// layout's tool region, or full-span at the edge when there is none (#2191).
// The placement is intent, never state: moving the tool afterwards does not
// rewrite it, so close + reopen returns here.
func (m *Model) openToolAtHome(sp toolSpawn, zone layout.Zone) bool {
	ws := m.activeWS()
	occupant := m.dockOccupant(zone)
	// Global tools (#1890) tab-join too (#2042): the project switch detaches
	// tab-hosted global sessions tab-wise, so sharing the dock is safe.
	if occupant != "" && canHostTabs(ws.Panes.Get(occupant)) && m.ensureTabHost(occupant) {
		ws.Panes.Get(occupant).AddTerminalTab(m.newToolTab(sp))
		m.setFocus(occupant)
		m.rememberTool(sp.name, occupant)
		m.layout()
		saveLayout(ws.Tree, ws.Panes)
		return true
	}
	key := m.addToolPane(sp)
	// A non-tabbable occupant keeps its pane and the tool stacks into the same
	// dock; with no occupant at all the layout's own tool region takes it
	// before the full-span workspace edge does (#2191).
	if !m.dockNewPane(key, zone, occupant) {
		ws.Panes.Close(key)
		return false
	}
	m.setFocus(key)
	m.rememberTool(sp.name, key)
	m.layout()
	saveLayout(ws.Tree, ws.Panes)
	return true
}

// toggleTool applies the focus/return cycle on an existing instance,
// preferring the most recently used one when several are open (#835): not
// focused → focus it (activating its tab for a tab-hosted tool); focused →
// return focus to the remembered pane.
func (m *Model) toggleTool(name string, locs []toolLoc) {
	loc := locs[0]
	if recent := m.toolRecent[name]; recent != "" {
		for _, l := range locs {
			if l.key == recent {
				loc = l
				break
			}
		}
	}
	inst := m.activeWS().Panes.Get(loc.key)
	if inst == nil {
		return
	}
	focusedHere := m.activeWS().Panes.Focused() == loc.key &&
		(loc.tab < 0 || inst.ActiveTab() == loc.tab)
	if !focusedHere {
		m.activeWS().ReturnFocus = m.activeWS().Panes.Focused()
		m.setFocus(loc.key)
		if loc.tab >= 0 {
			inst.ActivateTab(loc.tab)
		}
		m.rememberTool(name, loc.key)
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
}

// rememberTool records the pane the tool was last opened or focused in, so
// the plain tool.<name> toggle targets the most recent instance (#835). The
// map is shared across the value-model copies (lazy init hands every copy
// the same map).
func (m *Model) rememberTool(name, key string) {
	if m.toolRecent == nil {
		m.toolRecent = map[string]string{}
	}
	m.toolRecent[name] = key
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
