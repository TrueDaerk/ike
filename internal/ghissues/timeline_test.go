package ghissues

// timeline_test.go covers the detail view's issue timeline (#2084): the lazy
// fetch on open, the rendering of comments and compact events, the loading /
// error / pagination states, and the refetch paths.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/forge"
)

// timelineStub injects a fetch factory that records what was asked for and
// resolves to a marker message.
type timelineStub struct {
	issue, page int
	calls       int
}

func (s *timelineStub) inject(m *Model) {
	m.SetTimeline(func(issue, page int) tea.Cmd {
		s.issue, s.page = issue, page
		s.calls++
		return func() tea.Msg { return forge.TimelineMsg{Issue: issue, Page: page} }
	})
}

func TestDetailFetchesTimelineOnOpen(t *testing.T) {
	m := filled(t)
	m.SetSize(90, 40)
	stub := &timelineStub{}
	stub.inject(m)
	cmd := m.Update(key("enter"))
	if !m.DetailOpen() {
		t.Fatal("enter must open the detail")
	}
	if cmd == nil || stub.calls != 1 || stub.issue != 2 || stub.page != 1 {
		t.Fatalf("open must fetch page one of issue 2, got calls=%d issue=%d page=%d", stub.calls, stub.issue, stub.page)
	}
	if !m.TimelineLoading() || !strings.Contains(m.View(), "loading activity") {
		t.Fatal("the loading state must show while the fetch is in flight")
	}
	// Re-opening the same issue must not refetch.
	press(m, "esc")
	if cmd := m.Update(key("enter")); cmd != nil || stub.calls != 1 {
		t.Fatal("an already-fetched issue must not refetch on reopen")
	}
}

func TestTimelineRendersCommentsAndEvents(t *testing.T) {
	m := filled(t)
	m.SetSize(90, 40)
	stub := &timelineStub{}
	stub.inject(m)
	m.Update(key("enter"))
	m.SetTimelineResult(forge.TimelineMsg{Issue: 2, Page: 1, Entries: []forge.TimelineEntry{
		{Kind: forge.TimelineComment, Actor: "ada", Body: "Please add *tables* too.", ID: "77", Time: fixedNow.Add(-2 * 3600e9)},
		{Kind: forge.TimelineComment, Actor: "me", Body: "On it.", ID: "78", Own: true, Time: fixedNow.Add(-3600e9)},
		{Kind: forge.TimelineLabeled, Actor: "bo", Body: "feature", LabelColor: "a2eeef", Time: fixedNow.Add(-1800e9)},
		{Kind: forge.TimelineClosed, Actor: "bo", Time: fixedNow.Add(-900e9)},
		{Kind: forge.TimelineAssigned, Actor: "bo", Body: "cy", Time: fixedNow.Add(-600e9)},
	}})
	view := m.View()
	for _, want := range []string{"activity", "@ada", "tables", "(you)", "added", "feature", "closed this", "assigned"} {
		if !strings.Contains(view, want) {
			t.Fatalf("detail view must contain %q:\n%s", want, view)
		}
	}
	if m.TimelineCount() != 5 || m.TimelineLoading() {
		t.Fatalf("count = %d loading = %v", m.TimelineCount(), m.TimelineLoading())
	}
}

func TestTimelinePagination(t *testing.T) {
	m := filled(t)
	m.SetSize(90, 40)
	stub := &timelineStub{}
	stub.inject(m)
	m.Update(key("enter"))
	m.SetTimelineResult(forge.TimelineMsg{Issue: 2, Page: 1, More: true, Entries: []forge.TimelineEntry{
		{Kind: forge.TimelineClosed, Actor: "bo", Time: fixedNow},
	}})
	if !m.TimelineMore() || !strings.Contains(m.View(), "p loads more") {
		t.Fatal("the load-more row must show while more pages follow")
	}
	cmd := m.Update(key("p"))
	if cmd == nil || stub.page != 2 || !m.TimelineLoading() {
		t.Fatalf("p must fetch page 2, got page=%d", stub.page)
	}
	m.SetTimelineResult(forge.TimelineMsg{Issue: 2, Page: 2, Entries: []forge.TimelineEntry{
		{Kind: forge.TimelineReopened, Actor: "bo", Time: fixedNow},
	}})
	if m.TimelineCount() != 2 || m.TimelineMore() {
		t.Fatalf("page 2 must append, count = %d more = %v", m.TimelineCount(), m.TimelineMore())
	}
	if strings.Contains(m.View(), "p loads more") {
		t.Fatal("the load-more row must disappear on the last page")
	}
}

func TestTimelineErrorAndRefetch(t *testing.T) {
	m := filled(t)
	m.SetSize(90, 40)
	stub := &timelineStub{}
	stub.inject(m)
	m.Update(key("enter"))
	m.SetTimelineResult(forge.TimelineMsg{Issue: 2, Page: 1, Err: errFake("offline")})
	if m.TimelineError() == "" || !strings.Contains(m.View(), "activity fetch failed") {
		t.Fatal("the error state must render")
	}
	cmd := m.Update(key("r"))
	if cmd == nil || stub.calls != 2 || stub.page != 1 || !m.TimelineLoading() {
		t.Fatalf("r must refetch page one, calls = %d", stub.calls)
	}
}

func TestTimelineWalkFetchesNextIssue(t *testing.T) {
	m := filled(t)
	m.SetSize(90, 40)
	stub := &timelineStub{}
	stub.inject(m)
	m.Update(key("enter"))
	m.SetTimelineResult(forge.TimelineMsg{Issue: 2, Page: 1})
	cmd := m.Update(key("ctrl+j"))
	if cmd == nil || stub.calls != 2 || stub.issue == 2 {
		t.Fatalf("walking must fetch the new issue's timeline, calls=%d issue=%d", stub.calls, stub.issue)
	}
}

func TestTimelineStaleResultDropped(t *testing.T) {
	m := filled(t)
	m.SetSize(90, 40)
	stub := &timelineStub{}
	stub.inject(m)
	m.Update(key("enter"))
	m.SetTimelineResult(forge.TimelineMsg{Issue: 99, Page: 1, Entries: []forge.TimelineEntry{
		{Kind: forge.TimelineClosed, Actor: "x", Time: fixedNow},
	}})
	if m.TimelineCount() != 0 || !m.TimelineLoading() {
		t.Fatal("an answer for another issue must be dropped")
	}
}

// timelineRendered opens issue 2's detail, applies the entries and returns the
// timeline's rendered lines at the given pane width (#2106).
func timelineRendered(t *testing.T, w int, entries []forge.TimelineEntry) []string {
	t.Helper()
	m := filled(t)
	m.SetSize(w, 40)
	(&timelineStub{}).inject(m)
	m.Update(key("enter"))
	m.SetTimelineResult(forge.TimelineMsg{Issue: 2, Page: 1, Entries: entries})
	return m.timelineLines(m.theme(), m.Selected())
}

// twoComments is the recurring fixture: two consecutive comments (the second
// one own) followed by a compact event.
func twoComments() []forge.TimelineEntry {
	return []forge.TimelineEntry{
		{Kind: forge.TimelineComment, Actor: "ada", Body: "Please add *tables* too.", ID: "77", Time: fixedNow.Add(-2 * 3600e9)},
		{Kind: forge.TimelineComment, Actor: "me", Body: "On it.", ID: "78", Own: true, Time: fixedNow.Add(-3600e9)},
		{Kind: forge.TimelineClosed, Actor: "bo", Time: fixedNow.Add(-900e9)},
	}
}

func TestTimelineDividerSpansPaneWidth(t *testing.T) {
	const w = 90
	lines := timelineRendered(t, w, twoComments())
	var rule string
	for _, l := range lines {
		if strings.Contains(ansi.Strip(l), "activity") && strings.Contains(l, "─") {
			rule = l
			break
		}
	}
	if rule == "" {
		t.Fatalf("no activity divider in:\n%s", strings.Join(lines, "\n"))
	}
	if got := lipgloss.Width(rule); got != w {
		t.Fatalf("divider width = %d, want the full pane width %d: %q", got, w, ansi.Strip(rule))
	}
}

func TestTimelineDividerDegradesOnNarrowPanes(t *testing.T) {
	const w = 12
	lines := timelineRendered(t, w, twoComments())
	rule := ""
	for _, l := range lines {
		if strings.Contains(l, "─") {
			rule = l
			break
		}
	}
	if rule == "" {
		t.Fatal("a narrow pane must still draw the divider")
	}
	if got := lipgloss.Width(rule); got > w || got < w-1 {
		t.Fatalf("narrow divider width = %d, want ~%d: %q", got, w, ansi.Strip(rule))
	}
	if strings.Contains(ansi.Strip(rule), "activity") {
		t.Fatalf("the narrow divider must drop its label: %q", ansi.Strip(rule))
	}
}

func TestTimelineCommentBlocksCarryGutter(t *testing.T) {
	lines := timelineRendered(t, 90, twoComments())
	// Every line of a comment block — header, body, the blank ones inside —
	// carries the bar, so the two comments read as separate blocks.
	var heads []int
	for i, l := range lines {
		s := ansi.Strip(l)
		if strings.HasPrefix(s, "▌ @") && strings.Contains(s, "commented") {
			heads = append(heads, i)
		}
	}
	if len(heads) != 2 {
		t.Fatalf("want 2 gutter-barred comment headers, got %d:\n%s", len(heads), ansi.Strip(strings.Join(lines, "\n")))
	}
	// The first block runs unbroken from its header up to the second header.
	for i := heads[0]; i < heads[1]-1; i++ {
		if !strings.HasPrefix(ansi.Strip(lines[i]), "▌") {
			t.Fatalf("comment line %d lost its gutter: %q", i, ansi.Strip(lines[i]))
		}
	}
	if !strings.Contains(ansi.Strip(lines[heads[1]]), "(you)") {
		t.Fatal("the own comment must keep its (you) marker")
	}
	// The event stays a compact, gutter-free line, one blank line clear of the
	// comment block above it.
	ev, blank := -1, false
	for i, l := range lines {
		if strings.Contains(ansi.Strip(l), "closed this") {
			ev, blank = i, strings.TrimSpace(ansi.Strip(lines[i-1])) == ""
			break
		}
	}
	if ev < 0 || !blank {
		t.Fatalf("the event row must follow a blank line (index %d):\n%s", ev, ansi.Strip(strings.Join(lines, "\n")))
	}
	if strings.Contains(ansi.Strip(lines[ev]), "▌") {
		t.Fatalf("event rows must stay gutter-free: %q", ansi.Strip(lines[ev]))
	}
}

func TestTimelineCommentGutterDropsOnNarrowPanes(t *testing.T) {
	lines := timelineRendered(t, 16, twoComments())
	for i, l := range lines {
		if strings.Contains(ansi.Strip(l), "▌") {
			t.Fatalf("line %d must drop the gutter on a narrow pane: %q", i, ansi.Strip(l))
		}
	}
	if !strings.Contains(ansi.Strip(strings.Join(lines, "\n")), "@ada") {
		t.Fatal("the narrow rendering must still show the comment author")
	}
}

func TestTimelineOwnCommentGutterUsesItsOwnRole(t *testing.T) {
	m := filled(t)
	m.SetSize(90, 40)
	pal := m.theme()
	if m.commentGutter(pal, true) == m.commentGutter(pal, false) {
		t.Fatal("own comments must take a distinct gutter role")
	}
}

func TestTimelineEmptyState(t *testing.T) {
	m := filled(t)
	m.SetSize(90, 40)
	stub := &timelineStub{}
	stub.inject(m)
	m.Update(key("enter"))
	m.SetTimelineResult(forge.TimelineMsg{Issue: 2, Page: 1})
	if !strings.Contains(m.View(), "no activity yet") {
		t.Fatal("an empty finished timeline must say so")
	}
}

// renderDetail draws the detail once so the scroll bounds know how many lines
// it has — the auto-pagination reads them (#2113).
func renderDetail(m *Model) { _ = m.View() }

func TestTimelineAutoLoadsOnScrollToEnd(t *testing.T) {
	m := filled(t)
	m.SetSize(90, 40)
	stub := &timelineStub{}
	stub.inject(m)
	m.Update(key("enter"))
	m.SetTimelineResult(forge.TimelineMsg{Issue: 2, Page: 1, More: true, Entries: []forge.TimelineEntry{
		{Kind: forge.TimelineClosed, Actor: "bo", Time: fixedNow},
	}})
	renderDetail(m)
	// Scrolling up, away from the end, must not fetch anything.
	if cmd := m.Update(key("k")); cmd != nil || stub.calls != 1 {
		t.Fatalf("scrolling up must not paginate, calls = %d", stub.calls)
	}
	if cmd := m.Update(key("G")); cmd == nil || stub.calls != 2 || stub.page != 2 || !m.TimelineLoading() {
		t.Fatalf("reaching the end must fetch page 2 without a keypress, calls=%d page=%d", stub.calls, stub.page)
	}
	// One page per scroll to the end: while the fetch is in flight another
	// scroll must not pile a second request on top.
	if cmd := m.Update(key("j")); cmd != nil || stub.calls != 2 {
		t.Fatalf("an in-flight fetch must not be duplicated, calls = %d", stub.calls)
	}
	m.SetTimelineResult(forge.TimelineMsg{Issue: 2, Page: 2, Entries: []forge.TimelineEntry{
		{Kind: forge.TimelineReopened, Actor: "bo", Time: fixedNow},
	}})
	renderDetail(m)
	if cmd := m.Update(key("G")); cmd != nil || stub.calls != 2 {
		t.Fatalf("the last page must not trigger another fetch, calls = %d", stub.calls)
	}
}

func TestTimelineWheelAutoLoadsAtEnd(t *testing.T) {
	m := filled(t)
	m.SetSize(90, 40)
	stub := &timelineStub{}
	stub.inject(m)
	m.Update(key("enter"))
	m.SetTimelineResult(forge.TimelineMsg{Issue: 2, Page: 1, More: true, Entries: []forge.TimelineEntry{
		{Kind: forge.TimelineClosed, Actor: "bo", Time: fixedNow},
	}})
	renderDetail(m)
	if cmd := m.Wheel(-3); cmd != nil || stub.calls != 1 {
		t.Fatalf("wheeling up must not paginate, calls = %d", stub.calls)
	}
	if cmd := m.Wheel(200); cmd == nil || stub.calls != 2 || stub.page != 2 {
		t.Fatalf("wheeling to the end must fetch page 2, calls=%d page=%d", stub.calls, stub.page)
	}
}

func TestTimelineRefetchKeepsLoadedDepth(t *testing.T) {
	m := filled(t)
	m.SetSize(90, 40)
	stub := &timelineStub{}
	stub.inject(m)
	m.Update(key("enter"))
	m.SetTimelineResult(forge.TimelineMsg{Issue: 2, Page: 1, More: true, Entries: []forge.TimelineEntry{
		{Kind: forge.TimelineClosed, Actor: "bo", Time: fixedNow},
	}})
	m.Update(key("p"))
	m.SetTimelineResult(forge.TimelineMsg{Issue: 2, Page: 2, More: true, Entries: []forge.TimelineEntry{
		{Kind: forge.TimelineReopened, Actor: "bo", Time: fixedNow},
	}})
	if m.TimelineCount() != 2 {
		t.Fatalf("two pages must be loaded, count = %d", m.TimelineCount())
	}
	// 'r' restarts at page one but owes the second page again.
	if cmd := m.Update(key("r")); cmd == nil || stub.page != 1 {
		t.Fatalf("r must refetch page one, page = %d", stub.page)
	}
	cmd := m.SetTimelineResult(forge.TimelineMsg{Issue: 2, Page: 1, More: true, Entries: []forge.TimelineEntry{
		{Kind: forge.TimelineClosed, Actor: "bo", Time: fixedNow},
	}})
	if cmd == nil || stub.page != 2 || !m.TimelineLoading() {
		t.Fatalf("the refetch must walk back to page 2, page = %d loading = %v", stub.page, m.TimelineLoading())
	}
	if next := m.SetTimelineResult(forge.TimelineMsg{Issue: 2, Page: 2, More: true, Entries: []forge.TimelineEntry{
		{Kind: forge.TimelineReopened, Actor: "bo", Time: fixedNow},
	}}); next != nil {
		t.Fatal("the restored depth must stop the chain, not keep fetching")
	}
	if m.TimelineCount() != 2 || m.TimelineLoading() {
		t.Fatalf("r must keep the loaded depth, count = %d loading = %v", m.TimelineCount(), m.TimelineLoading())
	}
}

func TestTimelineRefetchStopsOnError(t *testing.T) {
	m := filled(t)
	m.SetSize(90, 40)
	stub := &timelineStub{}
	stub.inject(m)
	m.Update(key("enter"))
	m.SetTimelineResult(forge.TimelineMsg{Issue: 2, Page: 1, More: true})
	m.Update(key("p"))
	m.SetTimelineResult(forge.TimelineMsg{Issue: 2, Page: 2, More: true})
	m.Update(key("r"))
	calls := stub.calls
	if cmd := m.SetTimelineResult(forge.TimelineMsg{Issue: 2, Page: 1, Err: errFake("offline")}); cmd != nil {
		t.Fatal("a failed page must not chain the depth restore")
	}
	if stub.calls != calls || m.TimelineError() == "" || m.TimelineLoading() {
		t.Fatalf("the error state must settle, calls = %d err = %q", stub.calls, m.TimelineError())
	}
}
