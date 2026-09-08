package usagepanel

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/telemetry"
)

// now is the panel's fixed clock in every test, so the Today/Week/Month
// periods are deterministic.
var now = time.Date(2026, 9, 3, 15, 0, 0, 0, time.Local)

// dayKey is the local day key n days before the reference now.
func dayKey(n int) string { return now.AddDate(0, 0, -n).Format(telemetry.DayFormat) }

// report builds a report with one usage day per fill function.
func report(fills map[string]func(*telemetry.UsageDay)) *telemetry.Report {
	rep := &telemetry.Report{Projects: map[string]*telemetry.ProjectStat{}, Names: map[string]string{},
		Usage: map[string]*telemetry.UsageDay{}, Files: 1}
	for day, fill := range fills {
		d := telemetry.NewUsageDay()
		fill(d)
		rep.Usage[day] = d
	}
	return rep
}

// sample is a day with one of everything.
func sample(d *telemetry.UsageDay) {
	d.Commands["editor.save"] = map[string]int{telemetry.SourceKeybind: 5, telemetry.SourcePalette: 1}
	d.Commands["file.open"] = map[string]int{telemetry.SourceMenu: 2}
	d.Unbound[telemetry.ChordKey{Context: "editor[go]", Chord: "cmd+shift+z"}] = &telemetry.UnboundStat{N: 3}
	d.Unbound[telemetry.ChordKey{Context: "explorer", Chord: "cmd+alt+0"}] = &telemetry.UnboundStat{N: 1, Removed: "time.toggle"}
	d.Palette[":"] = &telemetry.PaletteStat{Picks: 3, Dismissed: 1, WithQuery: 1, NoResults: 1, OpenMs: 2000}
	d.Ops[telemetry.OpHTTPFlight] = &telemetry.OpStat{Started: 2, OK: 2, Ended: 2, TotalMs: 600, MaxMs: 400}
	d.Slow["vcs.commit"] = &telemetry.SlowStat{N: 1, TotalMs: 120, MaxMs: 120}
}

// panel builds a sized, focused panel over a report.
func panel(t *testing.T, rep *telemetry.Report) *Model {
	t.Helper()
	m := New(nil)
	m.SetSize(120, 20)
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

func TestTabsPickTheQuestion(t *testing.T) {
	m := panel(t, report(map[string]func(*telemetry.UsageDay){dayKey(0): sample}))

	rows := m.Rows()
	if len(rows) != 2 || rows[0].Name != "editor.save" || rows[0].Cells[1] != "6" || rows[0].Cells[2] != "5" {
		t.Fatalf("Commands rows = %+v, want editor.save ×6 (5 keybind) first", rows)
	}
	press(m, "tab")
	if m.Tab() != TabKeys {
		t.Fatalf("tab = %v, want Keys", m.Tab())
	}
	rows = m.Rows()
	if len(rows) != 2 || rows[0].Cells[1] != "cmd+shift+z" || rows[0].Cells[2] != "3" {
		t.Fatalf("Keys rows = %+v, want cmd+shift+z ×3 first", rows)
	}
	if !rows[1].Faint || rows[1].Cells[3] != "time.toggle" {
		t.Errorf("removed-by-config row = %+v, want faint with the default named", rows[1])
	}
	press(m, "tab")
	rows = m.Rows()
	if m.Tab() != TabPalette || len(rows) != 1 || rows[0].Cells[4] != "25%" || rows[0].Cells[7] != "1" {
		t.Fatalf("Palette rows = %+v, want ':' at 25%% with one fruitless search", rows)
	}
	press(m, "tab")
	rows = m.Rows()
	if m.Tab() != TabOps || len(rows) != 2 || rows[0].Cells[0] != "op" || rows[0].Cells[6] != "300ms" || rows[1].Cells[0] != "dispatch" {
		t.Fatalf("Ops rows = %+v, want the op (avg 300ms) then the slow dispatch", rows)
	}
	press(m, "tab")
	if m.Tab() != TabCommands {
		t.Errorf("tab wraps to Commands, got %v", m.Tab())
	}
	press(m, "shift+tab")
	if m.Tab() != TabOps {
		t.Errorf("shift+tab wraps to Ops, got %v", m.Tab())
	}
}

func TestPeriodPicksTheRange(t *testing.T) {
	m := panel(t, report(map[string]func(*telemetry.UsageDay){
		dayKey(0): func(d *telemetry.UsageDay) {
			d.Commands["editor.save"] = map[string]int{telemetry.SourceKeybind: 1}
		},
		dayKey(3): func(d *telemetry.UsageDay) {
			d.Commands["editor.save"] = map[string]int{telemetry.SourceKeybind: 2}
		},
		dayKey(20): func(d *telemetry.UsageDay) {
			d.Commands["editor.save"] = map[string]int{telemetry.SourceKeybind: 4}
		},
	}))
	if got := m.Rows()[0].Cells[1]; got != "1" {
		t.Errorf("Today = %s, want 1", got)
	}
	press(m, "p")
	if m.Period() != PeriodWeek {
		t.Fatalf("period = %v, want Week", m.Period())
	}
	if got := m.Rows()[0].Cells[1]; got != "3" {
		t.Errorf("Week = %s, want 3", got)
	}
	press(m, "p")
	if got := m.Rows()[0].Cells[1]; got != "7" {
		t.Errorf("Month = %s, want 7", got)
	}
	press(m, "P")
	if m.Period() != PeriodWeek {
		t.Errorf("P steps back to Week, got %v", m.Period())
	}
	if !strings.Contains(m.View(), "Week") || !strings.Contains(m.View(), "Commands") {
		t.Errorf("header missing the selectors:\n%s", m.View())
	}
}

func TestFilterNarrowsRows(t *testing.T) {
	m := panel(t, report(map[string]func(*telemetry.UsageDay){dayKey(0): sample}))
	press(m, "/")
	if !m.Filtering() {
		t.Fatal("/ must focus the filter row")
	}
	for _, r := range "file" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	rows := m.Rows()
	if len(rows) != 1 || rows[0].Name != "file.open" {
		t.Errorf("filtered rows = %+v, want file.open only", rows)
	}
}

func TestExportBuildsCSVForTheTab(t *testing.T) {
	m := panel(t, report(map[string]func(*telemetry.UsageDay){dayKey(0): sample}))
	m.SetTab(TabKeys)
	cmd := press(m, "e")
	if cmd == nil {
		t.Fatal("e must yield an export command")
	}
	msg, ok := cmd().(ExportMsg)
	if !ok {
		t.Fatalf("message = %T, want ExportMsg", cmd())
	}
	if msg.Label != "Keys · Today" {
		t.Errorf("label = %q", msg.Label)
	}
	lines := strings.Split(strings.TrimSpace(msg.CSV), "\n")
	if len(lines) != 3 {
		t.Fatalf("csv lines = %d, want header + 2:\n%s", len(lines), msg.CSV)
	}
	if lines[0] != "tab,period,from,to,context,chord,misses,removed_default" {
		t.Errorf("header = %q", lines[0])
	}
	want := "Keys,Today," + dayKey(0) + "," + dayKey(0) + ",editor[go],cmd+shift+z,3,"
	if lines[1] != want {
		t.Errorf("row = %q, want %q", lines[1], want)
	}
}

func TestRefreshKeyAndEmptyStates(t *testing.T) {
	m := panel(t, report(map[string]func(*telemetry.UsageDay){}))
	cmd := press(m, "r")
	if cmd == nil {
		t.Fatal("r must yield a refresh command")
	}
	if _, ok := cmd().(RefreshMsg); !ok {
		t.Errorf("message = %T, want RefreshMsg", cmd())
	}
	if !strings.Contains(m.View(), "no commands in this period") {
		t.Errorf("empty Commands text missing:\n%s", m.View())
	}
	m.SetTab(TabKeys)
	if !strings.Contains(m.View(), "no unbound chords") {
		t.Errorf("empty Keys text missing:\n%s", m.View())
	}
	m.Set(&telemetry.Report{})
	if !strings.Contains(m.View(), "no usage log yet") {
		t.Errorf("no-log text missing:\n%s", m.View())
	}
	m.SetLoading(true)
	if !strings.Contains(m.View(), "reading log") {
		t.Errorf("loading header missing:\n%s", m.View())
	}
}

func TestHeaderClicksSwitchTabAndPeriod(t *testing.T) {
	m := panel(t, report(map[string]func(*telemetry.UsageDay){dayKey(0): sample}))
	// " Usage" + 3 spaces, then "Commands Keys Palette Ops", 3 spaces, then
	// "Today Week Month".
	keysX := len(" Usage") + 3 + len("Commands") + 1
	m.Click(keysX, 0)
	if m.Tab() != TabKeys {
		t.Errorf("click on Keys → tab %v", m.Tab())
	}
	weekX := len(" Usage") + 3 + len("Commands Keys Palette Ops") + 3 + len("Today") + 1
	m.Click(weekX, 0)
	if m.Period() != PeriodWeek {
		t.Errorf("click on Week → period %v", m.Period())
	}
	m.Click(0, headerRows+1)
	if m.Cursor() != 1 {
		t.Errorf("click on the second row → cursor %d", m.Cursor())
	}
}

func TestNavigationKeepsCursorInRange(t *testing.T) {
	m := panel(t, report(map[string]func(*telemetry.UsageDay){dayKey(0): sample}))
	press(m, "j")
	if m.Cursor() != 1 {
		t.Errorf("j → cursor %d, want 1", m.Cursor())
	}
	press(m, "k")
	if m.Cursor() != 0 {
		t.Errorf("k → cursor %d, want 0", m.Cursor())
	}
	press(m, "G")
	if m.Cursor() != 1 {
		t.Errorf("G → cursor %d, want the last row", m.Cursor())
	}
	m.Wheel(5)
	if m.Cursor() < 0 || m.Cursor() > 1 {
		t.Errorf("wheel left the cursor at %d", m.Cursor())
	}
}
