package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
	"ike/internal/ghissues"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/registry"
)

// issues_panel_test.go covers the GitHub Issues tool window wiring (#1934):
// the toggle lifecycle, layout persistence, the fetch-result routing, and the
// start-work request path.

func issuesApp(t *testing.T) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return out.(Model)
}

func TestIssuesToggleLifecycle(t *testing.T) {
	m := issuesApp(t)
	before := m.activeWS().Panes.Focused()

	out, cmd := m.Update(IssuesToggleMsg{})
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.IssuesKey) || m.activeWS().Panes.Focused() != pane.IssuesKey {
		t.Fatalf("first toggle must open + focus the panel (focus=%q)", m.activeWS().Panes.Focused())
	}
	if cmd == nil {
		t.Fatal("opening must start the first fetch")
	}
	out, _ = m.Update(IssuesToggleMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != before {
		t.Fatalf("focus = %q, want %q", m.activeWS().Panes.Focused(), before)
	}
	out, _ = m.Update(IssuesToggleMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != pane.IssuesKey {
		t.Fatal("third toggle must re-focus the panel")
	}
}

func TestIssuesPanePersists(t *testing.T) {
	m := issuesApp(t)
	out, _ := m.Update(IssuesToggleMsg{})
	m = out.(Model)
	_, ids, ok := loadLayout()
	if !ok {
		t.Fatal("layout must have been saved")
	}
	if ids[pane.IssuesKey].Kind != "issues" {
		t.Fatalf("identity = %+v", ids[pane.IssuesKey])
	}
	// A fresh model over the same config dir restores the pane (empty).
	m2 := NewWith(registry.New(), host.MapConfig{})
	out2, _ := m2.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 = out2.(Model)
	if !m2.activeWS().Panes.Has(pane.IssuesKey) {
		t.Fatal("restored layout must keep the issues pane leaf")
	}
	_ = m
}

func TestIssuesFetchResultFillsPanel(t *testing.T) {
	m := issuesApp(t)
	out, _ := m.Update(IssuesToggleMsg{})
	m = out.(Model)
	out, _ = m.Update(forge.IssuesMsg{Issues: []forge.Issue{{Number: 7, Title: "seven"}}})
	m = out.(Model)
	p := m.issuesPanel()
	if p == nil || !p.Loaded() || p.Visible() != 1 {
		t.Fatalf("panel must hold the fetched listing (loaded=%v)", p != nil && p.Loaded())
	}
}

func TestIssuesStartWorkRequestRunsFlow(t *testing.T) {
	m := issuesApp(t)
	// Run from a non-repo directory: the command must resolve to a
	// StartWorkDoneMsg carrying git's error — never touch the real checkout,
	// never panic or hang.
	t.Chdir(t.TempDir())
	_, cmd := m.Update(ghissues.StartWorkRequestMsg{Number: 7, Title: "seven"})
	if cmd == nil {
		t.Fatal("the start-work request must dispatch the forge command")
	}
	msg, ok := cmd().(forge.StartWorkDoneMsg)
	if !ok || msg.Err == nil {
		t.Fatalf("msg = %#v, want the git error", msg)
	}
}
