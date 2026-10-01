package tracepanel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/agenttrace"
)

// history_test.go covers the pane half of #2860: the session picker (list,
// filter, enter, esc), the read-only stored-session view with its header,
// and the rewind marker in both views.

var (
	tHist1 = time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)
	tHist2 = time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)
)

func historyItems() []HistoryItem {
	return []HistoryItem{
		{ID: "11111111-1111-4111-8111-111111111111", StartedAt: tHist2, EndedAt: tHist2.Add(2 * time.Minute), Turns: 2, Files: 3, Prompt: "Add a greeting to main.go", Live: true},
		{ID: "22222222-2222-4222-8222-222222222222", StartedAt: tHist1, EndedAt: tHist1.Add(65 * time.Minute), Ended: true, Turns: 5, Files: 1, Prompt: "Refactor the parser"},
	}
}

func TestHistoryKeyAsksForTheList(t *testing.T) {
	m := panel(t)
	// Without a session the picker is still reachable.
	m.SetNoSession("/w", nil)
	if _, ok := send(m, "s").(HistoryMsg); !ok {
		t.Fatal("s in the empty state must ask for the history")
	}
	s := fixture(t, true)
	m.Set(agenttrace.BuildTree(s), info(s))
	if _, ok := send(m, "s").(HistoryMsg); !ok {
		t.Fatal("s on a session must ask for the history")
	}
	if !strings.Contains(plain(m.View()), "s sessions") {
		t.Fatal("the hint must name s")
	}
}

func TestPickerListsFiltersOpensAndCloses(t *testing.T) {
	m := panel(t)
	s := fixture(t, true)
	m.Set(agenttrace.BuildTree(s), info(s))
	m.OpenPicker(historyItems())
	if !m.PickerOpen() {
		t.Fatal("picker must open")
	}
	view := plain(m.View())
	for _, want := range []string{"sessions · 2 stored", "● " + tHist2.Local().Format("2006-01-02 15:04"), "live", "2 turns", "3 files", "Add a greeting", "1h05m", "5 turns", "1 file ", "Refactor the parser", "enter show"} {
		if !strings.Contains(view, want) {
			t.Errorf("picker lacks %q:\n%s", want, view)
		}
	}
	if cur := m.PickerCurrent(); cur == nil || !cur.Live {
		t.Fatalf("cursor must start on the newest row, got %+v", cur)
	}
	// Filter through the shared line search.
	send(m, "/")
	for _, r := range "refac" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if rows := m.PickerRows(); len(rows) != 1 || rows[0] != "22222222-2222-4222-8222-222222222222" {
		t.Fatalf("filtered rows = %v", rows)
	}
	if !strings.Contains(plain(m.View()), "/refac") {
		t.Fatal("the filter line must show the query")
	}
	// Enter keeps the filter; enter again opens the stored session.
	send(m, "enter")
	msg, ok := send(m, "enter").(ShowHistoryMsg)
	if !ok || msg.ID != "22222222-2222-4222-8222-222222222222" {
		t.Fatalf("enter = %#v", msg)
	}
	if m.PickerOpen() {
		t.Fatal("enter must close the picker")
	}
	// A no-match query says so; esc drops the query first, then closes.
	m.OpenPicker(historyItems())
	send(m, "/")
	m.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
	if !strings.Contains(plain(m.View()), "no session matches") {
		t.Fatal("a miss must be reported")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.PickerOpen() || len(m.PickerRows()) != 2 {
		t.Fatal("esc must drop the query and keep the picker")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.PickerOpen() {
		t.Fatal("esc without a query must close the picker")
	}
	// Enter on the live row returns to the live session.
	m.OpenPicker(historyItems())
	if _, ok := send(m, "enter").(LiveMsg); !ok {
		t.Fatal("enter on the live row must ask for the live session")
	}
	// Navigation and the mouse: down selects the second row, a double
	// click opens it, the wheel scrolls.
	m.OpenPicker(historyItems())
	send(m, "down")
	if cur := m.PickerCurrent(); cur == nil || cur.Live {
		t.Fatalf("down = %+v", cur)
	}
	now := time.Now()
	m.SetNow(func() time.Time { return now })
	if cmd := m.Click(5, 2); cmd != nil {
		t.Fatal("a single click only selects")
	}
	if cur := m.PickerCurrent(); cur == nil || !cur.Live {
		t.Fatalf("click on row 0 = %+v", cur)
	}
	cmd := m.Click(5, 2)
	if cmd == nil {
		t.Fatal("a double click must open")
	}
	if _, ok := cmd().(LiveMsg); !ok {
		t.Fatal("double click on the live row = LiveMsg")
	}
	m.OpenPicker(historyItems())
	m.Wheel(3)
	if m.PickerCurrent() == nil {
		t.Fatal("the wheel must leave a valid cursor")
	}
}

func TestStoredSessionHeaderAndReturn(t *testing.T) {
	m := panel(t)
	s := fixture(t, true)
	inf := info(s)
	inf.History = HistoryLabel(tHist2)
	m.SetStored(agenttrace.BuildTree(s), agenttrace.BuildPath(s), inf)
	if !m.Stored() || !m.HasSession() {
		t.Fatal("SetStored must show a stored session")
	}
	view := plain(m.View())
	if !strings.Contains(view, "history · "+tHist2.Local().Format("2006-01-02 15:04")) || !strings.Contains(view, "esc/r back to live") {
		t.Fatalf("header =\n%s", view)
	}
	if strings.Contains(view, "not read yet") || strings.Contains(view, "read ") {
		t.Fatal("a stored session shows no read status")
	}
	if !strings.Contains(view, "#1 Add a greeting") {
		t.Fatalf("stored rows missing:\n%s", view)
	}
	// Opens on the first turn, not following the tail.
	if cur := m.Current(); cur == nil || cur.Key != "t1" || m.Following() {
		t.Fatalf("stored tree opens on %+v following=%v", cur, m.Following())
	}
	if _, ok := send(m, "esc").(LiveMsg); !ok {
		t.Fatal("esc on a stored session must return to the live one")
	}
	if _, ok := send(m, "r").(LiveMsg); !ok {
		t.Fatal("r on a stored session must return to the live one")
	}
	// The graph view of a stored session starts on the first box.
	m.SetViewMode(ViewGraph)
	m.SetStored(agenttrace.BuildTree(s), agenttrace.BuildPath(s), inf)
	if cur := m.CurrentStop(); cur == nil || cur.Key != "t1" {
		t.Fatalf("stored graph opens on %+v", cur)
	}
	// Back on the live session the header is the usual one.
	m.Reset()
	m.Set(agenttrace.BuildTree(s), info(s))
	if m.Stored() || !strings.Contains(plain(m.View()), "11111111 · scan") {
		t.Fatal("Reset + Set must return to the live header")
	}
}

// rewindFixture parses the rewind transcript.
func rewindFixture(t *testing.T) *agenttrace.Session {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "agenttrace", "testdata", "rewind.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	p := agenttrace.NewParser()
	p.ReadFile = func(string) ([]byte, error) { return nil, os.ErrNotExist }
	for _, l := range strings.Split(string(data), "\n") {
		p.Line([]byte(l))
	}
	return p.Session()
}

func TestRewindMarkerInTreeAndGraph(t *testing.T) {
	m := panel(t)
	s := rewindFixture(t)
	m.Set(agenttrace.BuildTree(s), info(s))
	rows := m.Rows()
	// The marker is a root that stays folded; the live turns show.
	if strings.Join(rows, ",") != "t1,rw8,t3, e9,  e10,   e10/f0,  e11,   e11/f0, e12" {
		t.Fatalf("rows = %v", rows)
	}
	view := plain(m.View())
	if !strings.Contains(view, "↶ rewound") || !strings.Contains(view, "1 turn abandoned") {
		t.Fatalf("tree lacks the rewind marker:\n%s", view)
	}
	// Expanding the marker reveals the abandoned turn.
	if !m.Select("rw8") {
		t.Fatal("the marker must be selectable")
	}
	send(m, "space")
	if rows := m.Rows(); !strings.Contains(strings.Join(rows, ","), "rw8, rw8/t2") {
		t.Fatalf("expanded rows = %v", rows)
	}

	// Graph: a faint box with the ↶ glyph; space lists the branch.
	m.SetViewMode(ViewGraph)
	m.SetPath(agenttrace.BuildPath(s))
	view = plain(m.View())
	if !strings.Contains(view, "┌↶") || !strings.Contains(view, "↶ rewound") {
		t.Fatalf("graph lacks the rewind box:\n%s", view)
	}
	if !m.Select("rw8") {
		t.Fatal("the rewind box must be selectable")
	}
	send(m, "space")
	if m.Expanded() != "rw8" {
		t.Fatalf("expanded = %q", m.Expanded())
	}
	view = plain(m.View())
	for _, want := range []string{"abandoned branch · 1 turn abandoned", "? #2 Now delete everything in util.go", "util.go · edit :1 +0 −2", "✓ util.go is now empty."} {
		if !strings.Contains(view, want) {
			t.Errorf("detail block lacks %q:\n%s", want, view)
		}
	}
	// Enter on the marker toggles it too; the live path is unchanged.
	send(m, "enter")
	if m.Expanded() != "" {
		t.Fatal("enter must collapse the marker")
	}
	keys := []string{}
	for _, st := range m.Path() {
		keys = append(keys, st.Key)
	}
	if strings.Join(keys, ",") != "t1,e2/f0,t1/end,rw8,t3,e10/f0,t3/end" {
		t.Fatalf("path keys = %v", keys)
	}
}
