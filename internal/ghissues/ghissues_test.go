package ghissues

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
)

// ghissues_test.go covers the pane model (#1934, #2090): result application,
// the tabbed views and their context preservation, the filter/sort/state/label
// logic, the two overlays, the mouse targets and the action messages the pane
// emits for the app to run.

// fixedNow is the clock the age columns and the sort orders are measured
// against, so relative ages in the rendering are deterministic.
var fixedNow = time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)

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
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "ctrl+j":
		return tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl}
	case "ctrl+k":
		return tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}
	// The word-edit chords a text-entry surface owes the user (#2360).
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "home":
		return tea.KeyPressMsg{Code: tea.KeyHome}
	case "end":
		return tea.KeyPressMsg{Code: tea.KeyEnd}
	case "delete":
		return tea.KeyPressMsg{Code: tea.KeyDelete}
	case "alt+left":
		return tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt}
	case "ctrl+left":
		return tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModCtrl}
	case "alt+right":
		return tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModAlt}
	case "ctrl+right":
		return tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModCtrl}
	case "alt+backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt}
	case "super+backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModSuper}
	case "ctrl+w":
		return tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl}
	}
	panic("unhandled key " + s)
}

// press feeds a whole key sequence to the model, discarding the commands.
func press(m *Model, keys ...string) {
	for _, k := range keys {
		m.Update(key(k))
	}
}

func filled(t *testing.T) *Model {
	t.Helper()
	m := New(nil)
	m.SetSize(90, 12)
	m.now = func() time.Time { return fixedNow }
	m.SetResult(forge.IssuesMsg{
		State: forge.IssuesOpen,
		Issues: []forge.Issue{
			{Number: 1, Title: "explorer crash on rename", URL: "https://e/1", State: "OPEN",
				Author: "ada", CreatedAt: fixedNow.Add(-72 * time.Hour), UpdatedAt: fixedNow.Add(-time.Hour),
				Labels: []forge.Label{{Name: "bug", Color: "d73a4a"}}, Assignees: []string{"dev"}},
			{Number: 2, Title: "add markdown preview", URL: "https://e/2", State: "OPEN",
				Author: "bo", CreatedAt: fixedNow.Add(-24 * time.Hour), UpdatedAt: fixedNow.Add(-48 * time.Hour),
				Labels: []forge.Label{{Name: "feature", Color: "a2eeef"}}, Body: "## Task\nRender it."},
			{Number: 3, Title: "explorer icons", URL: "https://e/3", State: "OPEN",
				Author: "cy", CreatedAt: fixedNow.Add(-240 * time.Hour), UpdatedAt: fixedNow.Add(-240 * time.Hour),
				Labels: []forge.Label{{Name: "feature", Color: "a2eeef"}}},
		},
		PRs: []forge.PR{
			{Number: 9, Title: "fix the explorer crash", State: "OPEN", URL: "https://e/pr/9",
				HeadRef: "issue/1-explorer-crash", Author: "ada", Review: "APPROVED",
				Checks: forge.ChecksPassing, UpdatedAt: fixedNow.Add(-2 * time.Hour)},
			{Number: 8, Title: "old work", State: "MERGED", URL: "https://e/pr/8",
				HeadRef: "issue/5-old", Author: "bo", UpdatedAt: fixedNow.Add(-96 * time.Hour)},
		},
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
	m.Update(key("f"))
	if !m.Filtering() {
		t.Fatal("f must open the filter line")
	}
	press(m, "e", "x", "p", "l", "o", "r", "e", "r")
	if m.Visible() != 2 {
		t.Fatalf("visible = %d, want 2", m.Visible())
	}
	m.Update(key("enter"))
	if m.Filtering() || m.Filter() != "explorer" {
		t.Fatalf("enter must keep the filter (filtering=%v filter=%q)", m.Filtering(), m.Filter())
	}
	if v := m.View(); !strings.Contains(v, "match: explorer") {
		t.Fatalf("a kept filter must show as a chip in the filter row:\n%s", v)
	}
	// esc inside the overlay reverts to what it opened with — the kept
	// pattern survives; esc on the list peels it (#2104).
	press(m, "f", "x")
	m.Update(key("esc"))
	if m.Filter() != "explorer" {
		t.Fatalf("esc must revert to the pattern the overlay opened with, got %q", m.Filter())
	}
	m.Update(key("esc"))
	if m.Filter() != "" || m.Visible() != 3 {
		t.Fatalf("esc on the list must clear the match (filter=%q visible=%d)", m.Filter(), m.Visible())
	}
}

func TestSlashStaysAnAlias(t *testing.T) {
	m := filled(t)
	m.Update(key("/"))
	if !m.Filtering() {
		t.Fatal("/ must stay an alias of the filter key")
	}
}

func TestLabelPickerMultiSelects(t *testing.T) {
	m := filled(t)
	m.Update(key("l"))
	if !m.PickerOpen() {
		t.Fatal("l must open the label picker")
	}
	m.Update(key("space")) // "bug"
	if got := m.LabelFilter(); len(got) != 1 || got[0] != "bug" {
		t.Fatalf("label filter = %v, want [bug]", got)
	}
	if m.Visible() != 1 {
		t.Fatalf("visible = %d, want 1", m.Visible())
	}
	m.Update(key("down"))
	m.Update(key("space")) // + "feature": the picker is an OR filter
	if got := m.LabelFilter(); len(got) != 2 {
		t.Fatalf("label filter = %v, want two labels", got)
	}
	if m.Visible() != 3 {
		t.Fatalf("visible = %d, want 3 (bug OR feature)", m.Visible())
	}
	m.Update(key("enter"))
	if m.PickerOpen() {
		t.Fatal("enter must close the picker")
	}
	if got := m.LabelFilter(); len(got) != 2 {
		t.Fatalf("enter must keep the selection, got %v", got)
	}
}

func TestLabelPickerEscRestores(t *testing.T) {
	m := filled(t)
	press(m, "l", "space", "enter") // keep "bug"
	press(m, "l", "down", "space")  // add "feature", then cancel
	m.Update(key("esc"))
	if got := m.LabelFilter(); len(got) != 1 || got[0] != "bug" {
		t.Fatalf("esc must restore the selection the picker opened with, got %v", got)
	}
	if m.Visible() != 1 {
		t.Fatalf("visible = %d, want 1 after the restore", m.Visible())
	}
}

func TestLabelPickerClearsWithBackspace(t *testing.T) {
	m := filled(t)
	press(m, "l", "space", "backspace")
	if got := m.LabelFilter(); len(got) != 0 {
		t.Fatalf("backspace must clear the selection, got %v", got)
	}
}

func TestStateFilterCyclesAndRefetches(t *testing.T) {
	m := filled(t)
	var asked []forge.IssueState
	m.SetRefresh(func(st forge.IssueState, _ int, _ bool) tea.Cmd {
		asked = append(asked, st)
		return func() tea.Msg { return nil }
	})
	m.Update(key("t"))
	if m.StateFilter() != FilterClosed {
		t.Fatalf("state = %v, want closed", m.StateFilter())
	}
	m.Update(key("t"))
	if m.StateFilter() != FilterAll {
		t.Fatalf("state = %v, want all", m.StateFilter())
	}
	m.Update(key("t"))
	if m.StateFilter() != FilterOpen {
		t.Fatalf("state = %v, want open", m.StateFilter())
	}
	// The third change narrows back to open — the fixture's listing was
	// fetched as open, so no refetch is owed (#2104).
	want := []forge.IssueState{forge.IssuesClosed, forge.IssuesAll}
	if len(asked) != 2 {
		t.Fatalf("only the states the listing cannot answer refetch, got %v", asked)
	}
	for i, st := range want {
		if asked[i] != st {
			t.Fatalf("fetch %d asked for %q, want %q", i, asked[i], st)
		}
	}
}

func TestStateFilterHidesClosedIssues(t *testing.T) {
	m := filled(t)
	m.SetResult(forge.IssuesMsg{State: forge.IssuesAll, Issues: []forge.Issue{
		{Number: 1, Title: "open one", State: "OPEN"},
		{Number: 2, Title: "closed one", State: "CLOSED"},
	}})
	if m.Visible() != 1 {
		t.Fatalf("the open filter must hide closed issues, visible = %d", m.Visible())
	}
	m.state = FilterClosed
	m.applyFilter()
	if m.Visible() != 1 || m.Selected().Number != 2 {
		t.Fatalf("the closed filter must show only closed issues, got %+v", m.Selected())
	}
	m.state = FilterAll
	m.applyFilter()
	if m.Visible() != 2 {
		t.Fatalf("the all filter must show both, visible = %d", m.Visible())
	}
}

func TestSortOrdersCycle(t *testing.T) {
	m := filled(t)
	order := func() []int {
		var out []int
		for _, i := range m.visible {
			out = append(out, m.issues[i].Number)
		}
		return out
	}
	// The default (relevance without a pattern) reads as newest first.
	if got := order(); got[0] != 2 {
		t.Fatalf("default order = %v, want the newest issue first", got)
	}
	m.Update(key("a")) // newest
	if m.SortOrder() != SortNewest {
		t.Fatalf("sort = %v, want newest", m.SortOrder())
	}
	if got := order(); got[0] != 2 || got[2] != 3 {
		t.Fatalf("newest order = %v", got)
	}
	m.Update(key("a")) // oldest
	if got := order(); got[0] != 3 || got[2] != 2 {
		t.Fatalf("oldest order = %v", got)
	}
	m.Update(key("a")) // updated
	if got := order(); got[0] != 1 {
		t.Fatalf("updated order = %v, want the most recently touched first", got)
	}
	m.Update(key("a")) // number
	if got := order(); got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("number order = %v", got)
	}
	m.Update(key("a")) // back to relevance
	if m.SortOrder() != SortRelevance {
		t.Fatalf("sort = %v, want relevance after the full cycle", m.SortOrder())
	}
}

func TestGroupingByLabel(t *testing.T) {
	m := filled(t)
	// Since #2114 'g' is list navigation (jump to the top) in every mode; the
	// grouping toggle lives in the filter overlay and the action menu.
	m.Update(key("g"))
	if m.Grouped() {
		t.Fatal("g must navigate, not toggle the grouping (#2114)")
	}
	m.toggleGroup()
	if !m.Grouped() {
		t.Fatal("the grouping toggle must switch the grouping on")
	}
	headers := 0
	for _, r := range m.rows {
		if r.idx < 0 {
			headers++
		}
	}
	if headers != 2 {
		t.Fatalf("two labels must produce two group headers, got %d in %+v", headers, m.rows)
	}
	if m.rows[0].idx >= 0 {
		t.Fatal("a grouped list must start with a header row")
	}
	if m.Cursor() == 0 || m.Selected() == nil {
		t.Fatalf("the cursor must never rest on a header (cursor=%d selected=%v)", m.Cursor(), m.Selected())
	}
	if v := m.View(); !strings.Contains(v, "▸ bug (1)") {
		t.Fatalf("group headers must render:\n%s", v)
	}
	// Walking the list steps over the headers in the direction of travel.
	for i := 0; i < len(m.rows); i++ {
		m.Update(key("j"))
		if m.Selected() == nil {
			t.Fatalf("j landed on a header at row %d", m.Cursor())
		}
	}
	for i := 0; i < len(m.rows); i++ {
		m.Update(key("k"))
		if m.Selected() == nil {
			t.Fatalf("k landed on a header at row %d", m.Cursor())
		}
	}
	m.toggleGroup()
	if m.Grouped() || len(m.rows) != 3 {
		t.Fatalf("the toggle must switch back to the flat list, rows = %d", len(m.rows))
	}
}

func TestTabSwitching(t *testing.T) {
	m := filled(t)
	if m.ActiveTab() != TabIssues {
		t.Fatal("the pane must open on the issue list")
	}
	m.Update(key("tab"))
	if m.ActiveTab() != TabPRs {
		t.Fatal("tab must switch to the PR view")
	}
	if m.VisiblePRs() != 1 {
		t.Fatalf("the open state filter must show only the open PR, got %d", m.VisiblePRs())
	}
	v := m.View()
	if !strings.Contains(v, "#9") || !strings.Contains(v, "issue/1-explorer-crash") {
		t.Fatalf("the PR list must show number and head branch:\n%s", v)
	}
	if !strings.Contains(v, "approved") {
		t.Fatalf("the PR list must show the review decision:\n%s", v)
	}
	m.Update(key("shift+tab"))
	if m.ActiveTab() != TabIssues {
		t.Fatal("shift+tab must switch back")
	}
}

func TestPRTabKeepsItsOwnCursor(t *testing.T) {
	m := filled(t)
	m.state = FilterAll
	m.applyFilter()
	press(m, "j")         // issue cursor to row 1
	press(m, "tab", "j")  // PR cursor to row 1
	press(m, "shift+tab") // back to the issues
	if m.Cursor() != 1 {
		t.Fatalf("the issue view must keep its cursor, got %d", m.Cursor())
	}
	press(m, "tab")
	if m.Cursor() != 1 {
		t.Fatalf("the PR view must keep its cursor, got %d", m.Cursor())
	}
}

func TestPREnterOpensDetail(t *testing.T) {
	m := filled(t)
	fetched := 0
	m.SetPRDetailFetch(func(pr int) tea.Cmd {
		fetched = pr
		return func() tea.Msg { return nil }
	})
	m.Update(key("tab"))
	cmd := m.Update(key("enter"))
	if !m.PRDetailOpen() {
		t.Fatal("enter on a PR must open the detail view (#2089)")
	}
	if cmd == nil || fetched != 9 {
		t.Fatalf("the detail must fetch the selected PR, fetched=%d", fetched)
	}
	// 'o' still opens the browser from the detail.
	ocmd := m.Update(key("o"))
	if ocmd == nil {
		t.Fatal("o must emit the open-URL request")
	}
	if msg, ok := ocmd().(OpenURLMsg); !ok || msg.URL != "https://e/pr/9" {
		t.Fatalf("msg = %#v", msg)
	}
	press(m, "esc")
	if m.PRDetailOpen() {
		t.Fatal("esc must return to the PR list")
	}
}

func TestDetailRendersBodyAndKeepsContext(t *testing.T) {
	m := filled(t)
	m.Update(key("j")) // to issue #1 (the second row of the newest-first list)
	wantCursor := m.Cursor()
	if sel := m.Selected(); sel == nil || sel.Number != 1 {
		t.Fatalf("selected = %+v", m.Selected())
	}
	m.Update(key("k")) // back to #2, whose body has content
	m.Update(key("enter"))
	if !m.DetailOpen() {
		t.Fatal("enter must open the detail view")
	}
	v := m.View()
	if !strings.Contains(v, "Render") || !strings.Contains(v, "Task") {
		t.Fatalf("detail must show the rendered body:\n%s", v)
	}
	if !strings.Contains(v, "issue 1/3") {
		t.Fatalf("the detail header must show the list position:\n%s", v)
	}
	m.Update(key("ctrl+j"))
	if sel := m.Selected(); sel == nil || sel.Number != 1 {
		t.Fatalf("ctrl+j must walk to the next issue, got %+v", m.Selected())
	}
	if !m.DetailOpen() {
		t.Fatal("walking must stay in the detail view")
	}
	if m.Cursor() != wantCursor {
		t.Fatalf("walking must move the list cursor with it (%d != %d)", m.Cursor(), wantCursor)
	}
	m.Update(key("ctrl+k"))
	if sel := m.Selected(); sel == nil || sel.Number != 2 {
		t.Fatalf("ctrl+k must walk back, got %+v", m.Selected())
	}
	m.Update(key("esc"))
	if m.DetailOpen() {
		t.Fatal("esc must close the detail view")
	}
}

func TestDetailRestoresCursorAndScroll(t *testing.T) {
	m := filled(t)
	m.SetSize(90, 4) // a body of two rows, so the list has to scroll
	press(m, "j", "j")
	cursor, top := m.Cursor(), m.Top()
	if top == 0 {
		t.Fatalf("the fixture must scroll for this test (top=%d)", top)
	}
	m.Update(key("enter"))
	press(m, "j", "j", "j") // scroll inside the detail
	m.Update(key("esc"))
	if m.Cursor() != cursor || m.Top() != top {
		t.Fatalf("esc must restore cursor/scroll exactly (%d/%d != %d/%d)", m.Cursor(), m.Top(), cursor, top)
	}
}

func TestActionMenuListsEveryKey(t *testing.T) {
	m := filled(t)
	m.Update(key("m"))
	if !m.ActionMenuOpen() {
		t.Fatal("m must open the action menu")
	}
	v := m.View()
	for _, want := range []string{"Actions — issues", "Open the issue detail", "Start work", "Filter by label"} {
		if !strings.Contains(v, want) {
			t.Fatalf("the action menu must list %q:\n%s", want, v)
		}
	}
	// A table taller than the box scrolls rather than dropping entries.
	// Arrows, not j/k: the letters are the menu's type-ahead (#2111).
	for i := 0; i < len(m.Actions())-1; i++ {
		m.Update(key("down"))
	}
	if v := m.View(); !strings.Contains(v, "Group by label") {
		t.Fatalf("the menu must scroll to its last entries:\n%s", v)
	}
	// Every key the list view handles is in the table the menu renders.
	keys := map[string]bool{}
	for _, a := range m.Actions() {
		keys[a[0]] = true
	}
	for _, want := range []string{"enter", "s", "o", "f", "l", "t", "a", "esc", "tab", "r"} {
		if !keys[want] {
			t.Fatalf("action table is missing %q: %v", want, m.Actions())
		}
	}
	m.Update(key("esc"))
	if m.ActionMenuOpen() {
		t.Fatal("esc must close the action menu")
	}
}

func TestActionMenuRunsTheSelectedAction(t *testing.T) {
	m := filled(t)
	m.Update(key("m"))
	m.Update(key("enter")) // the first entry: open the detail
	if m.ActionMenuOpen() {
		t.Fatal("enter must close the menu")
	}
	if !m.DetailOpen() {
		t.Fatal("enter must run the selected action")
	}
}

func TestActionMenuFollowsTheView(t *testing.T) {
	m := filled(t)
	m.Update(key("tab"))
	m.Update(key("m"))
	if !strings.Contains(m.View(), "pull requests") {
		t.Fatalf("the menu must name the view it lists:\n%s", m.View())
	}
	for _, a := range m.Actions() {
		if strings.Contains(a[1], "Group by label") {
			t.Fatal("the PR view has no grouping action to offer")
		}
	}
}

func TestClearFiltersOnEscape(t *testing.T) {
	m := filled(t)
	press(m, "f", "e", "x", "p", "enter")
	press(m, "l", "space", "enter")
	m.Update(key("t"))
	if m.Filter() == "" || len(m.LabelFilter()) == 0 || m.StateFilter() == FilterOpen {
		t.Fatal("the fixture must have every filter set")
	}
	// esc peels one narrowing at a time (#2104): match, then labels, then
	// the state gate.
	m.Update(key("esc"))
	if m.Filter() != "" || len(m.LabelFilter()) == 0 {
		t.Fatalf("the first esc must clear only the match (filter=%q labels=%v)",
			m.Filter(), m.LabelFilter())
	}
	m.Update(key("esc"))
	if len(m.LabelFilter()) != 0 || m.StateFilter() == FilterOpen {
		t.Fatalf("the second esc must clear only the labels (labels=%v state=%v)",
			m.LabelFilter(), m.StateFilter())
	}
	m.Update(key("esc"))
	if m.StateFilter() != FilterOpen {
		t.Fatalf("the third esc must reset the state gate, got %v", m.StateFilter())
	}
}

func TestStartWorkEmitsRequest(t *testing.T) {
	m := filled(t)
	cmd := m.Update(key("s"))
	if cmd == nil {
		t.Fatal("s must emit the start-work request")
	}
	msg, ok := cmd().(StartWorkRequestMsg)
	if !ok || msg.Number != 2 {
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
	if !ok || msg.URL != "https://e/2" {
		t.Fatalf("msg = %#v", msg)
	}
}

func TestRefreshRunsInjectedCmd(t *testing.T) {
	m := filled(t)
	ran := false
	m.SetRefresh(func(forge.IssueState, int, bool) tea.Cmd {
		return func() tea.Msg { ran = true; return nil }
	})
	if cmd := m.Update(key("r")); cmd != nil {
		cmd()
	}
	if !ran {
		t.Fatal("r must run the injected refresh command")
	}
}

func TestRowsShowAgeAndAuthor(t *testing.T) {
	m := filled(t)
	v := m.View()
	for _, want := range []string{"bug", "@dev", "ada", "3d", "1d"} {
		if !strings.Contains(v, want) {
			t.Fatalf("the list rows must show %q:\n%s", want, v)
		}
	}
}

func TestTabBarRenders(t *testing.T) {
	m := filled(t)
	v := m.View()
	if !strings.Contains(v, "Issues 3") || !strings.Contains(v, "PRs 1") {
		t.Fatalf("the tab bar must show both views with their counts:\n%s", v)
	}
}

func TestClickSelectsAndDoubleClickOpens(t *testing.T) {
	m := filled(t)
	now := fixedNow
	m.now = func() time.Time { return now }
	m.Click(4, m.bodyTop()+1)
	if m.Cursor() != 1 {
		t.Fatalf("a body click must select the row, cursor = %d", m.Cursor())
	}
	if m.DetailOpen() {
		t.Fatal("a single click must not open the detail")
	}
	now = now.Add(100 * time.Millisecond)
	m.Click(4, m.bodyTop()+1)
	if !m.DetailOpen() {
		t.Fatal("a double click must open the detail")
	}
}

func TestTabBarClickSwitchesView(t *testing.T) {
	m := filled(t)
	spans := m.tabBarSpans()
	m.Click(spans[1][0], 0)
	if m.ActiveTab() != TabPRs {
		t.Fatal("a click on the PRs label must switch the view")
	}
	m.Click(spans[0][0], 0)
	if m.ActiveTab() != TabIssues {
		t.Fatal("a click on the Issues label must switch back")
	}
}

func TestWheelSkipsGroupHeaders(t *testing.T) {
	m := filled(t)
	m.toggleGroup()
	for i := 0; i < len(m.rows)+2; i++ {
		m.Wheel(1)
		if m.Selected() == nil {
			t.Fatalf("the wheel must skip headers, stopped at row %d", m.Cursor())
		}
	}
	for i := 0; i < len(m.rows)+2; i++ {
		m.Wheel(-1)
		if m.Selected() == nil {
			t.Fatalf("the wheel must skip headers upwards, stopped at row %d", m.Cursor())
		}
	}
}

func TestConfigureSeedsDefaults(t *testing.T) {
	m := filled(t)
	m.Configure(fakeConfig{"issues.default_tab": "prs", "issues.default_sort": "number"})
	if m.ActiveTab() != TabPRs {
		t.Fatalf("the default tab must seed the view, got %v", m.ActiveTab())
	}
	if m.SortOrder() != SortNumber {
		t.Fatalf("the default sort must seed the order, got %v", m.SortOrder())
	}
	// A hand switch wins over any later reload.
	m.Update(key("tab"))
	m.Update(key("a"))
	tab, order := m.ActiveTab(), m.SortOrder()
	m.Configure(fakeConfig{"issues.default_tab": "prs", "issues.default_sort": "number"})
	if m.ActiveTab() != tab || m.SortOrder() != order {
		t.Fatalf("a reload must not undo a hand switch (%v/%v became %v/%v)",
			tab, order, m.ActiveTab(), m.SortOrder())
	}
}

// fakeConfig is a host.Config over a literal map.
type fakeConfig map[string]string

func (f fakeConfig) Get(key string) (string, bool) { v, ok := f[key]; return v, ok }

func (f fakeConfig) Keys() []string {
	out := make([]string, 0, len(f))
	for k := range f {
		out = append(out, k)
	}
	return out
}

func TestPasteFeedsTheFilterLine(t *testing.T) {
	m := filled(t)
	if m.PasteText("explorer") {
		t.Fatal("a paste outside the filter line must not be consumed")
	}
	m.Update(key("f"))
	if !m.PasteText("explorer") {
		t.Fatal("a paste into the open filter line must be consumed")
	}
	if m.Filter() != "explorer" || m.Visible() != 2 {
		t.Fatalf("paste must narrow live (filter=%q visible=%d)", m.Filter(), m.Visible())
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
	if m.Filter() != "" || len(m.LabelFilter()) != 0 {
		t.Fatal("Reveal drops the filters so the issue is reachable")
	}
	if m.ActiveTab() != TabIssues {
		t.Fatal("Reveal switches back to the issue view")
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

// TestFilterOverlaySections (#2104): the unified overlay carries the match
// input, the state radio, the sort cycle, the grouping toggle and the label
// rows; changes apply live and esc reverts all of them.
func TestFilterOverlaySections(t *testing.T) {
	m := filled(t)
	var asked []forge.IssueState
	m.SetRefresh(func(st forge.IssueState, _ int, _ bool) tea.Cmd {
		asked = append(asked, st)
		return func() tea.Msg { return nil }
	})
	m.Update(key("f"))
	if !m.PickerOpen() || !m.Filtering() {
		t.Fatal("f must open the filter overlay on the match row")
	}
	m.Update(key("down")) // state row
	if m.Filtering() {
		t.Fatal("leaving the match row must stop routing printables to it")
	}
	m.Update(key("space")) // open → closed
	if m.StateFilter() != FilterClosed || len(asked) != 1 {
		t.Fatalf("space on the state row must cycle and refetch (state=%v asked=%v)",
			m.StateFilter(), asked)
	}
	m.Update(key("down")) // sort row
	m.Update(key("space"))
	if m.SortOrder() != SortNewest {
		t.Fatalf("space on the sort row must cycle, got %v", m.SortOrder())
	}
	m.Update(key("down")) // group row
	m.Update(key("space"))
	if !m.Grouped() {
		t.Fatal("space on the group row must toggle the grouping")
	}
	m.Update(key("down")) // label mode row
	m.Update(key("space"))
	if !m.LabelMatchAll() {
		t.Fatal("space on the label mode row must switch to all-of")
	}
	m.Update(key("space")) // back to any-of, so the label row below still reads
	m.Update(key("down"))  // first label row ("bug")
	m.Update(key("space"))
	if got := m.LabelFilter(); len(got) != 1 || got[0] != "bug" {
		t.Fatalf("space on a label row must toggle it, got %v", got)
	}
	m.Update(key("esc"))
	if m.PickerOpen() {
		t.Fatal("esc must close the overlay")
	}
	if m.StateFilter() != FilterOpen || m.SortOrder() != SortRelevance ||
		m.Grouped() || len(m.LabelFilter()) != 0 || m.LabelMatchAll() {
		t.Fatalf("esc must revert every section (state=%v sort=%v grouped=%v labels=%v)",
			m.StateFilter(), m.SortOrder(), m.Grouped(), m.LabelFilter())
	}
}

// TestLabelKeyOpensLabelSection (#2104): 'l' is a door into the same overlay,
// landing on the label section.
func TestLabelKeyOpensLabelSection(t *testing.T) {
	m := filled(t)
	m.Update(key("l"))
	if !m.PickerOpen() || m.Filtering() {
		t.Fatal("l must open the filter overlay past the match row")
	}
	m.Update(key("space")) // first label row: "bug"
	if got := m.LabelFilter(); len(got) != 1 || got[0] != "bug" {
		t.Fatalf("l must land on the label section, toggled %v", got)
	}
}

// TestChipClickClearsOneFilter (#2104): every active narrowing is a chip in
// the permanent filter row, and a click clears exactly the chip it hit.
func TestChipClickClearsOneFilter(t *testing.T) {
	m := filled(t)
	press(m, "f", "e", "x", "p", "enter")
	press(m, "l", "space", "enter") // select "bug"
	if m.Filter() == "" || len(m.LabelFilter()) != 1 {
		t.Fatal("fixture: match and one label set")
	}
	spans := m.chipSpans()
	if len(spans) != 2 {
		t.Fatalf("want two chips (match, bug), got %d", len(spans))
	}
	if cmd := m.Click(spans[1][0], 1); cmd != nil {
		cmd()
	}
	if len(m.LabelFilter()) != 0 || m.Filter() != "exp" {
		t.Fatalf("the label chip's click must clear only the label (labels=%v match=%q)",
			m.LabelFilter(), m.Filter())
	}
	if cmd := m.Click(m.chipSpans()[0][0], 1); cmd != nil {
		cmd()
	}
	if m.Filter() != "" {
		t.Fatalf("the match chip's click must clear the pattern, got %q", m.Filter())
	}
}

// TestFilterKeepsSelection (#2104): reordering or narrowing keeps the cursor
// on the issue it was on instead of yanking the view to the top.
func TestFilterKeepsSelection(t *testing.T) {
	m := filled(t)
	press(m, "j", "j") // newest order #2,#1,#3 — land on #3
	if is := m.Selected(); is == nil || is.Number != 3 {
		t.Fatalf("fixture: selected = %v, want #3", m.Selected())
	}
	press(m, "a", "a") // relevance → newest → oldest: #3 moves to row 0
	if is := m.Selected(); is == nil || is.Number != 3 {
		t.Fatalf("the sort cycle must keep the selection, got %v", m.Selected())
	}
	if m.Cursor() != 0 {
		t.Fatalf("oldest order puts #3 first, cursor = %d", m.Cursor())
	}
}

// TestLabelSelectionSticksAcrossListings (#2104): a refetch that no longer
// carries a selected label keeps the selection — the chip stays visible and
// clearable instead of vanishing silently.
func TestLabelSelectionSticksAcrossListings(t *testing.T) {
	m := filled(t)
	press(m, "l", "space", "enter") // select "bug"
	m.SetResult(forge.IssuesMsg{State: forge.IssuesOpen, Issues: []forge.Issue{
		{Number: 7, Title: "unlabelled", State: "OPEN"},
	}})
	if got := m.LabelFilter(); len(got) != 1 || got[0] != "bug" {
		t.Fatalf("the selection must survive the listing swap, got %v", got)
	}
	if m.Visible() != 0 {
		t.Fatalf("the sticky label still filters, visible = %d", m.Visible())
	}
	if !strings.Contains(m.View(), "bug") {
		t.Fatal("the sticky label's chip must render")
	}
	m.Update(key("esc"))
	if len(m.LabelFilter()) != 0 || m.Visible() != 1 {
		t.Fatalf("esc must peel the sticky label (labels=%v visible=%d)",
			m.LabelFilter(), m.Visible())
	}
}

// TestFooterKeepsMenuHint (#2104): a narrow pane drops footer segments from
// the tail but never the action-menu hint.
func TestFooterKeepsMenuHint(t *testing.T) {
	m := filled(t)
	m.SetSize(24, 12)
	v := m.View()
	lines := strings.Split(strings.TrimRight(v, "\n"), "\n")
	if !strings.Contains(lines[len(lines)-1], "m menu") {
		t.Fatalf("the footer must keep the menu hint on a narrow pane:\n%s",
			lines[len(lines)-1])
	}
}

// TestFilterRowIsPermanent (#2104): the status row is always there, so a chip
// appearing never shifts the body by a line.
func TestFilterRowIsPermanent(t *testing.T) {
	m := filled(t)
	if !strings.Contains(m.View(), "(f filters the list)") {
		t.Fatal("an unfiltered pane must show the filter hint row")
	}
	before := strings.Count(m.View(), "\n")
	press(m, "f", "x", "enter")
	if after := strings.Count(m.View(), "\n"); after != before {
		t.Fatalf("setting a filter must not change the row budget (%d != %d)", after, before)
	}
}
