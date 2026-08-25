package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
	"ike/internal/pane"
)

// issueEvent builds an IssueOpened event for the tests.
func issueEvent(n int, title string) forge.Event {
	return forge.Event{
		Kind:   forge.IssueOpened,
		Number: n,
		Title:  title,
		Author: "octocat",
		Labels: []forge.Label{{Name: "bug", Color: "d73a4a"}},
		URL:    "https://example.test/issues/" + title,
	}
}

// feedEvents runs one poll round's events through Update.
func feedEvents(m Model, events ...forge.Event) Model {
	tm, _ := m.Update(forge.EventsMsg{Events: events})
	return tm.(Model)
}

func TestForgeIssueOpenedRaisesDialog(t *testing.T) {
	m := feedEvents(newSized(), issueEvent(12, "Fix the thing"))
	if !m.forgeDialogOpen() {
		t.Fatal("an IssueOpened event must raise the dialog")
	}
	body := m.forgeDialogBody()
	for _, want := range []string{"#12", "Fix the thing", "octocat", "bug"} {
		if !strings.Contains(body, want) {
			t.Fatalf("dialog body %q misses %q", body, want)
		}
	}
	if got := m.forgeDialogTitle(); got != "Forge event" {
		t.Fatalf("title = %q want the single-event heading", got)
	}
}

func TestForgeEventsCollapseIntoOneDialogWithCount(t *testing.T) {
	m := feedEvents(newSized(), issueEvent(1, "one"))
	m = feedEvents(m, issueEvent(2, "two"), issueEvent(3, "three"))
	if len(m.forgeQueue) != 3 {
		t.Fatalf("queue = %d want 3 collapsed events", len(m.forgeQueue))
	}
	if !m.forgeDialogOpen() {
		t.Fatal("the one dialog must stay open")
	}
	if got := m.forgeDialogTitle(); !strings.Contains(got, "(3)") {
		t.Fatalf("title = %q want the pending count", got)
	}
	// No stacking: the floating shell hosts exactly the one dialog.
	if !m.shell.IsOpen() {
		t.Fatal("dialog must live in the single floating shell")
	}
}

func TestForgeDialogDismissAndDismissAll(t *testing.T) {
	m := feedEvents(newSized(), issueEvent(1, "one"), issueEvent(2, "two"))
	tm, _ := m.updateForgeDialog(tea.KeyPressMsg{Text: "d", Code: 'd'})
	m = tm.(Model)
	if len(m.forgeQueue) != 1 || !m.forgeDialogOpen() {
		t.Fatalf("one dismissal leaves queue=%d open=%v; want 1/true", len(m.forgeQueue), m.forgeDialogOpen())
	}
	m = feedEvents(m, issueEvent(3, "three"))
	tm, _ = m.updateForgeDialog(tea.KeyPressMsg{Text: "a", Code: 'a'})
	m = tm.(Model)
	if len(m.forgeQueue) != 0 || m.forgeDialogOpen() {
		t.Fatalf("dismiss all leaves queue=%d open=%v; want 0/false", len(m.forgeQueue), m.forgeDialogOpen())
	}
}

func TestForgeDialogEscDismissesLastAndCloses(t *testing.T) {
	m := feedEvents(newSized(), issueEvent(7, "solo"))
	tm, _ := m.updateForgeDialog(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if m.forgeDialogOpen() || len(m.forgeQueue) != 0 {
		t.Fatal("esc on the last event must dismiss it and close the dialog")
	}
}

func TestForgeDialogOpenAction(t *testing.T) {
	m := feedEvents(newSized(), issueEvent(42, "open me"))
	tm, _ := m.updateForgeDialog(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tm.(Model)
	if m.forgeDialogOpen() {
		t.Fatal("opening the issue closes the dialog")
	}
	if !m.activeWS().Panes.Has(pane.IssuesKey) {
		t.Fatal("the issues tool window must be open after the open action")
	}
	if m.forgeReveal != 42 {
		t.Fatalf("forgeReveal = %d want the announced issue number", m.forgeReveal)
	}
	// The listing arriving later lands the cursor on that issue's detail.
	tm, _ = m.Update(forge.IssuesMsg{Issues: []forge.Issue{{Number: 41, Title: "other"}, {Number: 42, Title: "open me"}}})
	m = tm.(Model)
	if m.forgeReveal != 0 {
		t.Fatal("a landed fetch must consume the pending reveal")
	}
	p := m.issuesPanel()
	if p == nil || !p.DetailOpen() {
		t.Fatal("reveal must open the issue's detail view")
	}
	if is := p.Selected(); is == nil || is.Number != 42 {
		t.Fatalf("selected issue = %v want #42", is)
	}
}

func TestForgeTypingGuardDefersDialogToBadge(t *testing.T) {
	m := newSized()
	m.lastInputAt = time.Now()
	m = feedEvents(m, issueEvent(5, "later"))
	if m.forgeDialogOpen() {
		t.Fatal("a dialog must not interrupt active typing")
	}
	if got := m.forgeBadgeSegment(); got != "● 1 new issue" {
		t.Fatalf("badge = %q want the deferred-event badge", got)
	}
	m = feedEvents(m, issueEvent(6, "later too"))
	if got := m.forgeBadgeSegment(); got != "● 2 new issues" {
		t.Fatalf("badge = %q want the grown count", got)
	}
	// The badge is persistent: unrelated Update passes never clear it.
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if m = tm.(Model); m.forgeBadgeSegment() == "" {
		t.Fatal("the badge must stay until the events are viewed")
	}
}

func TestForgeGuardExpiresAfterTypingWindow(t *testing.T) {
	m := newSized()
	m.lastInputAt = time.Now().Add(-2 * forgeTypingWindow)
	m = feedEvents(m, issueEvent(8, "now fine"))
	if !m.forgeDialogOpen() {
		t.Fatal("a stale typing stamp must not hold the dialog back")
	}
}

func TestForgeBadgeClearsOnIssuesView(t *testing.T) {
	m := newSized()
	m.lastInputAt = time.Now()
	m = feedEvents(m, issueEvent(9, "deferred"))
	if m.forgeBadgeSegment() == "" {
		t.Fatal("precondition: badge is set")
	}
	tm, _ := m.Update(IssuesToggleMsg{})
	if m = tm.(Model); m.forgeBadgeSegment() != "" {
		t.Fatal("opening the issues window clears the badge")
	}
}

func TestForgeBadgeClearsWhenDialogOpens(t *testing.T) {
	m := newSized()
	m.lastInputAt = time.Now()
	m = feedEvents(m, issueEvent(10, "deferred"))
	m.lastInputAt = time.Time{} // the user stopped typing
	m = feedEvents(m, issueEvent(11, "fresh"))
	if !m.forgeDialogOpen() {
		t.Fatal("the dialog must open once the guard is clear")
	}
	if m.forgeBadgeSegment() != "" {
		t.Fatal("the dialog viewing the events clears the badge")
	}
	if len(m.forgeQueue) != 2 {
		t.Fatalf("queue = %d want the deferred event collapsed in", len(m.forgeQueue))
	}
}

func TestForgeStylesPerEventType(t *testing.T) {
	m := newSized()
	// pr_merged defaults to a toast: no dialog, no badge, but a toast.
	m = feedEvents(m, forge.Event{Kind: forge.PRMerged, Number: 3, Title: "merged"})
	if m.forgeDialogOpen() || m.forgeBadgeSegment() != "" {
		t.Fatal("a toast-style event must not raise the dialog or the badge")
	}
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30}) // drains the toast
	if m = tm.(Model); len(m.toasts) != 1 {
		t.Fatalf("toasts = %d want the toast-style event", len(m.toasts))
	}
	// pr_checks_failing defaults to the badge.
	m = feedEvents(m, forge.Event{Kind: forge.PRChecksFailing, Number: 4, Title: "ci red"})
	if got := m.forgeBadgeSegment(); got != "● 1 forge event" {
		t.Fatalf("badge = %q want the badge-style event", got)
	}
	if m.forgeDialogOpen() {
		t.Fatal("a badge-style event must not raise the dialog")
	}
}

func TestForgeStyleOffStaysInHistoryOnly(t *testing.T) {
	m := newSized()
	if s := parseForgeStyle("off"); s != forgeStyleOff {
		t.Fatalf("parseForgeStyle(off) = %v", s)
	}
	m = feedEvents(m, issueEvent(20, "recorded"))
	if len(m.history) == 0 || !strings.Contains(m.history[0].text, "#20") {
		t.Fatalf("history = %v want the event recorded", m.history)
	}
}

func TestForgeEventsLandInHistory(t *testing.T) {
	m := newSized()
	before := m.notifUnseen
	m = feedEvents(m, issueEvent(1, "one"), forge.Event{Kind: forge.PRMerged, Number: 2, Title: "two"})
	if len(m.history) < 2 {
		t.Fatalf("history = %d entries want both events", len(m.history))
	}
	if m.notifUnseen != before+2 {
		t.Fatalf("notifUnseen = %d want %d", m.notifUnseen, before+2)
	}
	if !strings.Contains(m.history[0].text, "pull request merged #2") {
		t.Fatalf("newest history entry = %q", m.history[0].text)
	}
}

func TestForgeDialogOwnsKeyboard(t *testing.T) {
	m := feedEvents(newSized(), issueEvent(1, "one"), issueEvent(2, "two"))
	// j moves within the queue instead of reaching the editor.
	tm, _ := m.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	m = tm.(Model)
	if m.forgeCursor != 1 {
		t.Fatalf("forgeCursor = %d want the dialog to consume j", m.forgeCursor)
	}
	if !m.forgeDialogOpen() {
		t.Fatal("navigation keeps the dialog open")
	}
}
