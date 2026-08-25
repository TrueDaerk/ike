package ghissues

// timeline.go holds the detail view's issue timeline (#2084): the fetched
// history of the open issue — comments, label and assignee changes, state
// changes — appended under the rendered body. The pane stays subprocess-free:
// the app injects the per-issue/page fetch factory, opening a detail asks for
// page one, 'p' loads the next page (#2114 — 'L' before the key-map
// consolidation reserved the l family for labels), 'r' refetches, and results
// arrive as forge.TimelineMsg.

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/forge"
	"ike/internal/theme"
	"ike/internal/ui"
)

// SetTimeline injects the per-issue/page timeline fetch factory
// (forge.TimelineFactory at the workspace root).
func (m *Model) SetTimeline(fn func(issue, page int) tea.Cmd) { m.timeline = fn }

// PendingTimelineCmd returns the first-page fetch the open detail still
// needs, or nil when nothing is missing — the collection point every path
// into the detail view (enter, the walking chords, Reveal) runs through.
func (m *Model) PendingTimelineCmd() tea.Cmd {
	if !m.detail || m.tab != TabIssues || m.timeline == nil {
		return nil
	}
	is := m.Selected()
	if is == nil || m.tlFor == is.Number {
		return nil
	}
	m.startTimeline(is.Number)
	return m.timeline(is.Number, 1)
}

// startTimeline flips into the loading state for one issue's first page.
func (m *Model) startTimeline(number int) {
	m.tlFor = number
	m.tl = nil
	m.tlPage, m.tlMore = 0, false
	m.tlLoading, m.tlErr = true, ""
	m.tlRev++
}

// refetchTimeline drops the fetched history and re-asks for page one ('r' in
// the detail view).
func (m *Model) refetchTimeline() tea.Cmd {
	if !m.detail || m.timeline == nil {
		return nil
	}
	is := m.Selected()
	if is == nil {
		return nil
	}
	m.startTimeline(is.Number)
	return m.timeline(is.Number, 1)
}

// loadMoreTimeline fetches the next page ('p'), nil when there is none or one
// is already in flight.
func (m *Model) loadMoreTimeline() tea.Cmd {
	if m.timeline == nil || !m.tlMore || m.tlLoading || m.tlFor == 0 {
		return nil
	}
	m.tlLoading, m.tlErr = true, ""
	m.tlRev++
	return m.timeline(m.tlFor, m.tlPage+1)
}

// SetTimelineResult applies one fetched timeline page. An answer for an issue
// the pane no longer waits on is dropped; page one replaces, later pages
// append.
func (m *Model) SetTimelineResult(msg forge.TimelineMsg) {
	if msg.Issue != m.tlFor {
		return
	}
	m.tlLoading = false
	m.tlRev++
	if msg.Err != nil {
		m.tlErr = msg.Err.Error()
		return
	}
	m.tlErr = ""
	if msg.Page <= 1 {
		m.tl = msg.Entries
	} else {
		m.tl = append(m.tl, msg.Entries...)
	}
	m.tlPage, m.tlMore = msg.Page, msg.More
}

// TimelineCount reports how many timeline entries are loaded (tests).
func (m *Model) TimelineCount() int { return len(m.tl) }

// TimelineLoading reports whether a timeline fetch is in flight (tests).
func (m *Model) TimelineLoading() bool { return m.tlLoading }

// TimelineMore reports whether another timeline page follows (tests).
func (m *Model) TimelineMore() bool { return m.tlMore }

// TimelineError returns the last timeline fetch error, "" when none (tests).
func (m *Model) TimelineError() string { return m.tlErr }

// gutterCells is the width a comment block's left bar occupies ("▌ ").
const gutterCells = 2

// gutterMinWidth is the pane width below which comment blocks drop their
// gutter bar and fall back to the plain indented rendering (#2106) — on a
// very narrow pane the two columns cost more than the marker is worth.
const gutterMinWidth = 24

// timelineLines renders the open issue's history under its body: a full-width
// activity rule, comments as gutter-barred markdown blocks with an actor/age
// heading, everything else as compact single-line events, then the
// loading/error/pagination status row.
func (m *Model) timelineLines(pal *theme.Palette, is *forge.Issue) []string {
	if m.tlFor != is.Number {
		return nil
	}
	faint := lipgloss.NewStyle().Faint(true)
	var lines []string
	if len(m.tl) > 0 || m.tlLoading || m.tlErr != "" || m.tlPage > 0 {
		lines = append(lines, "", m.activityRule(pal))
	}
	afterComment := false
	for _, e := range m.tl {
		if e.Kind == forge.TimelineComment {
			lines = append(lines, "")
			lines = append(lines, m.commentBlock(pal, &e)...)
			afterComment = true
			continue
		}
		if afterComment {
			// A comment block ends flush with its gutter; one blank line
			// keeps the compact event rows from reading as its last line.
			lines = append(lines, "")
			afterComment = false
		}
		lines = append(lines, m.clip(m.eventLine(pal, &e)))
	}
	switch {
	case m.tlLoading:
		lines = append(lines, "", faint.Render(" (loading activity…)"))
	case m.tlErr != "":
		lines = append(lines, "", lipgloss.NewStyle().Foreground(pal.Error).Render(m.clip(" (activity fetch failed: "+m.tlErr+" — r retries)")))
	case m.tlMore:
		lines = append(lines, "", faint.Render(" (p loads more activity)"))
	case m.tlPage > 0 && len(m.tl) == 0:
		lines = append(lines, "", faint.Render(" (no activity yet)"))
	}
	return lines
}

// activityRule is the body/activity divider: a full-width horizontal rule in
// the palette's Secondary role with an accented "activity" label (#2106). It
// drops the label on panes too narrow to hold it and still spans the width.
func (m *Model) activityRule(pal *theme.Palette) string {
	w := m.width
	if w <= 0 {
		w = 80
	}
	rule := lipgloss.NewStyle().Foreground(pal.Secondary)
	label := "activity"
	// " ──" + " " + label + " " + fill
	fill := w - 1 - 2 - 1 - lipgloss.Width(label) - 1
	if fill < 2 {
		return rule.Render(" " + strings.Repeat("─", max(1, w-1)))
	}
	return " " + rule.Render("──") + " " +
		lipgloss.NewStyle().Foreground(pal.Accent).Bold(true).Render(label) + " " +
		rule.Render(strings.Repeat("─", fill))
}

// commentBlock renders one comment as a visually closed block: every line —
// the header and each markdown line, blank ones included — carries the same
// colored left gutter bar, so two consecutive comments cannot run into each
// other (#2106). Own comments take the Info role, others the Accent one.
func (m *Model) commentBlock(pal *theme.Palette, e *forge.TimelineEntry) []string {
	gut := m.commentGutter(pal, e.Own)
	lines := []string{m.clip(gut + m.commentHeader(pal, e))}
	for _, l := range m.commentBody(e.Body) {
		lines = append(lines, gut+l)
	}
	return lines
}

// commentGutter is the block's left bar cell, "" on panes below
// gutterMinWidth (where the plain indent is used instead).
func (m *Model) commentGutter(pal *theme.Palette, own bool) string {
	if m.width > 0 && m.width < gutterMinWidth {
		return " "
	}
	role := pal.Accent
	if own {
		role = pal.Info
	}
	return lipgloss.NewStyle().Foreground(role).Render("▌") + " "
}

// commentHeader is a comment's heading line: actor, own-comment marker, age.
// The leading gutter is added by commentBlock.
func (m *Model) commentHeader(pal *theme.Palette, e *forge.TimelineEntry) string {
	head := lipgloss.NewStyle().Foreground(pal.Accent).Bold(true).Render("@" + e.Actor)
	if e.Own {
		head += lipgloss.NewStyle().Foreground(pal.Info).Render(" (you)")
	}
	if age := ui.RelTime(e.Time, m.clock()); age != "" {
		head += lipgloss.NewStyle().Faint(true).Render(" commented " + age)
	} else {
		head += lipgloss.NewStyle().Faint(true).Render(" commented")
	}
	return head
}

// commentBody renders one comment's markdown through the pane's glamour
// pipeline — wrapped short by the gutter it is indented behind — degrading to
// the raw text when rendering fails.
func (m *Model) commentBody(body string) []string {
	if strings.TrimSpace(body) == "" {
		body = "*(no text)*"
	}
	out, err := m.renderMarkdownWrap(body, m.width-2-gutterCells)
	if err != nil {
		out = body
	}
	return strings.Split(strings.TrimRight(out, "\n"), "\n")
}

// eventLine renders one non-comment event compactly: actor, action, the
// label chip or assignee, and the relative time.
func (m *Model) eventLine(pal *theme.Palette, e *forge.TimelineEntry) string {
	faint := lipgloss.NewStyle().Faint(true)
	verb, obj := "", ""
	switch e.Kind {
	case forge.TimelineLabeled:
		verb, obj = "added", chip(forge.Label{Name: e.Body, Color: e.LabelColor})
	case forge.TimelineUnlabeled:
		verb, obj = "removed", chip(forge.Label{Name: e.Body, Color: e.LabelColor})
	case forge.TimelineClosed:
		verb = "closed this"
	case forge.TimelineReopened:
		verb = "reopened this"
	case forge.TimelineAssigned:
		verb, obj = "assigned", "@"+e.Body
		if e.Body == e.Actor {
			verb, obj = "self-assigned this", ""
		}
	case forge.TimelineUnassigned:
		verb, obj = "unassigned", "@"+e.Body
	default:
		verb = e.Kind
	}
	line := faint.Render(" · @" + e.Actor + " " + verb)
	if obj != "" {
		line += " " + obj
	}
	if age := ui.RelTime(e.Time, m.clock()); age != "" {
		line += faint.Render(" · " + age)
	}
	return line
}
