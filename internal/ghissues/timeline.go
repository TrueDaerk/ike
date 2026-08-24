package ghissues

// timeline.go holds the detail view's issue timeline (#2084): the fetched
// history of the open issue — comments, label and assignee changes, state
// changes — appended under the rendered body. The pane stays subprocess-free:
// the app injects the per-issue/page fetch factory, opening a detail asks for
// page one, 'L' loads the next page, 'r' refetches, and results arrive as
// forge.TimelineMsg.

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

// loadMoreTimeline fetches the next page ('L'), nil when there is none or one
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

// timelineLines renders the open issue's history under its body: comments as
// markdown blocks with an actor/age heading, everything else as compact
// single-line events, then the loading/error/pagination status row.
func (m *Model) timelineLines(pal *theme.Palette, is *forge.Issue) []string {
	if m.tlFor != is.Number {
		return nil
	}
	faint := lipgloss.NewStyle().Faint(true)
	var lines []string
	if len(m.tl) > 0 || m.tlLoading || m.tlErr != "" || m.tlPage > 0 {
		lines = append(lines, "", lipgloss.NewStyle().Foreground(pal.Border).Render(" ── activity ──"))
	}
	for _, e := range m.tl {
		if e.Kind == forge.TimelineComment {
			lines = append(lines, "", m.commentHeader(pal, &e))
			lines = append(lines, m.commentBody(e.Body)...)
			continue
		}
		lines = append(lines, m.clip(m.eventLine(pal, &e)))
	}
	switch {
	case m.tlLoading:
		lines = append(lines, "", faint.Render(" (loading activity…)"))
	case m.tlErr != "":
		lines = append(lines, "", lipgloss.NewStyle().Foreground(pal.Error).Render(m.clip(" (activity fetch failed: "+m.tlErr+" — r retries)")))
	case m.tlMore:
		lines = append(lines, "", faint.Render(" (L loads more activity)"))
	case m.tlPage > 0 && len(m.tl) == 0:
		lines = append(lines, "", faint.Render(" (no activity yet)"))
	}
	return lines
}

// commentHeader is a comment's heading line: actor, own-comment marker, age.
func (m *Model) commentHeader(pal *theme.Palette, e *forge.TimelineEntry) string {
	head := lipgloss.NewStyle().Foreground(pal.Accent).Bold(true).Render(" @" + e.Actor)
	if e.Own {
		head += lipgloss.NewStyle().Foreground(pal.Info).Render(" (you)")
	}
	if age := ui.RelTime(e.Time, m.clock()); age != "" {
		head += lipgloss.NewStyle().Faint(true).Render(" commented " + age)
	} else {
		head += lipgloss.NewStyle().Faint(true).Render(" commented")
	}
	return m.clip(head)
}

// commentBody renders one comment's markdown through the pane's glamour
// pipeline, degrading to the raw text when rendering fails.
func (m *Model) commentBody(body string) []string {
	if strings.TrimSpace(body) == "" {
		body = "*(no text)*"
	}
	out, err := m.renderMarkdown(body)
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
