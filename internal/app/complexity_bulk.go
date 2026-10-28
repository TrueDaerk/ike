package app

import (
	"strconv"
	"strings"

	"ike/internal/complexitypanel"
	"ike/internal/fuzzy"
	"ike/internal/host"
	"ike/internal/palette"
	"ike/internal/ui"
)

// complexity_bulk.go holds the Complexity window's bulk forms of three
// one-row keys (#3082), for the intentions the telemetry saw hammered:
// complexity.sitesAll (shift+space) expands or collapses the sites of every
// visible row, complexity.groupPick (B) jumps straight to a grouping mode
// through a locked palette picker instead of cycling with b, and
// complexity.baselineMarked (I) accepts or drops every function of the
// filter result in one baseline write, behind the baseline's confirmation
// dialog naming the count.

// ComplexitySitesAllMsg runs complexity.sitesAll.
type ComplexitySitesAllMsg struct{}

// ComplexityGroupPickMsg runs complexity.groupPick: open the picker.
type ComplexityGroupPickMsg struct{}

// ComplexityGroupPickedMsg applies one picked grouping mode.
type ComplexityGroupPickedMsg struct{ Choice complexitypanel.GroupChoice }

// ComplexityBaselineMarkedMsg runs complexity.baselineMarked.
type ComplexityBaselineMarkedMsg struct{}

// ensureComplexityPanel returns the open panel, opening it first when it
// is not; nil when it cannot open.
func (m *Model) ensureComplexityPanel() *complexitypanel.Model {
	if p := m.complexityPanel(); p != nil {
		return p
	}
	m.openComplexityPanel()
	return m.complexityPanel()
}

// toggleAllComplexitySites is the complexity.sitesAll handler.
func (m *Model) toggleAllComplexitySites() {
	p := m.ensureComplexityPanel()
	if p == nil {
		return
	}
	if !p.ToggleAllSites() {
		m.host.Notify(host.Info, "complexity: no visible function has sites to expand")
	}
}

// complexityGroupPrefix selects the grouping picker inside the palette;
// opened locked only, so the rune has no user-facing prefix story.
const complexityGroupPrefix = '¶'

// complexityGroupMode is the palette Mode listing the selected tab's
// grouping modes; the model fills choices before each locked open (the
// tabPickerMode pattern).
type complexityGroupMode struct {
	choices []complexitypanel.GroupChoice
}

// Prefix implements palette.Mode.
func (g *complexityGroupMode) Prefix() rune { return complexityGroupPrefix }

// Placeholder implements palette.Mode.
func (g *complexityGroupMode) Placeholder() string { return "Complexity: group by…" }

// Results implements palette.Mode: the choices in cycle order, fuzzy
// matched on the label, the current one badged.
func (g *complexityGroupMode) Results(query string, _ palette.Context) []palette.Item {
	var items []palette.Item
	for _, c := range g.choices {
		res, ok := fuzzy.Match(query, c.Label)
		if !ok {
			continue
		}
		it := palette.Item{Title: c.Label, Spans: res.Positions, Score: res.Score, Msg: ComplexityGroupPickedMsg{Choice: c}}
		if c.Current {
			it.Badge = "●"
		}
		items = append(items, it)
	}
	return items
}

// openComplexityGroupPicker is the complexity.groupPick handler.
func (m *Model) openComplexityGroupPicker() {
	p := m.ensureComplexityPanel()
	if p == nil {
		return
	}
	choices := p.GroupChoices()
	if len(choices) == 0 {
		m.host.Notify(host.Info, "complexity: this tab does not group")
		return
	}
	m.cxGroupPick.choices = choices
	m.palette.SetSize(m.width, m.height)
	m.palette.OpenLocked(m.paletteContext(), complexityGroupPrefix)
}

// pickComplexityGroup applies a picked mode and persists it, as b does.
func (m *Model) pickComplexityGroup(msg ComplexityGroupPickedMsg) {
	p := m.complexityPanel()
	if p == nil || !p.PickGroup(msg.Choice) {
		return
	}
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// openComplexityBaselineMarkedDialog is the complexity.baselineMarked
// handler: it builds the one toggle over the filter result and raises the
// baseline's confirmation dialog naming the count.
func (m *Model) openComplexityBaselineMarkedDialog() {
	if !m.baselineEditable() {
		return
	}
	p := m.ensureComplexityPanel()
	if p == nil {
		return
	}
	if m.shell.IsOpen() {
		m.host.Notify(host.Info, "complexity: close the open dialog first")
		return
	}
	msg, ok := p.BaselineMarked()
	if !ok {
		m.host.Notify(host.Info, "complexity: no listed function is over threshold or accepted")
		return
	}
	m.cxBaseDialog, m.cxBaseMarked = true, &msg
	heading := "Complexity: accept listed functions"
	if msg.Remove {
		heading = "Complexity: drop listed functions"
	}
	m.shell.SetContent(ui.ModelContent{Heading: heading, Body: func() string { return m.complexityBaselineMarkedBody(msg) }})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// complexityBaselineMarkedBody explains what the bulk toggle writes.
func (m Model) complexityBaselineMarkedBody(msg complexitypanel.BaselineToggleMsg) string {
	n := plural(len(msg.Items), "function", "functions")
	var b strings.Builder
	if msg.Remove {
		b.WriteString("Drop " + n + " the list shows from the baseline in\n")
	} else {
		b.WriteString("Record " + n + " the list shows over threshold as accepted debt in\n")
	}
	b.WriteString(displayPath(m.cxBase.path) + "?\n\n")
	b.WriteString("One write covers all " + strconv.Itoa(len(msg.Items)) + "; the rows the filter hides are untouched.\n\n")
	verb := "accept"
	if msg.Remove {
		verb = "drop"
	}
	b.WriteString("  [enter/y] " + verb + " " + strconv.Itoa(len(msg.Items)) + "   [esc/n] cancel")
	return b.String()
}
