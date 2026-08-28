package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/localhistory"
)

// projectHistorySeed writes two files, records snapshots for both (b.txt
// newest) and opens the project-wide timeline over them.
func projectHistorySeed(t *testing.T) (Model, string, string) {
	t.Helper()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("live\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := newSized()
	m.lhStore.Record(a, []byte("a1\n"))
	m.lhStore.Record(b, []byte("b1\n"))
	m.lhStore.Record(a, []byte("a2\n"))
	m.lhStore.Record(b, []byte("b2\n"))
	// Through the message the palette command dispatches, so the seed covers
	// the command wiring too.
	tm, _ := m.Update(ProjectHistoryMsg{})
	m = tm.(Model)
	if !m.projectHistoryOpen() {
		t.Fatal("the project-wide timeline did not open")
	}
	return m, a, b
}

// pressProjectHistory feeds one key into the open timeline through the root
// Update, so the test exercises the real key routing too.
func pressProjectHistory(t *testing.T, m Model, key string) Model {
	t.Helper()
	press := tea.KeyPressMsg{Code: rune(key[0]), Text: key}
	switch key {
	case "enter":
		press = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		press = tea.KeyPressMsg{Code: tea.KeyEscape}
	case "ctrl+l":
		press = tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl}
	case "down":
		press = tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		press = tea.KeyPressMsg{Code: tea.KeyUp}
	}
	tm, cmd := m.Update(press)
	return drainCmd(tm.(Model), cmd)
}

// projectHistoryBody renders the panel's current body.
func projectHistoryBody(t *testing.T, m Model) string {
	t.Helper()
	c, ok := m.shell.Content().(interface{ Render(int) string })
	if !ok {
		t.Fatalf("shell content %T does not render", m.shell.Content())
	}
	return c.Render(120)
}

// TestProjectHistoryListsAcrossFilesNewestFirst covers the headline criterion:
// the timeline mixes every file's snapshots into one newest-first list under a
// day heading.
func TestProjectHistoryListsAcrossFilesNewestFirst(t *testing.T) {
	m, a, b := projectHistorySeed(t)
	if len(m.ph.view) != 4 {
		t.Fatalf("timeline = %d rows, want 4 (two files × two snapshots)", len(m.ph.view))
	}
	wantPaths := []string{b, a, b, a}
	for i, sn := range m.ph.view {
		if sn.Path != wantPaths[i] {
			t.Fatalf("row %d = %s, want %s (newest first, across files)", i, sn.Path, wantPaths[i])
		}
		if i > 0 && sn.Time.After(m.ph.view[i-1].Time) {
			t.Fatalf("row %d is newer than row %d — the list is not newest-first", i, i-1)
		}
	}
	body := projectHistoryBody(t, m)
	if !strings.Contains(body, "Today") {
		t.Fatalf("body has no day heading for freshly recorded snapshots:\n%s", body)
	}
	if !strings.Contains(body, "a.txt") || !strings.Contains(body, "b.txt") {
		t.Fatalf("body does not list both files:\n%s", body)
	}
}

// TestProjectHistoryGroupsByDay: rows fall under Today / Yesterday / dated
// headings, in that order, with each heading rendered once.
func TestProjectHistoryGroupsByDay(t *testing.T) {
	m, _, _ := projectHistorySeed(t)
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.Local)
	m.ph.now = now
	m.ph.all = []localhistory.Snapshot{
		{Path: "/p/today.go", Entry: localhistory.Entry{Time: now.Add(-time.Hour), Hash: "h1"}},
		{Path: "/p/yesterday.go", Entry: localhistory.Entry{Time: now.Add(-26 * time.Hour), Hash: "h2"}},
		{Path: "/p/older.go", Entry: localhistory.Entry{Time: now.Add(-72 * time.Hour), Hash: "h3"}},
	}
	m.refreshProjectHistory()

	body := projectHistoryBody(t, m)
	iToday := strings.Index(body, "Today")
	iYesterday := strings.Index(body, "Yesterday")
	iOlder := strings.Index(body, "Fri 2026-08-21")
	if iToday < 0 || iYesterday < 0 || iOlder < 0 {
		t.Fatalf("missing day headings (today %d, yesterday %d, older %d):\n%s",
			iToday, iYesterday, iOlder, body)
	}
	if !(iToday < iYesterday && iYesterday < iOlder) {
		t.Fatalf("day headings out of order:\n%s", body)
	}
	if n := strings.Count(body, "Yesterday"); n != 1 {
		t.Fatalf("the Yesterday heading renders %d times, want 1:\n%s", n, body)
	}
}

// TestProjectHistoryFilterNarrowsByPath: typing filters the rows by path
// substring, esc clears the filter before it closes the panel.
func TestProjectHistoryFilterNarrowsByPath(t *testing.T) {
	m, _, b := projectHistorySeed(t)
	for _, k := range []string{"b", ".", "t"} {
		m = pressProjectHistory(t, m, k)
	}
	if len(m.ph.view) != 2 {
		t.Fatalf("filter %q left %d rows, want the 2 of b.txt", m.ph.search.Query(), len(m.ph.view))
	}
	for _, sn := range m.ph.view {
		if sn.Path != b {
			t.Fatalf("filtered row = %s, want only %s", sn.Path, b)
		}
	}
	body := projectHistoryBody(t, m)
	if !strings.Contains(body, "filter: b.t") || strings.Contains(body, "a.txt") {
		t.Fatalf("body does not reflect the filter:\n%s", body)
	}

	// esc clears the filter first; only the second esc closes.
	m = pressProjectHistory(t, m, "esc")
	if !m.projectHistoryOpen() {
		t.Fatal("the first esc closed the panel instead of clearing the filter")
	}
	if len(m.ph.view) != 4 {
		t.Fatalf("esc left %d rows, want all 4 back", len(m.ph.view))
	}
	m = pressProjectHistory(t, m, "esc")
	if m.projectHistoryOpen() {
		t.Fatal("the second esc did not close the panel")
	}
}

// TestProjectHistoryLazyLoadsPages: a long history opens on one page and grows
// on ctrl+l, saying how much is reachable — an unbounded render of thousands
// of rows is what the cap exists to avoid.
func TestProjectHistoryLazyLoadsPages(t *testing.T) {
	m, _, _ := projectHistorySeed(t)
	now := time.Now()
	m.ph.now = now
	m.ph.all = nil
	for i := range projectHistoryPage * 2 {
		m.ph.all = append(m.ph.all, localhistory.Snapshot{
			Path:  fmt.Sprintf("/p/f%03d.go", i),
			Entry: localhistory.Entry{Time: now.Add(-time.Duration(i) * time.Minute), Hash: fmt.Sprintf("h%d", i)},
		})
	}
	m.ph.shown = projectHistoryPage
	m.refreshProjectHistory()

	if got := m.reachableProjectHistory(); got != projectHistoryPage {
		t.Fatalf("reachable rows = %d, want the first page (%d)", got, projectHistoryPage)
	}
	body := projectHistoryBody(t, m)
	if !strings.Contains(body, "ctrl+l loads more") {
		t.Fatalf("body does not say that rows are held back:\n%s", body)
	}
	m = pressProjectHistory(t, m, "ctrl+l")
	if got := m.reachableProjectHistory(); got != 2*projectHistoryPage {
		t.Fatalf("reachable rows after ctrl+l = %d, want %d", got, 2*projectHistoryPage)
	}
	// The rendered block still fits the shell's box: the panel windows its
	// rows instead of rendering the whole history.
	if lines := strings.Count(projectHistoryBody(t, m), "\n") + 1; lines > m.height-10 {
		t.Fatalf("body renders %d lines into a %d-row terminal", lines, m.height)
	}
}

// TestProjectHistoryWindowFollowsSelection: walking down a long list scrolls
// the rendered window with the cursor (and reveals the next page on the way),
// so the selection is never rendered off the panel.
func TestProjectHistoryWindowFollowsSelection(t *testing.T) {
	m, _, _ := projectHistorySeed(t)
	now := time.Now()
	m.ph.now = now
	m.ph.all = nil
	for i := range 120 {
		m.ph.all = append(m.ph.all, localhistory.Snapshot{
			// A day boundary every 40 rows, so the window has to budget for
			// the headings between them too.
			Path:  fmt.Sprintf("/p/f%03d.go", i),
			Entry: localhistory.Entry{Time: now.Add(-time.Duration(i) * 45 * time.Minute), Hash: fmt.Sprintf("h%d", i)},
		})
	}
	m.refreshProjectHistory()

	for range 60 {
		m = pressProjectHistory(t, m, "down")
	}
	if m.ph.sel != 60 {
		t.Fatalf("selection after 60 steps = %d, want 60", m.ph.sel)
	}
	want := displayPath(m.ph.view[m.ph.sel].Path)
	body := projectHistoryBody(t, m)
	if !strings.Contains(body, "▍ "+want) {
		t.Fatalf("the selected row %q is not in the rendered window:\n%s", want, body)
	}
	if m.ph.top == 0 {
		t.Fatal("the window did not scroll with the selection")
	}
	if lines := strings.Count(body, "\n") + 1; lines > m.height-10 {
		t.Fatalf("body renders %d lines into a %d-row terminal", lines, m.height)
	}
}

// TestProjectHistoryHandoffOpensPerFilePanel covers the second criterion:
// enter lands in the per-file panel at the picked snapshot — with the file
// opened, so the panel's restore works from there.
func TestProjectHistoryHandoffOpensPerFilePanel(t *testing.T) {
	m, a, _ := projectHistorySeed(t)
	// Row 3 is a.txt's older snapshot ("a1"), and nothing is open yet.
	m.ph.sel = 3
	m.refreshProjectHistory()
	want := m.ph.view[3]
	if want.Path != a {
		t.Fatalf("row 3 = %s, want %s", want.Path, a)
	}

	m = pressProjectHistory(t, m, "enter")
	if m.projectHistoryOpen() {
		t.Fatal("the timeline stayed open after the handoff")
	}
	if !m.localHistoryOpen() {
		t.Fatal("the per-file panel did not open")
	}
	if m.lhPath != a {
		t.Fatalf("per-file panel shows %s, want %s", m.lhPath, a)
	}
	if m.lhSel != 1 {
		t.Fatalf("panel selection = %d, want 1 (the older snapshot the row named)", m.lhSel)
	}
	if got := m.lhEntries[m.lhSel]; got.Hash != want.Hash || !got.Time.Equal(want.Time) {
		t.Fatalf("selected entry = %+v, want the picked snapshot %+v", got, want.Entry)
	}
	ed := m.editorForPath(a)
	if ed == nil {
		t.Fatal("the handoff did not open the file, so restore would have nothing to edit")
	}

	// Restore works from the per-file panel exactly as it does when the panel
	// is opened from the file itself.
	tm, _ := m.updateLocalHistoryPicker(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m = tm.(Model)
	if got := m.editorForPath(a).Text(); got != "a1" {
		t.Fatalf("buffer after restore = %q, want %q", got, "a1")
	}
	if data, _ := os.ReadFile(a); string(data) != "live\n" {
		t.Fatalf("restore touched the file on disk: %q", data)
	}
}

// TestProjectHistoryClickRow: a click selects a row, a second click on it
// hands off, and the day headings are inert (#2275).
func TestProjectHistoryClickRow(t *testing.T) {
	m, _, _ := projectHistorySeed(t)
	lines := strings.Split(projectHistoryBody(t, m), "\n")
	head, row := -1, -1
	for i, idx := range m.ph.rows {
		if idx < 0 && head < 0 && i < len(lines) && lines[i] == "Today" {
			head = i
		}
		if idx == 1 {
			row = i
		}
	}
	if head < 0 || row < 0 {
		t.Fatalf("click map has no heading/row pair: %v", m.ph.rows)
	}
	tm, _ := m.projectHistoryClickRow(head)
	m = tm.(Model)
	if m.ph.sel != 0 || !m.projectHistoryOpen() {
		t.Fatalf("a click on a day heading changed the panel (sel=%d, open=%v)",
			m.ph.sel, m.projectHistoryOpen())
	}
	tm, _ = m.projectHistoryClickRow(row)
	m = tm.(Model)
	if m.ph.sel != 1 || !m.projectHistoryOpen() {
		t.Fatalf("the first click must only select (sel=%d, open=%v)", m.ph.sel, m.projectHistoryOpen())
	}
	tm, cmd := m.projectHistoryClickRow(row)
	m = drainCmd(tm.(Model), cmd)
	if m.projectHistoryOpen() || !m.localHistoryOpen() {
		t.Fatal("the second click on the selected row did not hand off to the per-file panel")
	}
}

// TestProjectHistoryNeedsSnapshots: an empty store notifies instead of opening
// an empty list, like the per-file panel.
func TestProjectHistoryNeedsSnapshots(t *testing.T) {
	m := newSized()
	m.openProjectHistory()
	if m.projectHistoryOpen() {
		t.Fatal("the timeline opened with no snapshots at all")
	}
}
