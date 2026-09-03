package timepanel

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/telemetry"
)

// now is the panel's fixed clock in every test, so the Today/Week/Month
// ranges are deterministic.
var now = time.Date(2026, 9, 3, 15, 0, 0, 0, time.Local)

// dayKey is the local day key n days before the reference now.
func dayKey(n int) string { return now.AddDate(0, 0, -n).Format(telemetry.DayFormat) }

// report builds a report holding the given per-token, per-day active times.
func report(t *testing.T, names map[string]string, entries ...entry) *telemetry.Report {
	t.Helper()
	rep := &telemetry.Report{Projects: map[string]*telemetry.ProjectStat{}, Names: map[string]string{}, Files: 1}
	for _, e := range entries {
		ps := rep.Projects[e.token]
		if ps == nil {
			ps = &telemetry.ProjectStat{Token: e.token, Days: map[string]*telemetry.DayStat{}}
			rep.Projects[e.token] = ps
		}
		ps.Days[e.day] = &telemetry.DayStat{Active: e.active, Sessions: e.sessions, Commands: e.cmds}
	}
	for token, name := range names {
		rep.Names[token] = name
	}
	return rep
}

type entry struct {
	token    string
	day      string
	active   time.Duration
	sessions int
	cmds     map[string]int
}

// panel builds a sized, focused panel over a report.
func panel(t *testing.T, rep *telemetry.Report) *Model {
	t.Helper()
	m := New(nil)
	m.SetSize(100, 20)
	m.SetFocused(true)
	m.SetNow(func() time.Time { return now })
	m.Set(rep)
	return &m
}

func press(m *Model, key string) tea.Cmd {
	return m.Update(tea.KeyPressMsg{Code: firstRune(key), Text: key})
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

func TestTabsPickTheRange(t *testing.T) {
	rep := report(t, map[string]string{"aaa": "ike"},
		entry{token: "aaa", day: dayKey(0), active: time.Hour, sessions: 1},
		entry{token: "aaa", day: dayKey(3), active: 2 * time.Hour, sessions: 2},
		entry{token: "aaa", day: dayKey(20), active: 3 * time.Hour, sessions: 3},
	)
	m := panel(t, rep)

	if got := m.Total(); got != time.Hour {
		t.Errorf("Today total = %v, want 1h", got)
	}
	m.SetTab(TabWeek)
	if got := m.Total(); got != 3*time.Hour {
		t.Errorf("Week total = %v, want 3h", got)
	}
	m.SetTab(TabMonth)
	if got := m.Total(); got != 6*time.Hour {
		t.Errorf("Month total = %v, want 6h", got)
	}
	if got := m.Rows(); got != 1 {
		t.Errorf("rows = %d, want 1", got)
	}
	if got := m.Summaries()[0].Sessions; got != 6 {
		t.Errorf("month sessions = %d, want 6", got)
	}
}

func TestTabKeyCyclesForwardAndBack(t *testing.T) {
	m := panel(t, report(t, nil))
	press(m, "tab")
	if m.Tab() != TabWeek {
		t.Fatalf("tab = %v, want Week", m.Tab())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.Tab() != TabToday {
		t.Errorf("shift+tab: tab = %v, want Today", m.Tab())
	}
	press(m, "l")
	press(m, "l")
	if m.Tab() != TabMonth {
		t.Errorf("l l: tab = %v, want Month", m.Tab())
	}
	press(m, "l")
	if m.Tab() != TabToday {
		t.Errorf("l wraps to %v, want Today", m.Tab())
	}
}

func TestRowsSortedByActiveAndTopCommandsCapped(t *testing.T) {
	rep := report(t, map[string]string{"aaa": "ike", "bbb": "site"},
		entry{token: "aaa", day: dayKey(0), active: time.Hour, sessions: 1, cmds: map[string]int{
			"a": 9, "b": 8, "c": 7, "d": 6, "e": 5, "f": 4, "g": 3,
		}},
		entry{token: "bbb", day: dayKey(0), active: 3 * time.Hour, sessions: 1},
	)
	m := panel(t, rep)
	rows := m.Summaries()
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].Name != "site" {
		t.Errorf("first row = %q, want the busiest project", rows[0].Name)
	}
	if got := len(rows[1].Commands); got != TopCommands {
		t.Errorf("commands = %d, want %d", got, TopCommands)
	}
	if rows[1].Commands[0].ID != "a" {
		t.Errorf("top command = %q, want the most frequent", rows[1].Commands[0].ID)
	}
}

func TestFilterNarrowsRows(t *testing.T) {
	rep := report(t, map[string]string{"aaa": "ike", "bbb": "site"},
		entry{token: "aaa", day: dayKey(0), active: time.Hour, sessions: 1},
		entry{token: "bbb", day: dayKey(0), active: 3 * time.Hour, sessions: 1},
	)
	m := panel(t, rep)
	press(m, "/")
	if !m.Filtering() {
		t.Fatal("/ did not focus the filter row")
	}
	for _, r := range "ike" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if got := m.Rows(); got != 1 {
		t.Fatalf("rows = %d, want 1", got)
	}
	if m.Summaries()[0].Name != "ike" {
		t.Errorf("row = %q, want ike", m.Summaries()[0].Name)
	}
	// The shared find chord is the second doorway to the same row (#2409).
	m2 := panel(t, rep)
	if !m2.OpenSearch() || !m2.Filtering() {
		t.Error("OpenSearch did not focus the filter row")
	}
}

func TestFilterProjectFieldMatches(t *testing.T) {
	rep := report(t, map[string]string{"aaa": "ike", "bbb": "site"},
		entry{token: "aaa", day: dayKey(0), active: time.Hour, sessions: 1},
		entry{token: "bbb", day: dayKey(0), active: 3 * time.Hour, sessions: 1},
	)
	m := panel(t, rep)
	m.filter.SetText("project:sit")
	m.Refresh()
	if got := m.Rows(); got != 1 || m.Summaries()[0].Name != "site" {
		t.Errorf("rows = %d (%v), want just site", got, m.Summaries())
	}
}

func TestExportEmitsCSVForTheCurrentView(t *testing.T) {
	rep := report(t, map[string]string{"aaa": "ike"},
		entry{token: "aaa", day: dayKey(0), active: 90 * time.Minute, sessions: 2,
			cmds: map[string]int{"editor.save": 4}},
	)
	m := panel(t, rep)
	cmd := press(m, "e")
	if cmd == nil {
		t.Fatal("e produced no command")
	}
	msg, ok := cmd().(ExportMsg)
	if !ok {
		t.Fatalf("message = %T, want ExportMsg", cmd())
	}
	if msg.Label != "Today" {
		t.Errorf("label = %q, want Today", msg.Label)
	}
	lines := strings.Split(strings.TrimRight(msg.CSV, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("csv lines = %d, want header + 1 row:\n%s", len(lines), msg.CSV)
	}
	if !strings.HasPrefix(lines[0], "range,from,to,project,token,active,active_seconds,sessions,top_commands") {
		t.Errorf("header = %q", lines[0])
	}
	want := []string{"Today", "ike", "aaa", "1h 30m", "5400", "2", "editor.save=4"}
	for _, w := range want {
		if !strings.Contains(lines[1], w) {
			t.Errorf("row %q missing %q", lines[1], w)
		}
	}
}

func TestCSVQuotesAwkwardNames(t *testing.T) {
	rep := report(t, map[string]string{"aaa": `we, "the" team`},
		entry{token: "aaa", day: dayKey(0), active: time.Minute, sessions: 1},
	)
	m := panel(t, rep)
	if !strings.Contains(m.CSV(), `"we, ""the"" team"`) {
		t.Errorf("csv did not quote the name:\n%s", m.CSV())
	}
}

func TestViewShowsRowsTabsAndChart(t *testing.T) {
	rep := report(t, map[string]string{"aaa": "ike"},
		entry{token: "aaa", day: dayKey(0), active: 90 * time.Minute, sessions: 2,
			cmds: map[string]int{"editor.save": 4}},
		entry{token: "aaa", day: dayKey(2), active: 30 * time.Minute, sessions: 1},
	)
	m := panel(t, rep)
	m.SetTab(TabWeek)
	out := m.View()
	for _, want := range []string{"Time", "Today", "Week", "Month", "ike", "2h 0m", "editor.save×4", "per day", "█"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, dayKey(0)) {
		t.Errorf("chart missing today's day key:\n%s", out)
	}
}

func TestEmptyStates(t *testing.T) {
	m := panel(t, &telemetry.Report{Projects: map[string]*telemetry.ProjectStat{}, Files: 0})
	if !strings.Contains(m.View(), "no usage log yet") {
		t.Errorf("view = %s", m.View())
	}
	m2 := panel(t, report(t, map[string]string{"aaa": "ike"},
		entry{token: "aaa", day: dayKey(10), active: time.Hour, sessions: 1}))
	if !strings.Contains(m2.View(), "no recorded time in this range") {
		t.Errorf("view = %s", m2.View())
	}
}

func TestShortPaneDropsTheChart(t *testing.T) {
	rep := report(t, map[string]string{"aaa": "ike"},
		entry{token: "aaa", day: dayKey(0), active: time.Hour, sessions: 1})
	m := panel(t, rep)
	m.SetSize(80, 6)
	if strings.Contains(m.View(), "per day") {
		t.Errorf("short pane still drew the chart:\n%s", m.View())
	}
	if m.bodyHeight() < 1 {
		t.Errorf("bodyHeight = %d", m.bodyHeight())
	}
}

func TestClickSelectsRowAndTab(t *testing.T) {
	rep := report(t, map[string]string{"aaa": "ike", "bbb": "site"},
		entry{token: "aaa", day: dayKey(0), active: time.Hour, sessions: 1},
		entry{token: "bbb", day: dayKey(0), active: 3 * time.Hour, sessions: 1},
	)
	m := panel(t, rep)
	m.Click(0, 2+1) // second list row
	if m.Cursor() != 1 {
		t.Errorf("cursor = %d, want 1", m.Cursor())
	}
	// The header's tab bar starts after " Time" plus three spaces.
	m.Click(len(" Time")+3+len("Today")+1, 0)
	if m.Tab() != TabWeek {
		t.Errorf("tab = %v, want Week", m.Tab())
	}
}

func TestRefreshKeyAsksTheRootModel(t *testing.T) {
	m := panel(t, report(t, nil))
	cmd := press(m, "r")
	if cmd == nil {
		t.Fatal("r produced no command")
	}
	if _, ok := cmd().(RefreshMsg); !ok {
		t.Fatalf("message = %T, want RefreshMsg", cmd())
	}
}

func TestBarScalesAgainstTheBusiestDay(t *testing.T) {
	if got := bar(0, time.Hour, 10); strings.Contains(got, "█") {
		t.Errorf("zero bar = %q, want blanks", got)
	}
	if got := bar(time.Hour, time.Hour, 10); got != strings.Repeat("█", 10) {
		t.Errorf("full bar = %q", got)
	}
	if got := bar(time.Second, time.Hour, 10); !strings.HasPrefix(got, "█") {
		t.Errorf("tiny non-zero bar = %q, want at least one block", got)
	}
	if got := len([]rune(bar(30*time.Minute, time.Hour, 10))); got != 10 {
		t.Errorf("bar width = %d, want 10", got)
	}
}
