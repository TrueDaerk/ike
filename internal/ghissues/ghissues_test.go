package ghissues

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
)

// ghissues_test.go covers the pane model (#1934): result application, fuzzy
// and label filtering, the detail view, and the action messages the pane
// emits for the app to run.

func key(s string) tea.KeyPressMsg {
	if len([]rune(s)) == 1 {
		r := []rune(s)[0]
		return tea.KeyPressMsg{Code: r, Text: s}
	}
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	}
	panic("unhandled key " + s)
}

func filled(t *testing.T) *Model {
	t.Helper()
	m := New(nil)
	m.SetSize(90, 12)
	m.SetResult(forge.IssuesMsg{
		Issues: []forge.Issue{
			{Number: 1, Title: "explorer crash on rename", URL: "https://e/1",
				Labels: []forge.Label{{Name: "bug", Color: "d73a4a"}}, Assignees: []string{"dev"}},
			{Number: 2, Title: "add markdown preview", URL: "https://e/2",
				Labels: []forge.Label{{Name: "feature", Color: "a2eeef"}}, Body: "## Task\nRender it."},
			{Number: 3, Title: "explorer icons", URL: "https://e/3",
				Labels: []forge.Label{{Name: "feature", Color: "a2eeef"}}},
		},
		PRs: []forge.PR{{Number: 9, State: "OPEN", HeadRef: "issue/1-explorer-crash", Checks: forge.ChecksPassing}},
	})
	return &m
}

func TestSetResultStates(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 10)
	m.MarkLoading()
	if !strings.Contains(m.View(), "fetching issues") {
		t.Fatal("loading state must render")
	}
	m.SetResult(forge.IssuesMsg{Setup: "GitHub CLI (gh) not found — install it to browse issues"})
	if !strings.Contains(m.View(), "gh) not found") {
		t.Fatal("setup state must render")
	}
	m.SetResult(forge.IssuesMsg{Err: errFake("dial tcp: timeout")})
	if !strings.Contains(m.View(), "fetch failed") {
		t.Fatal("error state must render")
	}
	m.SetResult(forge.IssuesMsg{})
	if !m.Loaded() || !strings.Contains(m.View(), "no open issues") {
		t.Fatal("empty listing must render as loaded-empty")
	}
}

type errFake string

func (e errFake) Error() string { return string(e) }

func TestFuzzyFilterNarrows(t *testing.T) {
	m := filled(t)
	m.Update(key("/"))
	if !m.Filtering() {
		t.Fatal("/ must open the filter line")
	}
	for _, r := range "explorer" {
		m.Update(key(string(r)))
	}
	if m.Visible() != 2 {
		t.Fatalf("visible = %d, want 2", m.Visible())
	}
	m.Update(key("enter"))
	if m.Filtering() || m.Filter() != "explorer" {
		t.Fatalf("enter must keep the filter (filtering=%v filter=%q)", m.Filtering(), m.Filter())
	}
	m.Update(key("/"))
	m.Update(key("esc"))
	if m.Filter() != "" || m.Visible() != 3 {
		t.Fatalf("esc must clear the filter (filter=%q visible=%d)", m.Filter(), m.Visible())
	}
}

func TestLabelFilterCycles(t *testing.T) {
	m := filled(t)
	m.Update(key("l"))
	if m.LabelFilter() != "bug" || m.Visible() != 1 {
		t.Fatalf("first cycle: label=%q visible=%d", m.LabelFilter(), m.Visible())
	}
	m.Update(key("l"))
	if m.LabelFilter() != "feature" || m.Visible() != 2 {
		t.Fatalf("second cycle: label=%q visible=%d", m.LabelFilter(), m.Visible())
	}
	m.Update(key("l"))
	if m.LabelFilter() != "" || m.Visible() != 3 {
		t.Fatalf("third cycle must clear: label=%q visible=%d", m.LabelFilter(), m.Visible())
	}
}

func TestDetailRendersBody(t *testing.T) {
	m := filled(t)
	m.Update(key("j")) // to issue #2
	if sel := m.Selected(); sel == nil || sel.Number != 2 {
		t.Fatalf("selected = %+v", m.Selected())
	}
	m.Update(key("enter"))
	if !m.DetailOpen() {
		t.Fatal("enter must open the detail view")
	}
	v := m.View()
	if !strings.Contains(v, "Render") || !strings.Contains(v, "Task") {
		t.Fatalf("detail must show the rendered body:\n%s", v)
	}
	m.Update(key("esc"))
	if m.DetailOpen() {
		t.Fatal("esc must close the detail view")
	}
}

func TestStartWorkEmitsRequest(t *testing.T) {
	m := filled(t)
	cmd := m.Update(key("s"))
	if cmd == nil {
		t.Fatal("s must emit the start-work request")
	}
	msg, ok := cmd().(StartWorkRequestMsg)
	if !ok || msg.Number != 1 || msg.Title != "explorer crash on rename" {
		t.Fatalf("msg = %#v", msg)
	}
}

func TestOpenInBrowserEmitsURL(t *testing.T) {
	m := filled(t)
	cmd := m.Update(key("o"))
	if cmd == nil {
		t.Fatal("o must emit the open-URL request")
	}
	msg, ok := cmd().(OpenURLMsg)
	if !ok || msg.URL != "https://e/1" {
		t.Fatalf("msg = %#v", msg)
	}
}

func TestPRMarkerShown(t *testing.T) {
	m := filled(t)
	if v := m.View(); !strings.Contains(v, "PR#9") {
		t.Fatalf("linked PR must show in the list:\n%s", v)
	}
}

func TestRefreshRunsInjectedCmd(t *testing.T) {
	m := filled(t)
	ran := false
	m.SetRefresh(func() tea.Msg { ran = true; return nil })
	if cmd := m.Update(key("r")); cmd != nil {
		cmd()
	}
	if !ran {
		t.Fatal("r must run the injected refresh command")
	}
}

func TestLabelChipsInView(t *testing.T) {
	m := filled(t)
	if v := m.View(); !strings.Contains(v, "bug") || !strings.Contains(v, "@dev") {
		t.Fatalf("labels and assignee must render:\n%s", v)
	}
}

// TestRevealJumpsToIssueDetail: the forge event dialog's open action (#2086)
// lands on the announced issue's detail view, even past active filters.
func TestRevealJumpsToIssueDetail(t *testing.T) {
	m := filled(t)
	m.Update(key("/"))
	for _, r := range "markdown" {
		m.Update(tea.KeyPressMsg{Text: string(r), Code: r})
	}
	if m.Visible() != 1 {
		t.Fatalf("precondition: filter narrows to 1, have %d", m.Visible())
	}
	if !m.Reveal(3) {
		t.Fatal("Reveal must find a listed issue hidden by the filter")
	}
	if m.Filter() != "" || m.LabelFilter() != "" {
		t.Fatal("Reveal drops the filters so the issue is reachable")
	}
	if !m.DetailOpen() {
		t.Fatal("Reveal opens the detail view")
	}
	if is := m.Selected(); is == nil || is.Number != 3 {
		t.Fatalf("selected = %v want #3", is)
	}
	if m.Reveal(999) {
		t.Fatal("an unlisted number must not reveal")
	}
}
