package app

import (
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/complexitypanel"
	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/ui"
)

// complexity_baseline.go wires the accepted-debt baseline (Roadmap 0551,
// #2958) into the app: the project's .ike/complexity-baseline.json
// (complexity.Baseline) is read once at build time and again on every
// watcher event for it (a `git checkout`, a hand edit — watch.BaselineChanged),
// handed to the Complexity window (SetBaseline) and the Problems feed
// (complexityDiagnostics) whenever either re-reads the snapshot, and
// gated by complexity.baseline: off hands them nil, so every surface runs
// unchanged with nothing accepted. 'i' in the window (complexity.baseline,
// BaselineToggleMsg) and "Complexity: Accept All Over Threshold"
// (complexity.baselineAll, behind a centered confirmation dialog) mutate
// the baseline and save the file; the watcher then sees IKE's own write
// and re-reads the same content, which is harmless. The file lives beside
// .ike/settings.toml as project data meant to be committed — not in the
// state store.

// ComplexityBaselineMsg runs complexity.baseline ('i' in the pane): accept
// the selected function or group into the baseline, or drop it again.
type ComplexityBaselineMsg struct{}

// ComplexityBaselineAllMsg runs complexity.baselineAll: write every
// over-threshold function of the index into the baseline, after a
// confirmation dialog.
type ComplexityBaselineAllMsg struct{}

// complexityBaselineState is the loaded baseline and its bookkeeping,
// shared by pointer across value-model copies like cxDiff.
type complexityBaselineState struct {
	path string
	// baseline is the file's content, nil without a file or after a
	// malformed one.
	baseline *complexity.Baseline
	// err is the load error of a malformed file, kept until it is reported
	// once (reported) — buildModel cannot notify yet — or the file loads.
	err      error
	reported bool
}

// newComplexityBaselineState reads the project file at build time.
func newComplexityBaselineState(root string) *complexityBaselineState {
	st := &complexityBaselineState{path: complexity.BaselinePath(root)}
	st.load()
	return st
}

// load (re-)reads the file: a missing one is no baseline, a malformed one
// is no baseline plus a pending error.
func (st *complexityBaselineState) load() {
	b, err := complexity.LoadBaseline(st.path)
	st.baseline, st.err = b, err
	if err == nil {
		st.reported = false
	}
}

// complexityBaselineOn reads the complexity.baseline setting (default on
// for a bare test model).
func complexityBaselineOn() bool {
	if c := config.Get(); c != nil {
		return c.Complexity.Baseline
	}
	return true
}

// complexityBaseline is the baseline every surface applies: nil while the
// setting is off or there is no (valid) file.
func (m Model) complexityBaseline() *complexity.Baseline {
	if m.cxBase == nil || !complexityBaselineOn() {
		return nil
	}
	return m.cxBase.baseline
}

// reportComplexityBaseline notifies a malformed file once; called where
// the baseline is applied, so the toast lands after the model exists.
func (m *Model) reportComplexityBaseline() {
	st := m.cxBase
	if st == nil || st.err == nil || st.reported {
		return
	}
	st.reported = true
	m.host.Notify(host.Warn, "complexity: baseline ignored: "+st.err.Error())
}

// applyComplexityBaseline hands the baseline in effect to an open pane
// and republishes the Problems feed — on load, on every file change, on a
// toggle and on a reload of the setting.
func (m *Model) applyComplexityBaseline() {
	m.reportComplexityBaseline()
	if p := m.complexityPanel(); p != nil {
		p.SetBaseline(m.complexityBaseline())
		p.SetHidden(m.complexityHidden()) // #3031: the same file's hidden section
	}
	m.feedComplexityProblems(config.Get().Complexity)
}

// reloadComplexityBaseline is the watcher route (watch.BaselineChanged):
// re-read the file and re-apply it.
func (m *Model) reloadComplexityBaseline() {
	if m.cxBase == nil {
		return
	}
	m.cxBase.load()
	m.applyComplexityBaseline()
}

// baselineRel renders an absolute snapshot path the way the baseline keys
// files: relative to the index root, slash-separated.
func baselineRel(root, path string) string {
	if root != "" {
		if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(path)
}

// saveComplexityBaseline writes the state's baseline to the project file
// and reports the outcome; a failed write keeps the in-memory change (the
// next toggle retries) and says why.
func (m *Model) saveComplexityBaseline(what string) {
	st := m.cxBase
	if err := st.baseline.Save(st.path); err != nil {
		m.host.Notify(host.Error, "complexity: baseline not saved: "+err.Error())
		return
	}
	st.err, st.reported = nil, false
	m.host.Notify(host.Info, "complexity: "+what+" ("+displayPath(st.path)+")")
}

// baselineEditable reports whether a baseline edit may proceed: the
// setting on and a state to edit; it says why not otherwise.
func (m *Model) baselineEditable() bool {
	if m.cxBase == nil {
		return false
	}
	if !complexityBaselineOn() {
		m.host.Notify(host.Warn, "complexity: the baseline is off (complexity.baseline)")
		return false
	}
	return true
}

// toggleComplexityBaselineRow is the complexity.baseline handler ('i'): an
// unopened panel is opened first; a row with nothing to toggle says so.
func (m *Model) toggleComplexityBaselineRow() tea.Cmd {
	p := m.complexityPanel()
	if p == nil {
		m.openComplexityPanel()
		if p = m.complexityPanel(); p == nil {
			return nil
		}
	}
	cmd, ok := p.ToggleBaseline()
	if !ok {
		m.host.Notify(host.Info, "complexity: select an over-threshold function or group to accept, or an accepted one to drop")
		return nil
	}
	return cmd
}

// handleComplexityBaselineToggle applies the pane's BaselineToggleMsg:
// records the items at their current metrics (a worsened function
// re-accepted records the new ones) or drops their entries, then saves.
func (m *Model) handleComplexityBaselineToggle(msg complexitypanel.BaselineToggleMsg) {
	if !m.baselineEditable() || len(msg.Items) == 0 {
		return
	}
	st := m.cxBase
	if st.baseline == nil {
		c := config.Get().Complexity
		st.baseline = complexity.NewBaseline(c.CCNWarn, c.CognitiveWarn)
	}
	n := 0
	for _, it := range msg.Items {
		if msg.Remove {
			if st.baseline.Remove(complexity.EntryKey{File: it.Rel, Function: it.Fn.Name, Ordinal: it.Ordinal}) {
				n++
			}
			continue
		}
		st.baseline.Accept(it.Rel, it.Fn, it.Ordinal)
		n++
	}
	what := "accepted " + baselineCount(n, msg.Items)
	if msg.Remove {
		what = "dropped " + baselineCount(n, msg.Items) + " from the baseline"
	}
	m.saveComplexityBaseline(what)
	m.applyComplexityBaseline()
}

// baselineCount names what a toggle touched: "foo" for one function, "3
// functions" for more.
func baselineCount(n int, items []complexitypanel.BaselineItem) string {
	if n == 1 && len(items) == 1 {
		return items[0].Fn.Name
	}
	return strconv.Itoa(n) + " functions"
}

// overThresholdFunctions lists every function of the index snapshot at or
// above a CCN or cognitive threshold — its file's, #2960 — keyed the
// baseline's way.
func (m Model) overThresholdFunctions() []complexitypanel.BaselineItem {
	if m.complexityIndex == nil {
		return nil
	}
	res := m.complexityResolver(config.Get().Complexity)
	root := m.complexityIndex.Root()
	v := m.complexityIndex.Snapshot()
	var out []complexitypanel.BaselineItem
	for _, path := range v.Paths() {
		fns := v.Files[path]
		rel, th := baselineRel(root, path), res(path)
		mask := m.complexityHidden().Mask(rel, fns) // a hidden function (#3031) is nobody's debt
		for i, ord := range complexity.Ordinals(fns) {
			if th.Over(fns[i]) && !(mask != nil && mask[i]) {
				out = append(out, complexitypanel.BaselineItem{Rel: rel, Fn: fns[i], Ordinal: ord})
			}
		}
	}
	return out
}

// openComplexityBaselineDialog is the complexity.baselineAll handler: it
// counts the over-threshold functions and raises the confirmation dialog
// — a centered, bordered dialog in the floating shell, like the forge
// event dialog — unless another overlay holds the shell, the setting is
// off or there is nothing to accept.
func (m *Model) openComplexityBaselineDialog() {
	if !m.baselineEditable() {
		return
	}
	if m.shell.IsOpen() {
		m.host.Notify(host.Info, "complexity: close the open dialog first")
		return
	}
	n := len(m.overThresholdFunctions())
	if n == 0 {
		m.host.Notify(host.Info, "complexity: no function is over threshold — nothing to accept")
		return
	}
	m.cxBaseDialog = true
	m.shell.SetContent(ui.ModelContent{Heading: "Complexity: accept all over threshold", Body: func() string { return m.complexityBaselineDialogBody(n) }})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// complexityBaselineDialogOpen reports whether the dialog owns the
// keyboard.
func (m Model) complexityBaselineDialogOpen() bool { return m.cxBaseDialog && m.shell.IsOpen() }

// complexityBaselineDialogBody explains what the bulk action writes.
func (m Model) complexityBaselineDialogBody(n int) string {
	c := config.Get().Complexity
	var b strings.Builder
	b.WriteString("Record " + plural(n, "function", "functions") + " at or above the thresholds\n")
	b.WriteString("(CCN ≥ " + strconv.Itoa(c.CCNWarn) + " or cognitive ≥ " + strconv.Itoa(c.CognitiveWarn))
	if len(c.Overrides) > 0 {
		b.WriteString(", or a file's complexity.overrides entry") // #2960
	}
	b.WriteString(") as accepted debt in\n")
	b.WriteString(displayPath(m.cxBase.path) + "?\n\n")
	b.WriteString("The file replaces any earlier baseline. Only functions that are\n")
	b.WriteString("new or got worse will count as over threshold afterwards;\n")
	b.WriteString("commit the file so CI and teammates share the same baseline.\n\n")
	b.WriteString("  [enter/y] accept all   [esc/n] cancel")
	return b.String()
}

// updateComplexityBaselineDialog consumes every key while the dialog is
// open: enter / y writes the baseline, esc / n cancels.
func (m Model) updateComplexityBaselineDialog(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "y":
		m.closeComplexityBaselineDialog()
		m.acceptAllComplexityBaseline()
	case "esc", "n", "q":
		m.closeComplexityBaselineDialog()
	}
	return m, nil
}

// closeComplexityBaselineDialog dismisses the dialog.
func (m *Model) closeComplexityBaselineDialog() {
	m.cxBaseDialog = false
	m.shell.Close()
}

// acceptAllComplexityBaseline writes every over-threshold function of the
// snapshot into a fresh baseline — the file `ike complexity
// --write-baseline` would produce — and applies it.
func (m *Model) acceptAllComplexityBaseline() {
	if !m.baselineEditable() {
		return
	}
	c := config.Get().Complexity
	b := complexity.NewBaseline(c.CCNWarn, c.CognitiveWarn)
	b.Hidden = m.cxBase.baseline.HiddenOf() // the hidden set (#3031) survives a reseed
	items := m.overThresholdFunctions()
	for _, it := range items {
		b.Accept(it.Rel, it.Fn, it.Ordinal)
	}
	m.cxBase.baseline = b
	m.saveComplexityBaseline("accepted " + plural(b.Len(), "function", "functions") + " over threshold")
	m.applyComplexityBaseline()
}
