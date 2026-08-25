package ghissues

// timeline_test.go covers the detail view's issue timeline (#2084): the lazy
// fetch on open, the rendering of comments and compact events, the loading /
// error / pagination states, and the refetch paths.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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
	if !m.TimelineMore() || !strings.Contains(m.View(), "L loads more") {
		t.Fatal("the load-more row must show while more pages follow")
	}
	cmd := m.Update(tea.KeyPressMsg{Code: 'l', Text: "L", Mod: tea.ModShift})
	if cmd == nil || stub.page != 2 || !m.TimelineLoading() {
		t.Fatalf("L must fetch page 2, got page=%d", stub.page)
	}
	m.SetTimelineResult(forge.TimelineMsg{Issue: 2, Page: 2, Entries: []forge.TimelineEntry{
		{Kind: forge.TimelineReopened, Actor: "bo", Time: fixedNow},
	}})
	if m.TimelineCount() != 2 || m.TimelineMore() {
		t.Fatalf("page 2 must append, count = %d more = %v", m.TimelineCount(), m.TimelineMore())
	}
	if strings.Contains(m.View(), "L loads more") {
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
