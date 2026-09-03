package palette

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/frecency"
)

// recent_frecency_test.go covers the #2399 half of the recent-files dialog:
// frecency ranking of both lists, the projects-only "p:" filter, the
// preselection of the previous pick, and the hint line documenting the two.

const recentTestHalfLife = 14 * 24 * time.Hour

// fixedClock pins a store's clock to base + d.
func fixedClock(base time.Time, d time.Duration) func() time.Time {
	return func() time.Time { return base.Add(d) }
}

// recentFrecMode builds a RecentMode over list with an in-memory frecency
// store whose clock is pinned to base, and returns both.
func recentFrecMode(list []string, base time.Time) (*RecentMode, *Frecency) {
	m := testRecentMode(list)
	f := frecency.Load("", recentTestHalfLife)
	f.SetNow(fixedClock(base, 0))
	m.SetFrecency(f)
	return m, f
}

// TestRecentFrecencyOutranksBareRecency is the acceptance criterion: a file
// opened often, if a while ago, beats one opened once just now — the whole
// point of the change, since plain MRU order put the rare-but-newest file on
// top and that is what the esc-and-retry streaks were about.
func TestRecentFrecencyOutranksBareRecency(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	// MRU order: "fresh.go" was touched last, so recency alone lists it first.
	m, f := recentFrecMode([]string{"fresh.go", "hot.go"}, base)
	for i := 0; i < 6; i++ {
		f.SetNow(fixedClock(base, time.Duration(i)*time.Hour))
		f.Record(frecency.Key("hot.go"))
	}
	// One open of the other file, three days later — newer, but alone.
	f.SetNow(fixedClock(base, 3*24*time.Hour))
	f.Record(frecency.Key("fresh.go"))
	f.SetNow(fixedClock(base, 3*24*time.Hour+time.Minute))

	if got := titles(m.Results("", Context{Root: "."})); got[0] != "hot.go" {
		t.Fatalf("empty-query order = %v, want the frequently opened file first", got)
	}
	// The decay window still matters: once the frequent file is old enough,
	// the fresh one takes the lead back.
	f.SetNow(fixedClock(base, 200*24*time.Hour))
	f.Record(frecency.Key("fresh.go"))
	if got := titles(m.Results("", Context{Root: "."})); got[0] != "fresh.go" {
		t.Fatalf("aged order = %v, want the freshly opened file first", got)
	}
}

// TestRecentRecencyFallback covers both ways the listing falls back to plain
// MRU order: no usage data at all, and the setting turned to "recency".
func TestRecentRecencyFallback(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	m, f := recentFrecMode([]string{"b.go", "a.go", "c.go"}, base)

	// A store with no history ranks nothing: MRU order survives untouched.
	if got := titles(m.Results("", Context{Root: "."})); got[0] != "b.go" || got[2] != "c.go" {
		t.Fatalf("empty history reordered the list: %v", got)
	}
	// Entries without history keep MRU order *among themselves*, behind the
	// ones that have it.
	f.Record(frecency.Key("c.go"))
	if got := titles(m.Results("", Context{Root: "."})); got[0] != "c.go" || got[1] != "b.go" || got[2] != "a.go" {
		t.Fatalf("order = %v, want the recorded file first and MRU order behind it", got)
	}
	// palette.recent.ranking = "recency" restores the pre-#2399 listing.
	m.SetRanking(func() bool { return false })
	if got := titles(m.Results("", Context{Root: "."})); got[0] != "b.go" || got[2] != "c.go" {
		t.Fatalf("recency ranking = %v, want plain MRU order", got)
	}
}

// TestRecentFrecencyYieldsToTypedQuery guards the blend policy: history leads
// an empty query, but a typed one hands the lead back to match quality.
func TestRecentFrecencyYieldsToTypedQuery(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	m, f := recentFrecMode([]string{"zzz/hot.go", "alpha.go"}, base)
	for i := 0; i < 8; i++ {
		f.Record(frecency.Key("zzz/hot.go"))
	}
	if got := titles(m.Results("", Context{Root: "."})); got[0] != "zzz/hot.go" {
		t.Fatalf("empty-query order = %v, want the hot file first", got)
	}
	if got := titles(m.Results("alpha", Context{Root: "."})); len(got) == 0 || got[0] != "alpha.go" {
		t.Fatalf("typed query = %v, want the better fuzzy match first", got)
	}
}

// projectItems builds a Recent Projects column with per-item ranks, the way
// the root model injects it.
func projectItems(names []string, ranks map[string]float64) func() []Item {
	return func() []Item {
		out := make([]Item, len(names))
		for i, n := range names {
			out[i] = Item{Title: n, Key: "/p/" + n, Rank: ranks[n], Msg: pickedProject{n}}
		}
		return out
	}
}

// pickedProject stands in for the app's project.PickedMsg.
type pickedProject struct{ name string }

// TestRecentProjectsRankByInjectedFrecency covers the column's half of the
// ranking: the app supplies the score (it owns the project paths), the mode
// blends it exactly like a file's.
func TestRecentProjectsRankByInjectedFrecency(t *testing.T) {
	m := testRecentMode(nil)
	m.SetProjects(projectItems([]string{"newest", "hot"}, map[string]float64{"hot": 5}))
	if got := titles(m.SideResults("", Context{})); got[0] != "hot" {
		t.Fatalf("side order = %v, want the frequently switched project first", got)
	}
	m.SetRanking(func() bool { return false })
	if got := titles(m.SideResults("", Context{})); got[0] != "newest" {
		t.Fatalf("recency side order = %v, want the injected (recency) order", got)
	}
}

// TestRecentProjectsOnlyFilter covers the "p:" prefix: files drop out
// entirely and the rest of the query filters projects alone.
func TestRecentProjectsOnlyFilter(t *testing.T) {
	m := testRecentMode([]string{"alpha.go", "beta.go"})
	m.SetProjects(projectItems([]string{"alpha-service", "beta-service"}, nil))

	if got := m.Results("p:", Context{Root: "."}); got != nil {
		t.Fatalf("a projects-only query listed %d files, want none", len(got))
	}
	if got := titles(m.SideResults("p:", Context{})); len(got) != 2 {
		t.Fatalf("projects-only listing = %v, want both projects", got)
	}
	got := titles(m.SideResults("p:beta", Context{}))
	if len(got) != 1 || got[0] != "beta-service" {
		t.Fatalf("filtered projects = %v, want only beta-service", got)
	}
	// Without the prefix the same query still filters files too.
	if len(m.Results("beta", Context{Root: "."})) != 1 {
		t.Fatalf("a plain query must still list files")
	}
}

// TestRecentProjectsOnlyFocusesColumn checks the whole-palette effect: with an
// empty file list the automatic focus placement hands the keyboard to the
// projects column, so enter opens a project.
func TestRecentProjectsOnlyFocusesColumn(t *testing.T) {
	m := testRecentMode([]string{"alpha.go"})
	m.SetProjects(projectItems([]string{"svc"}, nil))
	p := New(Config{}, m)
	p.SetSize(80, 24)
	p.OpenLocked(Context{Root: "."}, RecentPrefix)
	if p.sideFocus {
		t.Fatal("a fresh open must start on the file list")
	}
	p.Update(runes("p:"))
	if !p.sideFocus {
		t.Fatal("a projects-only query must move the focus to the projects column")
	}
	cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter must activate the focused project")
	}
	if _, ok := cmd().(pickedProject); !ok {
		t.Fatalf("enter emitted %T, want the project activation", cmd())
	}
}

// TestRecentPreselectsPreviousPick is the second acceptance criterion:
// reopening the dialog lands on the entry it was last used to open, so enter
// switches straight back to it.
func TestRecentPreselectsPreviousPick(t *testing.T) {
	m := testRecentMode([]string{"a.go", "b.go", "c.go"})
	var picked string
	var pickedSide bool
	m.SetPickRecorder(func(key string, side bool) { picked, pickedSide = key, side })
	m.SetLastPick(func() (string, bool) { return picked, pickedSide })

	p := New(Config{}, m)
	p.SetSize(80, 24)
	p.OpenLocked(Context{Root: "."}, RecentPrefix)
	// Walk down to the third row and open it.
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if of, ok := cmd().(OpenFileMsg); !ok || of.Path != "c.go" {
		t.Fatalf("enter opened %+v, want c.go", cmd())
	}
	if pickedSide {
		t.Fatal("a file pick must not be recorded as a project pick")
	}

	// The next open comes back on that row, and enter repeats the jump.
	p.OpenLocked(Context{Root: "."}, RecentPrefix)
	if p.selected != 2 {
		t.Fatalf("selection = %d, want the previously picked row (2)", p.selected)
	}
	cmd = p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if of, ok := cmd().(OpenFileMsg); !ok || of.Path != "c.go" {
		t.Fatalf("re-open + enter opened %+v, want c.go again", cmd())
	}
	// A typed query is a fresh intent: it ranks normally from the top.
	p.OpenLocked(Context{Root: "."}, RecentPrefix)
	p.Update(runes("a"))
	if p.selected != 0 {
		t.Fatalf("selection after typing = %d, want the best match (0)", p.selected)
	}
}

// TestRecentPreselectsPreviousProjectPick is the same for the side column: a
// project pick comes back focused, so enter switches back to it.
func TestRecentPreselectsPreviousProjectPick(t *testing.T) {
	m := testRecentMode([]string{"a.go"})
	m.SetProjects(projectItems([]string{"one", "two"}, nil))
	var picked string
	var side bool
	m.SetPickRecorder(func(key string, s bool) { picked, side = key, s })
	m.SetLastPick(func() (string, bool) { return picked, side })

	p := New(Config{}, m)
	p.SetSize(80, 24)
	p.OpenLocked(Context{Root: "."}, RecentPrefix)
	p.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // to the projects column
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil {
		t.Fatal("enter must activate the project")
	}
	if picked != "/p/two" || !side {
		t.Fatalf("recorded pick = (%q, %v), want the second project", picked, side)
	}

	p.OpenLocked(Context{Root: "."}, RecentPrefix)
	if !p.sideFocus || p.sideSel != 1 {
		t.Fatalf("re-open focus = (side %v, row %d), want the previously picked project", p.sideFocus, p.sideSel)
	}
}

// TestRecentPreselectIgnoresVanishedPick guards the degradation: a remembered
// entry that is no longer listed leaves the default first-row selection.
func TestRecentPreselectIgnoresVanishedPick(t *testing.T) {
	m := testRecentMode([]string{"a.go", "b.go"})
	m.SetLastPick(func() (string, bool) { return frecency.Key("gone.go"), false })
	p := New(Config{}, m)
	p.SetSize(80, 24)
	p.OpenLocked(Context{Root: "."}, RecentPrefix)
	if p.selected != 0 {
		t.Fatalf("selection = %d, want the default first row", p.selected)
	}
}

// TestRecentHintDocumentsFilters covers the footer line: the two affordances
// no row can show are spelled out, and the palette renders it.
func TestRecentHintDocumentsFilters(t *testing.T) {
	m := testRecentMode([]string{"a.go"})
	// Without a projects column there is nothing to document.
	if got := m.Hint(""); got != "" {
		t.Fatalf("single-column hint = %q, want none", got)
	}
	m.SetProjects(projectItems([]string{"svc"}, nil))
	hint := m.Hint("")
	if !contains(hint, ProjectsOnlyPrefix) || !contains(hint, "tab") {
		t.Fatalf("hint = %q, want the column toggle and the projects prefix", hint)
	}
	p := New(Config{}, m)
	p.SetSize(80, 24)
	p.OpenLocked(Context{Root: "."}, RecentPrefix)
	if !contains(p.View(), ProjectsOnlyPrefix) {
		t.Fatal("the rendered box must carry the hint line")
	}
}

// contains is strings.Contains under a shorter name, for the assertions above.
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
