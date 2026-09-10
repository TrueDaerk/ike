package app

import (
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
//   - layout.pane_slots pins the other tools ("terminal=2, vcs=3", 2…9);
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
type paneSlotDef struct {
	id    string
	kind  pane.Kind
	label string
	open  func(*Model) tea.Cmd
}

// paneSlotDefs lists every tool a number can be reserved for, the explorer
// included. The openers are the tool toggle commands' own routes
// (togglePanel / openToolPane), so a chord on a closed tool opens it exactly
// the way its command does — placement, seeding and focus restore included.
// It is a package-level table because the numbering reads it for every pane of
// every frame; the openers take the model as a parameter and capture nothing.
var paneSlotDefs = []paneSlotDef{
	{config.PaneSlotExplorer, pane.KindExplorer, "Explorer", func(m *Model) tea.Cmd { m.toggleExplorer(); return nil }},
	{"terminal", pane.KindTerminal, "Terminal", func(m *Model) tea.Cmd { m.toggleTerminal(); return nil }},
	{"vcs", pane.KindVCS, "VCS", func(m *Model) tea.Cmd { m.toggleVCSPanel(); return nil }},
	{"problems", pane.KindProblems, "Problems", func(m *Model) tea.Cmd { m.toggleProblemsPanel(); return nil }},
	{"structure", pane.KindStructure, "Structure", func(m *Model) tea.Cmd { m.toggleStructurePanel(); return nil }},
	{"usages", pane.KindUsages, "Usages", func(m *Model) tea.Cmd { m.toggleUsagesPanel(); return nil }},
	{"breakpoints", pane.KindBreakpoints, "Breakpoints", func(m *Model) tea.Cmd { m.toggleBreakpointsPanel(); return nil }},
	{"tests", pane.KindTests, "Test Results", func(m *Model) tea.Cmd { m.toggleTestsPanel(); return nil }},
	{"issues", pane.KindIssues, "GitHub Issues", func(m *Model) tea.Cmd { return m.toggleIssuesPanel() }},
	{"dom", pane.KindDOM, "DOM Inspector", func(m *Model) tea.Cmd { m.toggleDOMPanel(); return nil }},
	{"xdoctor", pane.KindDoctor, "Xdebug Doctor", func(m *Model) tea.Cmd { m.toggleDoctorPanel(); return nil }},
	{"lspdoctor", pane.KindLSPDoctor, "LSP Doctor", func(m *Model) tea.Cmd { return m.handleLSPDoctor(ilsp.DoctorMsg{}) }},
	{"deps", pane.KindDeps, "Dependencies", func(m *Model) tea.Cmd { return m.toggleDepsPanel() }},
	{"time", pane.KindTime, "Project Time", func(m *Model) tea.Cmd { return m.toggleTimePanel() }},
	{"usage", pane.KindUsage, "Usage Report", func(m *Model) tea.Cmd { return m.toggleUsagePanel() }},
	{"debug", pane.KindDebug, "Debug", nil},
	{"http", pane.KindHTTP, "HTTP Response", nil},
}

// paneSlotTable reads layout.pane_slots into number → tool. The explorer's 1
// is added last and unconditionally: it is the one number no configuration can
// move or take away. A missing key (a bare host.MapConfig in a test, a plugin
// host) reads as the shipped default table, the same way pane_numbers reads as
// "on"; an empty value is a real, empty table — every number then flows.
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
	out := map[int]paneSlotDef{}
	slots, _ := config.ParsePaneSlots(entries) // the config layer reports the drops
	for _, s := range slots {
		if d, ok := defs[s.Tool]; ok {
			out[s.Number] = d
		}
	}
	out[1] = defs[config.PaneSlotExplorer]
	return out
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

// paneSlotKind is the kind a pane claims a reserved number with. It is the
// pane's own kind, except for the tab hosts (#1989, #573, #1778): there the
// *active* tab decides, because that is the tool the pane is currently
// showing — a pane displaying its terminal tab answers the terminal chord, and
// switching tabs moves the claim with the content on screen.
func (m Model) paneSlotKind(key string) (pane.Kind, bool) {
	inst := m.activeWS().Panes.Get(key)
	if inst == nil {
		return 0, false
	}
	if c := inst.ActiveContent(); c != nil {
		return c.Kind(), true
	}
	if inst.Kind() == pane.KindEditor && inst.TabTerminal(inst.ActiveTab()) != nil {
		return pane.KindTerminal, true
	}
	return inst.Kind(), true
}

// paneNumberAssign maps every addressable number to the pane carrying it.
// Reserved numbers are handed out first — to the earliest pane in reading
// order whose kind claims them — and everything left over flows into the
// numbers past the highest reserved one, in that same reading order. A
// reserved number whose tool is not open is simply absent: the gap is what
// keeps the other numbers from shifting when a tool comes and goes.
func (m Model) paneNumberAssign() map[int]string {
	slots := m.paneSlotTable()
	byKind := make(map[pane.Kind]int, len(slots))
	maxReserved := 0
	for n, d := range slots {
		byKind[d.kind] = n
		if n > maxReserved {
			maxReserved = n
		}
	}
	order := m.paneNumberOrder()
	out := make(map[int]string, len(order))
	reserved := make(map[string]bool, len(order))
	for _, key := range order {
		kind, ok := m.paneSlotKind(key)
		if !ok {
			continue
		}
		n, ok := byKind[kind]
		if !ok || out[n] != "" {
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
