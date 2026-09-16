package app

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	ilsp "ike/internal/lsp"
	"ike/internal/pane"
)

// paneslots.go reserves pane numbers for the explorer and the tool windows
// (#2592). The numbers of #2407 are geometric: with two editors open the VCS
// window is 4, with one it is 3, so reaching a tool by chord means reading its
// badge first — which is exactly what a muscle-memory chord is supposed to
// spare. A reserved number belongs to its tool instead:
//
//   - the explorer is always 1, not configurable;
//   - layout.pane_slots pins the other tools ("terminal=2, vcs=3", 2…9) —
//     built-in windows by id and [[tools.custom]] tools by name (#2601);
//   - the document panes take the numbers *after* the highest reserved one, in
//     the geometric reading order they always had;
//   - a reserved number stays with its tool while the tool is closed — the gap
//     keeps every other number stable — and the chord then *opens* it.
//
// A tool with no entry in the table is numbered like a document pane, so a
// short default table leaves the editors most of the nine chords.

// paneSlotDef is one reservable tool: the id used in layout.pane_slots, the
// pane kind the number addresses, the name a notification calls it by, and the
// toggle the chord runs when no pane of that kind is on screen. open is nil
// for the two windows that have no toggle command of their own — the debug
// area and the HTTP response viewer open with a session and a request — whose
// reserved number simply stays a gap until something else opens them.
//
// tool is set only for a [[tools.custom]] slot (#2601): every custom tool pane
// is a terminal pane, so the kind alone cannot tell two of them apart and the
// claim compares the session's tool name instead. It stays "" for the built-in
// windows, which a kind identifies exactly.
type paneSlotDef struct {
	id    string
	kind  pane.Kind
	label string
	open  func(*Model) tea.Cmd
	tool  string
}

// paneSlotDefs lists every tool a number can be reserved for, the explorer
// included. The openers are the tool toggle commands' own routes
// (togglePanel / openToolPane), so a chord on a closed tool opens it exactly
// the way its command does — placement, seeding and focus restore included.
// It is a package-level table because the numbering reads it for every pane of
// every frame; the openers take the model as a parameter and capture nothing.
var paneSlotDefs = []paneSlotDef{
	{id: config.PaneSlotExplorer, kind: pane.KindExplorer, label: "Explorer", open: func(m *Model) tea.Cmd { m.toggleExplorer(); return nil }},
	{id: "terminal", kind: pane.KindTerminal, label: "Terminal", open: func(m *Model) tea.Cmd { m.toggleTerminal(); return nil }},
	{id: "vcs", kind: pane.KindVCS, label: "VCS", open: func(m *Model) tea.Cmd { m.toggleVCSPanel(); return nil }},
	{id: "problems", kind: pane.KindProblems, label: "Problems", open: func(m *Model) tea.Cmd { m.toggleProblemsPanel(); return nil }},
	{id: "structure", kind: pane.KindStructure, label: "Structure", open: func(m *Model) tea.Cmd { m.toggleStructurePanel(); return nil }},
	{id: "usages", kind: pane.KindUsages, label: "Usages", open: func(m *Model) tea.Cmd { m.toggleUsagesPanel(); return nil }},
	{id: "breakpoints", kind: pane.KindBreakpoints, label: "Breakpoints", open: func(m *Model) tea.Cmd { m.toggleBreakpointsPanel(); return nil }},
	{id: "tests", kind: pane.KindTests, label: "Test Results", open: func(m *Model) tea.Cmd { m.toggleTestsPanel(); return nil }},
	{id: "issues", kind: pane.KindIssues, label: "GitHub Issues", open: func(m *Model) tea.Cmd { return m.toggleIssuesPanel() }},
	{id: "dom", kind: pane.KindDOM, label: "DOM Inspector", open: func(m *Model) tea.Cmd { m.toggleDOMPanel(); return nil }},
	{id: "xdoctor", kind: pane.KindDoctor, label: "Xdebug Doctor", open: func(m *Model) tea.Cmd { m.toggleDoctorPanel(); return nil }},
	{id: "lspdoctor", kind: pane.KindLSPDoctor, label: "LSP Doctor", open: func(m *Model) tea.Cmd { return m.handleLSPDoctor(ilsp.DoctorMsg{}) }},
	{id: "deps", kind: pane.KindDeps, label: "Dependencies", open: func(m *Model) tea.Cmd { return m.toggleDepsPanel() }},
	{id: "time", kind: pane.KindTime, label: "Project Time", open: func(m *Model) tea.Cmd { return m.toggleTimePanel() }},
	{id: "usage", kind: pane.KindUsage, label: "Usage Report", open: func(m *Model) tea.Cmd { return m.toggleUsagePanel() }},
	{id: "debug", kind: pane.KindDebug, label: "Debug"},
	{id: "http", kind: pane.KindHTTP, label: "HTTP Response"},
}

// customPaneSlotDef is the dynamic counterpart of paneSlotDefs for a
// [[tools.custom]] entry (#2601): the id is the tool name, the pane a terminal
// carrying that tool's session, and the opener the tool.<name> route itself —
// so a chord on a closed custom tool spawns it with the placement, slot
// assignment and focus handling its own command gives it, and a chord on an
// open one goes there. It is built per read rather than listed in the table
// because the set of custom tools is config, not code.
func customPaneSlotDef(name string) paneSlotDef {
	return paneSlotDef{
		id:    name,
		kind:  pane.KindTerminal,
		label: name,
		tool:  name,
		open:  func(m *Model) tea.Cmd { m.openTool(name, false); return nil },
	}
}

// paneSlotTable reads layout.pane_slots into number → tool. The explorer's 1
// is added last and unconditionally: it is the one number no configuration can
// move or take away. A missing key (a bare host.MapConfig in a test, a plugin
// host) reads as the shipped default table, the same way pane_numbers reads as
// "on"; an empty value is a real, empty table — every number then flows. A
// name matching no built-in window is looked up among the configured
// [[tools.custom]] entries (#2601), built-ins first, so a custom tool sharing a
// window's id never shadows it — the config layer diagnoses that collision.
func (m Model) paneSlotTable() map[int]paneSlotDef {
	raw, ok := m.host.Config().Get("layout.pane_slots")
	entries := splitPaneSlots(raw)
	if !ok {
		entries = config.DefaultPaneSlots()
	}
	defs := make(map[string]paneSlotDef, len(paneSlotDefs))
	for _, d := range paneSlotDefs {
		defs[d.id] = d
	}
	custom := customToolNames()
	out := map[int]paneSlotDef{}
	slots, _ := config.ParsePaneSlots(entries, custom...) // the config layer reports the drops
	for _, s := range slots {
		switch {
		case defs[s.Tool].id != "":
			out[s.Number] = defs[s.Tool]
		case slices.Contains(custom, s.Tool):
			out[s.Number] = customPaneSlotDef(s.Tool)
		}
	}
	out[1] = defs[config.PaneSlotExplorer]
	return out
}

// customToolNames lists the configured [[tools.custom]] names — the tool ids
// layout.pane_slots accepts besides the built-in windows (#2601).
func customToolNames() []string {
	c := config.Get()
	if c == nil {
		return nil
	}
	return c.Tools.CustomNames()
}

// splitPaneSlots reads the flat config value — the entries joined with commas
// — back into elements.
func splitPaneSlots(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// paneSlotKind is the kind a pane claims a reserved number with, and — for a
// terminal running a named tool — that tool's name (#2601), which is the only
// thing telling two custom tool panes apart. It is the pane's own kind, except
// for the tab hosts (#1989, #573, #1778): there the *active* tab decides,
// because that is the tool the pane is currently showing — a pane displaying
// its terminal tab answers the terminal chord, and switching tabs moves the
// claim with the content on screen.
func (m Model) paneSlotKind(key string) (pane.Kind, string, bool) {
	inst := m.activeWS().Panes.Get(key)
	if inst == nil {
		return 0, "", false
	}
	if c := inst.ActiveContent(); c != nil {
		return c.Kind(), "", true
	}
	if inst.Kind() == pane.KindEditor {
		if t := inst.TabTerminal(inst.ActiveTab()); t != nil {
			return pane.KindTerminal, t.Tool(), true
		}
		return inst.Kind(), "", true
	}
	if inst.Kind() == pane.KindTerminal {
		return pane.KindTerminal, inst.Terminal().Tool(), true
	}
	return inst.Kind(), "", true
}

// paneNumberAssign maps every addressable number to the pane carrying it.
// Reserved numbers are handed out first — to the earliest pane in reading
// order whose kind (or, for a custom tool, whose tool name) claims them — and
// everything left over flows into the numbers past the highest reserved one,
// in that same reading order. A reserved number whose tool is not open is
// simply absent: the gap is what keeps the other numbers from shifting when a
// tool comes and goes. A pane running a pinned custom tool answers that pin
// and nothing else, so two custom tools on two numbers never trade chords.
func (m Model) paneNumberAssign() map[int]string {
	slots := m.paneSlotTable()
	byKind := make(map[pane.Kind]int, len(slots))
	byTool := make(map[string]int, len(slots))
	maxReserved := 0
	for n, d := range slots {
		if d.tool != "" {
			byTool[d.tool] = n
		} else {
			byKind[d.kind] = n
		}
		if n > maxReserved {
			maxReserved = n
		}
	}
	order := m.paneNumberOrder()
	out := make(map[int]string, len(order))
	reserved := make(map[string]bool, len(order))
	for _, key := range order {
		kind, tool, ok := m.paneSlotKind(key)
		if !ok {
			continue
		}
		n, claimed := 0, false
		if tool != "" {
			n, claimed = byTool[tool]
		}
		if !claimed {
			n, claimed = byKind[kind]
		}
		if !claimed || out[n] != "" {
			continue
		}
		out[n], reserved[key] = key, true
	}
	next := maxReserved + 1
	for _, key := range order {
		if reserved[key] || next > paneNumberMax {
			continue
		}
		out[next] = key
		next++
	}
	return out
}
