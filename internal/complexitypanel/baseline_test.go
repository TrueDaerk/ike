package complexitypanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/theme"
)

// baseline_test.go covers the panel half of the accepted-debt baseline
// (#2958): the classification of the rows, the muted ✓ rendering, the
// totals and group rows that count only new and worsened functions, the
// baseline: filter, and 'i' on function, site and group rows.

// baselinePanel is sample() with render (CCN 25 / cog 30) and Page::show
// (CCN 16) accepted and ListNav (CCN 12, cog 18) recorded worse than it
// was, so the three statuses are all on screen.
func baselinePanel(t *testing.T) (*Model, *complexity.Baseline) {
	t.Helper()
	m := newPanel(t)
	b := complexity.NewBaseline(15, 15)
	b.Accept("internal/app/app.go", fn("render", 0, 20, 25, 30), 0)
	b.Accept("web/index.php", fn("Page::show", 0, 9, 16, 4), 0)
	b.Accept("internal/ui/list.go", fn("ListNav", 5, 40, 12, 10), 0) // cog grew 10 → 18
	m.SetBaseline(b)
	return m, b
}

func statusOf(m *Model, name string) complexity.Status {
	for _, r := range m.Rows() {
		if r.Group == nil && r.Site == nil && r.Fn.Name == name {
			return r.Fn.Baseline
		}
	}
	return -1
}

func TestBaselineClassifiesRowsAndTotals(t *testing.T) {
	m, _ := baselinePanel(t)
	if got := statusOf(m, "render"); got != complexity.BaselineAccepted {
		t.Errorf("render = %v, want accepted", got)
	}
	if got := statusOf(m, "ListNav"); got != complexity.BaselineWorsened {
		t.Errorf("ListNav = %v, want worsened", got)
	}
	if got := statusOf(m, "tiny"); got != complexity.BaselineNew {
		t.Errorf("tiny = %v, want new", got)
	}
	// Over: ListNav (worsened) only; render and Page::show are accepted.
	s := m.Summary()
	if s.OverThreshold != 1 || s.Accepted != 2 || s.Worsened != 1 || s.Excess != 3 {
		t.Fatalf("summary over=%d accepted=%d worsened=%d excess=%d", s.OverThreshold, s.Accepted, s.Worsened, s.Excess)
	}
	m.SetSize(240, 12) // room for the sort metric's percentiles (#2953)
	v := m.View()
	for _, want := range []string{"render ✓", "Page::show ✓", "1 over", "excess 3", "2 accepted", "i accept"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "ListNav ✓") || strings.Contains(v, "tiny ✓") {
		t.Errorf("only accepted rows carry ✓:\n%s", v)
	}
	// The accepted row's over-threshold cells are muted (faint), not
	// warned; the worsened row keeps the warning colour and no faint run.
	pal := theme.DefaultPalette()
	for i, r := range m.Rows() {
		line := m.renderRow(pal, i)
		switch r.Fn.Name {
		case "render":
			if !strings.Contains(line, "\x1b[2;") {
				t.Errorf("render row must paint its CCN muted:\n%q", line)
			}
		case "ListNav":
			if strings.Contains(line, "\x1b[2;") || !strings.Contains(line, "18") {
				t.Errorf("ListNav row must keep the warning colour:\n%q", line)
			}
		}
	}
	// Dropping the baseline restores the plain population.
	m.SetBaseline(nil)
	if s := m.Summary(); s.OverThreshold != 3 || s.Accepted != 0 || strings.Contains(m.View(), "✓") || strings.Contains(m.View(), "accepted") {
		t.Fatalf("nil baseline: over=%d accepted=%d\n%s", s.OverThreshold, s.Accepted, m.View())
	}
}

func TestBaselineFilterKey(t *testing.T) {
	m, _ := baselinePanel(t)
	for want, rows := range map[string]string{
		"baseline:accepted": "render,Page::show",
		"baseline:worsened": "ListNav",
		"baseline:new":      "inner,tiny",
	} {
		m.filter.SetText(want)
		m.Refresh()
		if got := strings.Join(names(m.Rows()), ","); got != rows {
			t.Errorf("%s = %q, want %q", want, got, rows)
		}
	}
	// Group rows: by the members' statuses.
	m.filter.SetText("")
	m.Refresh()
	m.CycleGroup()
	m.CycleGroup() // file level
	m.filter.SetText("baseline:worsened")
	m.Refresh()
	if len(m.Rows()) != 1 || m.Rows()[0].Rel != "internal/ui/list.go" {
		t.Errorf("baseline:worsened on file groups = %v", m.Rows())
	}
	m.filter.SetText("baseline:accepted")
	m.Refresh()
	if len(m.Rows()) != 2 {
		t.Errorf("baseline:accepted on file groups = %d rows", len(m.Rows()))
	}
	m.filter.SetText("baseline:new")
	m.Refresh()
	if len(m.Rows()) != 0 {
		t.Errorf("no file group has a new over-threshold member: %v", m.Rows())
	}
}

func TestBaselineGroupRowsCountOnlyNewAndWorsened(t *testing.T) {
	m, _ := baselinePanel(t)
	m.CycleGroup()
	m.CycleGroup() // file level
	for _, r := range m.Rows() {
		g := r.Group
		switch r.Rel {
		case "internal/app/app.go":
			if g.OverThreshold != 0 || g.Excess != 0 || g.Accepted != 1 {
				t.Errorf("app.go group = over %d excess %d accepted %d", g.OverThreshold, g.Excess, g.Accepted)
			}
		case "internal/ui/list.go":
			if g.OverThreshold != 1 || g.Excess != 3 || g.Worsened != 1 {
				t.Errorf("list.go group = over %d excess %d worsened %d", g.OverThreshold, g.Excess, g.Worsened)
			}
		}
		if len(r.Members) != 2 && r.Rel != "web/index.php" {
			t.Errorf("%s members = %d", r.Rel, len(r.Members))
		}
	}
}

func toggle(t *testing.T, m *Model) (BaselineToggleMsg, bool) {
	t.Helper()
	cmd, ok := m.ToggleBaseline()
	if !ok {
		return BaselineToggleMsg{}, false
	}
	msg, is := cmd().(BaselineToggleMsg)
	if !is {
		t.Fatalf("cmd yields %T", cmd())
	}
	return msg, true
}

func selectRow(t *testing.T, m *Model, name string) {
	t.Helper()
	for i, r := range m.Rows() {
		if r.Group == nil && r.Site == nil && r.Fn.Name == name {
			m.cursor = i
			return
		}
	}
	t.Fatalf("no row %s in %v", name, names(m.Rows()))
}

func TestToggleBaselineFunctionRows(t *testing.T) {
	m, _ := baselinePanel(t)
	// An accepted function → remove.
	selectRow(t, m, "render")
	msg, ok := toggle(t, m)
	if !ok || !msg.Remove || len(msg.Items) != 1 || msg.Items[0].Rel != "internal/app/app.go" || msg.Items[0].Fn.Name != "render" || msg.Items[0].Ordinal != 0 {
		t.Fatalf("accepted row: %+v ok=%v", msg, ok)
	}
	// A worsened function → re-accept at the current metrics.
	selectRow(t, m, "ListNav")
	msg, ok = toggle(t, m)
	if !ok || msg.Remove || len(msg.Items) != 1 || msg.Items[0].Fn.Cognitive != 18 {
		t.Fatalf("worsened row: %+v ok=%v", msg, ok)
	}
	// A function under the thresholds has nothing to accept.
	selectRow(t, m, "tiny")
	if _, ok := toggle(t, m); ok {
		t.Fatal("an under-threshold row must not toggle")
	}
	// The 'i' key yields the same message; a site row stands for its
	// function.
	selectRow(t, m, "ListNav")
	cmd := m.Update(key("i"))
	if cmd == nil {
		t.Fatal("i must yield a command")
	}
	if got, is := cmd().(BaselineToggleMsg); !is || got.Items[0].Fn.Name != "ListNav" {
		t.Fatalf("i → %+v", cmd())
	}
	// The Changes tab never toggles.
	m.SetTab(TabChanges)
	if _, ok := toggle(t, m); ok {
		t.Fatal("the Changes tab must not toggle")
	}
}

func TestToggleBaselineGroupRows(t *testing.T) {
	m := newPanel(t)
	b := complexity.NewBaseline(15, 15)
	m.SetBaseline(b)
	m.CycleGroup()
	m.CycleGroup() // file level: app.go (render over), list.go (ListNav over), index.php (Page::show over)
	var appRow int = -1
	for i, r := range m.Rows() {
		if r.Rel == "internal/app/app.go" {
			appRow = i
		}
	}
	if appRow < 0 {
		t.Fatal("no app.go group")
	}
	m.cursor = appRow
	msg, ok := toggle(t, m)
	if !ok || msg.Remove || len(msg.Items) != 1 || msg.Items[0].Fn.Name != "render" {
		t.Fatalf("group accept: %+v ok=%v", msg, ok)
	}
	// Once every over-threshold member is accepted the group toggles them
	// off again.
	for _, it := range msg.Items {
		b.Accept(it.Rel, it.Fn, it.Ordinal)
	}
	m.SetBaseline(b)
	m.cursor = appRow
	msg, ok = toggle(t, m)
	if !ok || !msg.Remove || len(msg.Items) != 1 {
		t.Fatalf("group remove: %+v ok=%v", msg, ok)
	}
	// A group without an over-threshold member has nothing to toggle.
	m.Set(complexity.View{Gen: 4, Files: map[string][]complexity.Function{"/proj/x.go": {fn("small", 0, 3, 2, 1)}}}, complexity.Stats{Enabled: true})
	m.cursor = 0
	if _, ok := toggle(t, m); ok {
		t.Fatal("a group of small functions must not toggle")
	}
	_ = tea.KeyPressMsg{}
}
