package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/complexitypanel"
	"ike/internal/config"
	"ike/internal/configcmd"
	"ike/internal/host"
)

// complexity_hidden.go wires the hidden set (Roadmap 0551, #3031) into the
// app. Function-level hides live in the `hidden` section of the baseline
// file (.ike/complexity-baseline.json, complexity.HiddenSet), so they ride
// the baseline state (complexityBaselineState: read at build time, re-read
// on every watch.BaselineChanged) — but not its setting: complexityHidden()
// hands every surface the loaded set whether complexity.baseline is on or
// off, and 'x' writes the file either way. File-level hides are a
// complexity.exclude glob written through the normal config pipeline
// (configcmd.WriteAndReload), so the Settings form shows the new pattern
// and the index rewalks without the file through reconfigureComplexityIndex
// — one exclusion mechanism, not two.

// ComplexityHideMsg runs complexity.hide ('x' in the pane): hide the
// selected function — or the selected group / tree / file row's file — or
// un-hide a hidden function shown by hidden:yes.
type ComplexityHideMsg struct{}

// ComplexityHideFileMsg runs complexity.hideFile ('X' in the pane): hide
// the selected row's whole file through complexity.exclude.
type ComplexityHideFileMsg struct{}

// ComplexityShowHiddenMsg runs complexity.showHidden ('H' in the pane):
// toggle the hidden:yes view that lists the hidden functions.
type ComplexityShowHiddenMsg struct{}

// complexityHidden is the hidden set every surface applies: the baseline
// file's `hidden` section, nil without a (valid) file. Unlike
// complexityBaseline it ignores the complexity.baseline setting.
func (m Model) complexityHidden() *complexity.HiddenSet {
	if m.cxBase == nil {
		return nil
	}
	return m.cxBase.baseline.HiddenOf()
}

// complexityPanelOrOpen is the open Complexity window, opened first when
// it is not; nil only when it cannot be opened.
func (m *Model) complexityPanelOrOpen() *complexitypanel.Model {
	p := m.complexityPanel()
	if p == nil {
		m.openComplexityPanel()
		p = m.complexityPanel()
	}
	return p
}

// hideComplexityRow is the complexity.hide handler: the pane's
// HideToggleMsg or HideFileMsg for the selected row, or the reason there is
// none as a notice.
func (m *Model) hideComplexityRow() tea.Cmd {
	p := m.complexityPanelOrOpen()
	if p == nil {
		return nil
	}
	cmd, why := p.ToggleHide()
	if cmd == nil {
		m.host.Notify(host.Info, "complexity: "+why)
	}
	return cmd
}

// hideComplexityFileRow is the complexity.hideFile handler.
func (m *Model) hideComplexityFileRow() tea.Cmd {
	p := m.complexityPanelOrOpen()
	if p == nil {
		return nil
	}
	cmd, why := p.HideFile()
	if cmd == nil {
		m.host.Notify(host.Info, "complexity: "+why)
	}
	return cmd
}

// toggleComplexityHiddenView is the complexity.showHidden handler.
func (m *Model) toggleComplexityHiddenView() {
	p := m.complexityPanelOrOpen()
	if p == nil {
		return
	}
	if p.ToggleHiddenView() {
		m.host.Notify(host.Info, "complexity: showing "+plural(p.HiddenCount(), "hidden function", "hidden functions")+" — x on one un-hides it, H leaves the view")
		return
	}
	m.host.Notify(host.Info, "complexity: hidden functions left out again")
}

// handleComplexityHideToggle applies the pane's HideToggleMsg: adds the
// items to the hidden set (creating the baseline file when there is none)
// or drops them, saves and re-applies. The baseline setting does not gate
// it: hiding is not accepting.
func (m *Model) handleComplexityHideToggle(msg complexitypanel.HideToggleMsg) {
	if m.cxBase == nil || len(msg.Items) == 0 {
		return
	}
	st := m.cxBase
	if st.baseline == nil {
		c := config.Get().Complexity
		st.baseline = complexity.NewBaseline(c.CCNWarn, c.CognitiveWarn)
	}
	n := 0
	for _, it := range msg.Items {
		k := complexity.EntryKey{File: it.Rel, Function: it.Fn.Name, Ordinal: it.Ordinal}
		if msg.Remove {
			if st.baseline.Unhide(k) {
				n++
			}
			continue
		}
		if st.baseline.Hide(k) {
			n++
		}
	}
	what := "hid " + baselineCount(n, msg.Items)
	if msg.Remove {
		what = "un-hid " + baselineCount(n, msg.Items)
	}
	m.saveComplexityBaseline(what)
	m.applyComplexityBaseline()
}

// handleComplexityHideFile applies the pane's HideFileMsg: the globs join
// complexity.exclude in the layer that already holds the key (the project
// file when none does — a hidden vendor directory is project data), through
// the reload pipeline, so the index rewalks and the Settings form shows the
// pattern. A glob already excluded says so and writes nothing.
func (m *Model) handleComplexityHideFile(msg complexitypanel.HideFileMsg) tea.Cmd {
	c := config.Get()
	if c == nil || len(msg.Globs) == 0 {
		return nil
	}
	list := append([]string(nil), c.Complexity.Exclude...)
	added := 0
	for _, g := range msg.Globs {
		if g == "" || containsString(list, g) {
			continue
		}
		list = append(list, g)
		added++
	}
	if added == 0 {
		m.host.Notify(host.Info, "complexity: "+msg.Label+" is already in complexity.exclude")
		return nil
	}
	scope, layer := config.ProjectScope, "project"
	if config.Origin(m.cfgOpts, "complexity.exclude") == "user" {
		scope, layer = config.UserScope, "user"
	}
	m.host.Notify(host.Info, "complexity: hiding "+msg.Label+" (complexity.exclude, "+layer+" settings; "+
		plural(len(list), "pattern", "patterns")+")")
	return configcmd.WriteAndReload(m.cfgOpts, scope, "complexity.exclude", list)
}

// containsString reports whether list holds s.
func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
