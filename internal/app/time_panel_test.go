package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/registry"
	"ike/internal/telemetry"
	"ike/internal/timepanel"
)

// time_panel_test.go covers the Time tool window's app half (#2426): the
// toggle state machine, the background read of the usage log, the CSV export
// and the opt-in status-line segment.

// writeUsageLog drops one synthetic session file into the config dir's
// telemetry directory, recording active time in the project rooted at wd.
func writeUsageLog(t *testing.T, cfgDir, wd string, active time.Duration) {
	t.Helper()
	dir := filepath.Join(cfgDir, "telemetry")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	token := telemetry.ProjectToken(wd)
	start := time.Now().Add(-2 * time.Hour)
	stamp := func(d time.Duration) string {
		return start.Add(d).UTC().Format("2006-01-02T15:04:05.000Z07:00")
	}
	events := []telemetry.Event{
		{V: 4, TS: stamp(0), SID: "t", Type: telemetry.TypeSession,
			Data: map[string]string{"app": "0", "os": "test", "project": token}},
		{V: 4, TS: stamp(time.Minute), SID: "t", Type: telemetry.TypeCommand,
			Data: map[string]string{"id": "editor.save", "source": telemetry.SourceKeybind}},
		{V: 4, TS: stamp(time.Hour), SID: "t", Type: telemetry.TypeProjectLeave,
			Data: map[string]string{"project": token, "reason": "quit",
				"ms": strconv.FormatInt(active.Milliseconds(), 10)}},
	}
	var buf []byte
	for _, e := range events {
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	if err := os.WriteFile(filepath.Join(dir, "20260903T090000-t.jsonl"), buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

// timeApp builds a sized app over an isolated config dir, returning both.
func timeApp(t *testing.T) (Model, string) {
	t.Helper()
	cfgDir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", cfgDir)
	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return out.(Model), cfgDir
}

func TestTimeToggleLifecycle(t *testing.T) {
	m, _ := timeApp(t)
	before := m.activeWS().Panes.Focused()

	out, _ := m.Update(TimeToggleMsg{})
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.TimeKey) || m.activeWS().Panes.Focused() != pane.TimeKey {
		t.Fatalf("first toggle must open + focus the panel (focus=%q)", m.activeWS().Panes.Focused())
	}

	out, _ = m.Update(TimeToggleMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != before {
		t.Fatalf("focus = %q, want %q", m.activeWS().Panes.Focused(), before)
	}

	out, _ = m.Update(TimeToggleMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != pane.TimeKey {
		t.Fatal("third toggle must re-focus the panel")
	}
}

func TestTimeReadFillsPanelWithResolvedName(t *testing.T) {
	m, cfgDir := timeApp(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	writeUsageLog(t, cfgDir, wd, 42*time.Minute)

	out, _ := m.Update(TimeToggleMsg{})
	m = out.(Model)
	// The toggle's command does the read; run it and land the result.
	cmd := m.timeReadCmd()
	msg, ok := cmd().(timeReportMsg)
	if !ok {
		t.Fatalf("message = %T, want timeReportMsg", cmd())
	}
	out, _ = m.Update(msg)
	m = out.(Model)

	p := m.timePanel()
	if p == nil {
		t.Fatal("no panel")
	}
	rows := p.Summaries()
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want one project", rows)
	}
	if rows[0].Active != 42*time.Minute {
		t.Errorf("active = %v, want 42m", rows[0].Active)
	}
	// The current project is not in the recent-projects history yet, so its
	// name comes from the working directory rather than reading "(unknown)".
	if rows[0].Name != filepath.Base(wd) {
		t.Errorf("name = %q, want %q", rows[0].Name, filepath.Base(wd))
	}
	if !strings.Contains(p.View(), "42m") {
		t.Errorf("view missing the total:\n%s", p.View())
	}
}

func TestTimeExportWritesCSVScratch(t *testing.T) {
	m, _ := timeApp(t)
	out, _ := m.Update(timepanel.ExportMsg{Label: "Today", CSV: "range,project\nToday,ike\n"})
	m = out.(Model)

	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() {
		t.Fatal("the export did not open the scratch file")
	}
	if filepath.Ext(ed.Path()) != ".csv" {
		t.Errorf("path = %q, want a .csv scratch", ed.Path())
	}
	data, err := os.ReadFile(ed.Path())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "range,project\nToday,ike\n" {
		t.Errorf("scratch content = %q", data)
	}
}

// segmentApp builds a sized app over an isolated config dir holding the given
// settings.toml. NewWith does not load the config (only New does), so the
// helper installs the process-wide snapshot the segment reads and restores the
// defaults afterwards.
func segmentApp(t *testing.T, toml string) (Model, string) {
	t.Helper()
	cfgDir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", cfgDir)
	if toml != "" {
		if err := os.WriteFile(filepath.Join(cfgDir, "settings.toml"), []byte(toml), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, _ := config.Load(config.Discover("."))
	config.Set(cfg)
	t.Cleanup(func() {
		fresh, _ := config.Load(config.Options{})
		config.Set(fresh)
	})
	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return out.(Model), cfgDir
}

func TestProjectTimeSegmentOffByDefault(t *testing.T) {
	m, cfgDir := segmentApp(t, "")
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	writeUsageLog(t, cfgDir, wd, 42*time.Minute)

	if projectTimeSegmentOn() {
		t.Fatal("statusline.project_time defaults to on")
	}
	// Even with a report on the model the segment stays silent, and nothing
	// is armed — an opted-out session never scans the telemetry directory.
	msg := m.timeReadCmd()().(timeReportMsg)
	out, _ := m.Update(msg)
	m = out.(Model)
	if got := m.projectTimeSegment(); got != "" {
		t.Errorf("segment while off = %q, want empty", got)
	}
	if cmd := m.timeSegmentCmd(); cmd != nil {
		t.Error("the segment ticker armed while the setting is off")
	}
}

func TestProjectTimeSegmentOnShowsToday(t *testing.T) {
	m, cfgDir := segmentApp(t, "[statusline]\nproject_time = \"on\"\n")
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	writeUsageLog(t, cfgDir, wd, 42*time.Minute)

	if !projectTimeSegmentOn() {
		t.Fatal("the setting did not take effect")
	}
	msg := m.timeReadCmd()().(timeReportMsg)
	out, _ := m.Update(msg)
	m = out.(Model)
	if got := m.projectTimeSegment(); !strings.Contains(got, "42m") {
		t.Errorf("segment = %q, want today's 42m", got)
	}
	if m.timeSegmentCmd() == nil {
		t.Error("the segment ticker did not arm while the setting is on")
	}
}

func TestProjectTimeSegmentSilentWithoutData(t *testing.T) {
	m, _ := segmentApp(t, "[statusline]\nproject_time = \"on\"\n")
	msg := m.timeReadCmd()().(timeReportMsg)
	out, _ := m.Update(msg)
	m = out.(Model)
	if got := m.projectTimeSegment(); got != "" {
		t.Errorf("segment with an empty log = %q, want empty", got)
	}
}
