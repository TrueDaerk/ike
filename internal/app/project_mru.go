package app

import (
	"os"
	"path/filepath"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/project"
)

// project_mru.go implements the direct MRU project chords (#2489):
// project.switchMRU1…9 jump straight to the N-th most recently used *other*
// project, without the picker detour. Telemetry showed project hopping is the
// dominant navigation and that most hops are short visits, so every hop past
// "back to the last one" (project.switchLast, #2398) was costing a dialog.
//
// The numbering is the recent-projects history's, current project excluded
// (project.MRUTargets) — the same order the picker and the Recent Projects
// column list, and both render the digit as the row's Hint, so the chords are
// learned from the lists one already looks at. Number one is the project one
// came from, i.e. project.switchLast's target.

// The digit chords join the terminal allowlist (#805) for the same reason
// project.switchLast is on it: the hop is often made while looking at a shell,
// and ctrl+alt+digit has no meaning there.
func init() {
	for i := 1; i <= project.MaxMRU; i++ {
		terminalGlobalCommands["project.switchMRU"+strconv.Itoa(i)] = true
	}
}

// currentProjectRoot is the absolute, cleaned path of the project the model is
// standing in — the whole IDE is anchored at the process working directory —
// or "" when it cannot be resolved, in which case nothing is filtered out.
func currentProjectRoot() string {
	cur, err := os.Getwd()
	if err != nil {
		return ""
	}
	return filepath.Clean(cur)
}

// mruProjectTargets is the digit chords' target list: the recent projects in
// MRU order, the current one dropped.
func mruProjectTargets() []string {
	return project.MRUTargets(project.History(config.Get()), currentProjectRoot())
}

// handleSwitchMRUProject routes project.switchMRU1…9 (#2489): switch to the
// index-th (1-based) entry of the MRU list. The root is validated off the
// Update loop by project.SwitchTo — a history entry whose directory has since
// vanished reports the usual switch failure rather than half-switching — and
// the switch itself is the ordinary seamless transaction, so a still-parked
// workspace resumes with its tabs and terminals intact (#777) and the history
// records the open.
//
// A digit past the end of the list is a no-op with a short notice: the chord
// family is fixed at nine, the list rarely is.
func (m Model) handleSwitchMRUProject(index int) (tea.Model, tea.Cmd) {
	targets := mruProjectTargets()
	if index < 1 || index > len(targets) {
		m.host.Notify(host.Info, "no recent project "+strconv.Itoa(index))
		return m, nil
	}
	return m, project.SwitchTo(targets[index-1])
}
