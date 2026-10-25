package complexitypanel

import (
	"strings"
	"testing"

	"ike/internal/complexity"
)

// hidden_test.go covers the panel half of the hidden set (#3031): hidden
// functions leave the rows, the totals, the groups and the header count;
// hidden:yes lists them muted and marked; x hides a function row and
// un-hides a shown one, hides the file of a group / tree row; X hides the
// row's file; H toggles the view.

// hiddenPanel is sample() with render (app.go) hidden.
func hiddenPanel(t *testing.T) (*Model, *complexity.HiddenSet) {
	t.Helper()
	m := newPanel(t)
	h := &complexity.HiddenSet{}
	h.Add(complexity.EntryKey{File: "internal/app/app.go", Function: "render", Ordinal: 0})
	m.SetHidden(h)
	return m, h
}

func hideMsg(t *testing.T, m *Model) HideToggleMsg {
	t.Helper()
	cmd, why := m.ToggleHide()
	if cmd == nil {
		t.Fatalf("ToggleHide refused: %s", why)
	}
	msg, ok := cmd().(HideToggleMsg)
	if !ok {
		t.Fatalf("ToggleHide yielded %T", cmd())
	}
	return msg
}

func hideFileMsg(t *testing.T, cmd func() interface{}, why string) HideFileMsg {
	t.Helper()
	if cmd == nil {
		t.Fatalf("refused: %s", why)
	}
	msg, ok := cmd().(HideFileMsg)
	if !ok {
		t.Fatalf("yielded %T", cmd())
	}
	return msg
}

func TestHiddenFunctionsLeaveRowsTotalsAndHeader(t *testing.T) {
	m, _ := hiddenPanel(t)
	for _, r := range m.Rows() {
		if r.Fn.Name == "render" {
			t.Fatalf("render must be hidden: %v", names(m.Rows()))
		}
	}
	if s := m.Summary(); s.Functions != 4 || s.OverThreshold != 2 {
		t.Fatalf("summary follows the visible population: %+v", s)
	}
	if m.HiddenCount() != 1 {
		t.Fatalf("hidden count = %d", m.HiddenCount())
	}
	m.SetSize(240, 12)
	v := m.View()
	for _, want := range []string{"1 hidden", "x hide", "H hidden"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	// The group rows and the CSV follow.
	m.CycleGroup() // by class
	m.CycleGroup() // by file
	for _, r := range m.Rows() {
		if r.Group != nil && r.Rel == "internal/app/app.go" && r.Group.Functions != 1 {
			t.Fatalf("the file group counts only tiny: %+v", r.Group.Summary)
		}
	}
	m.CycleGroup()
	if csv := m.CSV(); strings.Contains(csv, "render") {
		t.Fatalf("the export must not list a hidden function:\n%s", csv)
	}
	// Nothing hidden: no count, no H hint.
	m.SetHidden(nil)
	if v := m.View(); m.HiddenCount() != 0 || strings.Contains(v, "1 hidden") || strings.Contains(v, "H hidden") {
		t.Fatalf("without a set nothing is hidden:\n%s", m.View())
	}
}

func TestHiddenViewListsMarkedRowsAndUnhides(t *testing.T) {
	m, _ := hiddenPanel(t)
	if m.ToggleHiddenView() != true || !m.ShowingHidden() || m.FilterText() != "hidden:yes" {
		t.Fatalf("H must set hidden:yes: %q", m.FilterText())
	}
	if got := names(m.Rows()); len(got) != 1 || got[0] != "render" {
		t.Fatalf("hidden:yes rows = %v", got)
	}
	if !m.Rows()[0].Hidden {
		t.Fatal("the shown row carries the Hidden flag")
	}
	m.SetSize(240, 12)
	if v := m.View(); !strings.Contains(v, "render ✕") || !strings.Contains(v, "H hidden: shown") {
		t.Fatalf("the hidden row is marked:\n%s", v)
	}
	// x on the shown row un-hides it.
	msg := hideMsg(t, m)
	if !msg.Remove || len(msg.Items) != 1 || msg.Items[0].Fn.Name != "render" || msg.Items[0].Rel != "internal/app/app.go" {
		t.Fatalf("un-hide message = %+v", msg)
	}
	// hidden:no is the complement; H again clears the term.
	m.filter.SetText("hidden:no")
	m.Refresh()
	if got := names(m.Rows()); len(got) != 4 {
		t.Fatalf("hidden:no rows = %v", got)
	}
	m.filter.SetText("hidden:yes")
	m.Refresh()
	if m.ToggleHiddenView() || m.FilterText() != "" {
		t.Fatalf("H must clear the term: %q", m.FilterText())
	}
	// The empty hidden view explains itself.
	m.SetHidden(nil)
	m.ToggleHiddenView()
	if v := m.View(); !strings.Contains(v, "nothing hidden on this tab") {
		t.Fatalf("empty hidden view:\n%s", v)
	}
}

func TestHideKeysBuildMessages(t *testing.T) {
	m := newPanel(t)
	// The first row is render (CCN 25); x hides it.
	msg := hideMsg(t, m)
	if msg.Remove || len(msg.Items) != 1 || msg.Items[0].Fn.Name != "render" || msg.Items[0].Ordinal != 0 {
		t.Fatalf("hide message = %+v", msg)
	}
	// The 'x' key yields the same command.
	if cmd := m.Update(key("x")); cmd == nil {
		t.Fatal("x must yield the hide command")
	} else if _, ok := cmd().(HideToggleMsg); !ok {
		t.Fatalf("x yielded %T", cmd())
	}
	// X hides the row's file.
	fm := hideFileMsg(t, func() interface{} { c, _ := m.HideFile(); return c() }, "")
	if len(fm.Globs) != 1 || fm.Globs[0] != "internal/app/app.go" || fm.Label != "internal/app/app.go" {
		t.Fatalf("hide file message = %+v", fm)
	}
	// On a group row x hides the file too.
	m.CycleGroup()
	m.CycleGroup() // by file
	if r, _ := m.Selected(); r.Group == nil {
		t.Fatalf("expected a group row, got %+v", r)
	}
	cmd, why := m.ToggleHide()
	fm = hideFileMsg(t, func() interface{} { return cmd() }, why)
	if r, _ := m.Selected(); fm.Globs[0] != r.Rel {
		t.Fatalf("group hide = %+v, want %s", fm, r.Rel)
	}
	m.CycleGroup() // none
	// The Trend tab has nothing to hide.
	m.SetTab(TabTrend)
	if c, why := m.ToggleHide(); c != nil || why == "" {
		t.Fatalf("Trend refusal = %v, %q", c, why)
	}
	m.SetTab(TabProject)
	// A site row stands for its function.
	m.ToggleSites()
	if rows := m.Rows(); len(rows) > 1 && rows[1].Site != nil {
		m.cursor = 1
		if msg := hideMsg(t, m); msg.Items[0].Fn.Name != "render" {
			t.Fatalf("site row hide = %+v", msg)
		}
	}
}

func TestHideInTreeViewTargetsDirectoriesAndRefusesRoot(t *testing.T) {
	m := newPanel(t)
	m.ToggleTreeView()
	if c, why := m.ToggleHide(); c != nil || !strings.Contains(why, "project root") {
		t.Fatalf("root refusal = %v, %q", c, why)
	}
	// Unfold the root and step onto its first directory.
	m.Update(key("l"))
	m.Update(key("j"))
	r, ok := m.Selected()
	if !ok || r.Group == nil {
		t.Fatalf("selected = %+v, %v", r, ok)
	}
	cmd, why := m.ToggleHide()
	fm := hideFileMsg(t, func() interface{} { return cmd() }, why)
	if !strings.HasSuffix(fm.Globs[0], "/**") || !strings.HasPrefix(fm.Globs[0], r.Rel) {
		t.Fatalf("directory hide = %+v for %q", fm, r.Rel)
	}
}
