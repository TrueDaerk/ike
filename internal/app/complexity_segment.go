package app

import (
	"image/color"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/complexitypanel"
	"ike/internal/config"
)

// complexity_segment.go is the opt-in "Δcx" status segment (Roadmap 0550,
// #2921): the summed CCN delta of the working tree against the Changes
// tab's base revision. It only reads the diff the Changes machinery stores;
// complexityDiffWanted keeps that machinery armed while the segment is on.

// complexitySegment renders "Δcx +4" / "Δcx -2" / "Δcx ±0"; hidden while
// complexity.status_segment or complexity.enabled is off, outside git
// repositories, before the first diff lands and while the diff is empty.
func (m Model) complexitySegment() string {
	c := config.Get()
	if c == nil || !c.Complexity.Enabled || !c.Complexity.StatusSegment || m.vcs.snap == nil {
		return ""
	}
	ch := m.ComplexityChanges()
	if ch.NotGit || !ch.Ready || len(ch.Diff.Files) == 0 {
		return ""
	}
	d := ch.Diff.DeltaCCN()
	switch {
	case d > 0:
		return "Δcx +" + strconv.Itoa(d)
	case d < 0:
		return "Δcx " + strconv.Itoa(d)
	}
	return "Δcx ±0"
}

// complexitySegmentColor is the verdict colour of the segment: warning when
// the summed CCN rose, success when it fell, muted when it held.
func (m Model) complexitySegmentColor(seg string) color.Color {
	switch {
	case strings.HasPrefix(seg, "Δcx +"):
		return m.pal().Warning
	case strings.HasPrefix(seg, "Δcx -"):
		return m.pal().Success
	}
	return m.pal().InlayHint
}

// tintComplexitySegment recolours the segment's text inside the finished
// status line. Only the foreground switches (and switches back to the bar's
// own), so the panel background and the mode badge stay intact; a segment
// dropped or clipped by the overflow shrinker is simply not found.
func (m Model) tintComplexitySegment(line string) string {
	seg := m.complexitySegment()
	if seg == "" {
		return line
	}
	i := strings.LastIndex(line, seg)
	if i < 0 {
		return line
	}
	on := ansi.Style{}.ForegroundColor(m.complexitySegmentColor(seg)).String()
	off := ansi.Style{}.ForegroundColor(m.pal().Foreground).String()
	return line[:i] + on + seg + off + line[i+len(seg):]
}

// openComplexityChanges is the segment's click: toggle the Complexity
// window and show its Changes tab.
func (m *Model) openComplexityChanges() tea.Cmd {
	cmd := m.toggleComplexityPanel()
	if p := m.complexityPanel(); p != nil {
		p.SetTab(complexitypanel.TabChanges)
	}
	return cmd
}
