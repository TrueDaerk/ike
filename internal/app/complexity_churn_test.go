package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/vcs"
)

// complexity_churn_test.go covers the hotspot columns' app half (#2957):
// no git call in Update, one fetch per VCS snapshot generation while the
// Complexity window is open, the cached counts reaching the pane, a
// changed complexity.churn_days re-fetching, and the columns hiding
// outside git.

// churnDaysConfig sets complexity.churn_days for one test.
func churnDaysConfig(t *testing.T, days int) {
	t.Helper()
	orig := config.Get()
	t.Cleanup(func() { config.Set(orig) })
	c, _ := config.Load(config.Options{})
	c.Complexity.ChurnDays = days
	config.Set(c)
}

// settle runs one settled Update pass.
func churnSettle(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	out, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return out.(Model), cmd
}

func TestComplexityChurnFetchPerSnapshotGeneration(t *testing.T) {
	churnDaysConfig(t, 90)
	m, _ := timeApp(t)
	if m.complexityIndex == nil {
		t.Skip("no complexity index in this build")
	}
	m.vcs.snap = nil
	// A closed window fetches nothing, even inside a repository.
	m.vcs.snap = vcs.NewSnapshot("/r", nil)
	m, _ = churnSettle(t, m)
	if m.cxChurn.fetching {
		t.Fatal("a closed Complexity window must not fetch the churn")
	}
	m.vcs.snap = nil
	out, _ := m.Update(ComplexityToggleMsg{})
	m = out.(Model)
	p := m.complexityPanel()
	if p == nil {
		t.Fatal("the Complexity window did not open")
	}
	m, _ = churnSettle(t, m)
	if counts, _ := p.Churn(); counts != nil || m.cxChurn.fetching {
		t.Fatalf("outside git: counts %v fetching %v", counts, m.cxChurn.fetching)
	}

	// A snapshot lands: the settled pass launches exactly one fetch.
	out, cmd := m.Update(vcs.SnapshotMsg{Snap: vcs.NewSnapshot("/r", map[string]vcs.FileStatus{"a.go": vcs.StatusModified})})
	m = out.(Model)
	if cmd == nil || !m.cxChurn.fetching || m.cxChurn.pending.snapGen != 1 || m.cxChurn.pending.days != 90 {
		t.Fatalf("snapshot must launch the fetch: fetching=%v pending=%+v", m.cxChurn.fetching, m.cxChurn.pending)
	}
	fetch := m.complexityChurnSyncCmd()
	if fetch != nil {
		t.Fatal("a fetch in flight must not launch a second")
	}

	// The counts land and reach the pane; the next pass runs nothing.
	out, _ = m.Update(vcs.ChurnMsg{Root: m.complexityIndex.Root(), Counts: map[string]int{"a.go": 3}})
	m = out.(Model)
	if counts, days := p.Churn(); counts["a.go"] != 3 || days != 90 || m.cxChurn.fetching {
		t.Fatalf("pane churn = %v/%d fetching=%v", counts, days, m.cxChurn.fetching)
	}
	if cmd := m.complexityChurnSyncCmd(); cmd != nil {
		t.Fatal("an unchanged generation must not re-fetch")
	}

	// A refreshed snapshot re-fetches once; the command is the git call.
	m.Update(vcs.SnapshotMsg{Snap: vcs.NewSnapshot("/r", map[string]vcs.FileStatus{"b.go": vcs.StatusModified})})
	if !m.cxChurn.fetching || m.cxChurn.pending.snapGen != 2 {
		t.Fatalf("a new snapshot must re-fetch: %+v", m.cxChurn.pending)
	}
	m.applyComplexityChurn(vcs.ChurnMsg{Counts: map[string]int{"a.go": 4}})
	if counts, _ := p.Churn(); counts["a.go"] != 4 {
		t.Fatalf("pane churn after the refresh = %v", counts)
	}

	// A changed window applies live: one more fetch, over the new window.
	churnDaysConfig(t, 30)
	cmd = m.complexityChurnSyncCmd()
	if cmd == nil || m.cxChurn.pending.days != 30 {
		t.Fatalf("a new churn_days must re-fetch: %+v", m.cxChurn.pending)
	}
	if msg, ok := cmd().(vcs.ChurnMsg); !ok || msg.Root != m.complexityIndex.Root() {
		t.Fatalf("the fetch must run vcs.ChurnCmd on the project root, got %#v", msg)
	}
	m.applyComplexityChurn(vcs.ChurnMsg{Counts: map[string]int{"a.go": 1}})
	if _, days := p.Churn(); days != 30 {
		t.Fatalf("pane window = %d, want 30", days)
	}

	// A git error hides the columns without retrying every pass.
	m.vcs.snap = vcs.NewSnapshot("/r", map[string]vcs.FileStatus{"c.go": vcs.StatusModified})
	m.noteVCSSnapshotForComplexity()
	m.complexityChurnSyncCmd()
	m.applyComplexityChurn(vcs.ChurnMsg{Err: errTestChurn{}})
	if counts, _ := p.Churn(); counts != nil {
		t.Fatalf("a failed fetch must hide the columns, got %v", counts)
	}
	if cmd := m.complexityChurnSyncCmd(); cmd != nil {
		t.Fatal("a failed fetch must not retry on the same key")
	}

	// Leaving the repository hides the columns.
	m.applyComplexityChurn(vcs.ChurnMsg{Counts: map[string]int{}})
	m.vcs.snap = nil
	m, _ = churnSettle(t, m)
	if counts, _ := p.Churn(); counts != nil {
		t.Fatalf("outside git the columns must hide, got %v", counts)
	}
}

type errTestChurn struct{}

func (errTestChurn) Error() string { return "git: not a repository" }
