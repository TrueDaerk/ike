package ghissues

// textedit_test.go covers the ownership/permission gate and the text-edit picker
// (#2087): which texts the detail view offers to edit, that nothing is
// offered without a capability answer, and that choosing one emits the
// request the app opens a markdown buffer from.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
)

// editableWith opens the detail of issue 2 (author "bo") with the given
// capabilities applied, a foreign comment always loaded, and — when own is
// set — one comment of the authenticated user's own.
func editableWith(t *testing.T, caps forge.Capabilities, probed, own bool) *Model {
	t.Helper()
	m := filled(t)
	m.SetSize(90, 40)
	stub := &timelineStub{}
	stub.inject(m)
	if probed {
		m.SetRepoMeta(forge.RepoMetaMsg{Caps: caps})
	}
	m.Update(key("enter"))
	entries := []forge.TimelineEntry{
		{Kind: forge.TimelineComment, Actor: "ada", Body: "not mine", ID: "1"},
	}
	if own {
		entries = append(entries, forge.TimelineEntry{
			Kind: forge.TimelineComment, Actor: caps.Login,
			Body: "  mine, first line\nand more", ID: "2", Own: true,
		})
	}
	entries = append(entries, forge.TimelineEntry{Kind: forge.TimelineLabeled, Actor: "ada", Body: "bug"})
	m.SetTimelineResult(forge.TimelineMsg{Issue: 2, Page: 1, Entries: entries})
	return m
}

// editable is editableWith plus an own comment, the common case.
func editable(t *testing.T, caps forge.Capabilities, probed bool) *Model {
	t.Helper()
	return editableWith(t, caps, probed, true)
}

// runMsg resolves a command to the message it produced.
func runMsg(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	return cmd()
}

func TestEditActionsHiddenWithoutACapabilityAnswer(t *testing.T) {
	m := editable(t, forge.Capabilities{}, false)
	if len(m.textTargets()) > 0 || m.canComment() {
		t.Fatal("nothing may be editable before the probe answers")
	}
	if labels := m.EditEntryLabels(); len(labels) != 0 {
		t.Fatalf("targets = %v", labels)
	}
	for _, a := range m.Actions() {
		if a[0] == "e" || a[0] == "n" {
			t.Fatalf("unpermitted action offered: %v", a)
		}
	}
	// The keys stay inert too, not just the menu.
	if cmd := m.Update(key("e")); cmd != nil {
		t.Fatal("'e' must do nothing without a capability answer")
	}
	if cmd := m.Update(key("n")); cmd != nil {
		t.Fatal("'n' must do nothing without a capability answer")
	}
}

func TestOwnCommentIsEditableForeignIsNot(t *testing.T) {
	// A login with no write access on someone else's issue: only the own
	// comment is offered — not the body, not the foreign comment.
	m := editable(t, forge.Capabilities{Login: "me"}, true)
	labels := m.EditEntryLabels()
	if len(labels) != 1 {
		t.Fatalf("targets = %v, want only the own comment", labels)
	}
	if !strings.HasPrefix(labels[0], "Your comment — ") {
		t.Fatalf("label = %q", labels[0])
	}
	if !strings.Contains(labels[0], "mine, first line") {
		t.Fatalf("the preview must show the comment's first line: %q", labels[0])
	}
	if strings.Contains(labels[0], "and more") {
		t.Fatalf("the preview must stop after the first line: %q", labels[0])
	}
}

func TestIssueBodyEditableForItsAuthor(t *testing.T) {
	m := editableWith(t, forge.Capabilities{Login: "bo"}, true, false)
	if !m.canEditBody(m.Selected()) {
		t.Fatal("issue 2 was opened by bo, so bo may edit its body")
	}
	other := editableWith(t, forge.Capabilities{Login: "someone-else"}, true, false)
	if other.canEditBody(other.Selected()) {
		t.Fatal("a foreign issue body must not be editable without write access")
	}
}

func TestIssueBodyEditableWithWriteAccess(t *testing.T) {
	m := editableWith(t, forge.Capabilities{Login: "maintainer", Push: true}, true, false)
	if !m.canEditBody(m.Selected()) {
		t.Fatal("write access must allow editing a foreign issue body")
	}
	labels := m.EditEntryLabels()
	if len(labels) != 1 || labels[0] != "Issue body" {
		// The maintainer authored no comment here, so the body is the only
		// target — write access does not make foreign comments editable.
		t.Fatalf("targets = %v", labels)
	}
}

func TestSingleTargetEditsWithoutThePicker(t *testing.T) {
	m := editable(t, forge.Capabilities{Login: "me"}, true)
	// Only the own comment qualifies, so 'e' opens it straight away.
	msg := runMsg(t, m.Update(key("e")))
	req, ok := msg.(EditTextRequestMsg)
	if !ok {
		t.Fatalf("msg = %T", msg)
	}
	if m.EditPickerOpen() {
		t.Fatal("a single target must not raise the picker")
	}
	if req.Target.Kind != forge.TextComment || req.Target.ID != "2" || req.Target.Issue != 2 {
		t.Fatalf("target = %+v", req.Target)
	}
	if req.Base != "  mine, first line\nand more" {
		t.Fatalf("base = %q — the buffer must be prefilled with the comment", req.Base)
	}
}

func TestSeveralTargetsRaiseThePicker(t *testing.T) {
	// bo authored the issue *and* a comment: two targets, so 'e' asks which.
	m := editable(t, forge.Capabilities{Login: "bo", Push: true}, true)
	if cmd := m.Update(key("e")); cmd != nil {
		t.Fatal("the picker must open instead of editing immediately")
	}
	if !m.EditPickerOpen() {
		t.Fatal("'e' with several targets must raise the picker")
	}
	if labels := m.EditEntryLabels(); len(labels) != 2 || labels[0] != "Issue body" {
		t.Fatalf("targets = %v", labels)
	}
	if !strings.Contains(m.View(), "Edit what?") {
		t.Fatal("the picker must render its heading")
	}
	// Move to the comment and open it.
	press(m, "down")
	msg := runMsg(t, m.Update(key("enter")))
	req, ok := msg.(EditTextRequestMsg)
	if !ok {
		t.Fatalf("msg = %T", msg)
	}
	if req.Target.Kind != forge.TextComment || req.Target.ID != "2" {
		t.Fatalf("target = %+v", req.Target)
	}
	if m.EditPickerOpen() {
		t.Fatal("the picker must close after a choice")
	}
}

func TestEditPickerEscapeOpensNothing(t *testing.T) {
	m := editable(t, forge.Capabilities{Login: "bo", Push: true}, true)
	m.Update(key("e"))
	if cmd := m.Update(key("esc")); cmd != nil {
		t.Fatal("esc must not open a buffer")
	}
	if m.EditPickerOpen() {
		t.Fatal("esc must close the picker")
	}
}

func TestNewCommentNeedsOnlyALogin(t *testing.T) {
	m := editableWith(t, forge.Capabilities{Login: "stranger"}, true, false)
	if len(m.textTargets()) > 0 {
		t.Fatal("a stranger may edit nothing here")
	}
	msg := runMsg(t, m.Update(key("n")))
	req, ok := msg.(EditTextRequestMsg)
	if !ok {
		t.Fatalf("msg = %T", msg)
	}
	if req.Target.Kind != forge.TextNewComment || req.Target.Issue != 2 {
		t.Fatalf("target = %+v", req.Target)
	}
	if req.Base != "" {
		t.Fatalf("a new comment starts empty, got %q", req.Base)
	}
	// The action is discoverable in the menu, the edit action is not.
	var hasComment, hasEdit bool
	for _, a := range m.Actions() {
		hasComment = hasComment || a[0] == "n"
		hasEdit = hasEdit || a[0] == "e"
	}
	if !hasComment || hasEdit {
		t.Fatalf("actions = %v", m.Actions())
	}
}

func TestFailedProbeHidesEverything(t *testing.T) {
	// The probe answers with nothing but an error: no capabilities arrived,
	// so the gate stays shut. ('r' retries it — #2088's startMeta.)
	m := editableWith(t, forge.Capabilities{}, false, true)
	m.SetRepoMeta(forge.RepoMetaMsg{Err: errTest})
	if _, probed := m.Capabilities(); probed {
		t.Fatal("a failed probe is not an answer")
	}
	if len(m.textTargets()) > 0 || m.canComment() {
		t.Fatal("a failed probe must hide every edit action")
	}
}

// errTest is the probe failure the gate must fall back on.
var errTest = errTestType{}

type errTestType struct{}

func (errTestType) Error() string { return "probe failed" }

func TestEditActionsAreListOnlyInTheDetail(t *testing.T) {
	m := filled(t)
	m.SetSize(90, 40)
	m.SetRepoMeta(forge.RepoMetaMsg{Caps: forge.Capabilities{Login: "bo", Push: true}})
	// On the list — no detail open — nothing is editable and the keys keep
	// their list meanings.
	if len(m.textTargets()) > 0 {
		t.Fatal("the list view offers no edit targets")
	}
	for _, a := range m.Actions() {
		if a[0] == "e" || a[0] == "n" {
			t.Fatalf("the list must not carry the detail's text-edit actions: %v", a)
		}
	}
}
