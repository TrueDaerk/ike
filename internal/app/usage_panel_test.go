package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ike/internal/pane"
	"ike/internal/usagepanel"
)

// usage_panel_test.go covers the Usage tool window's app half (#2552): the
// toggle state machine, the shared background read with the Time window and
// the CSV export.

func TestUsageToggleLifecycle(t *testing.T) {
	m, _ := timeApp(t)
	before := m.activeWS().Panes.Focused()

	out, _ := m.Update(UsageToggleMsg{})
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.UsageKey) || m.activeWS().Panes.Focused() != pane.UsageKey {
		t.Fatalf("first toggle must open + focus the panel (focus=%q)", m.activeWS().Panes.Focused())
	}

	out, _ = m.Update(UsageToggleMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != before {
		t.Fatalf("focus = %q, want %q", m.activeWS().Panes.Focused(), before)
	}

	out, _ = m.Update(UsageToggleMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != pane.UsageKey {
		t.Fatal("third toggle must re-focus the panel")
	}
}

func TestUsageReadFillsPanelFromTheSharedReport(t *testing.T) {
	m, cfgDir := timeApp(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	writeUsageLog(t, cfgDir, wd, 42*time.Minute)

	out, _ := m.Update(UsageToggleMsg{})
	m = out.(Model)
	msg, ok := m.timeReadCmd()().(timeReportMsg)
	if !ok {
		t.Fatal("the read did not yield a timeReportMsg")
	}
	out, _ = m.Update(msg)
	m = out.(Model)

	p := m.usagePanel()
	if p == nil {
		t.Fatal("no panel")
	}
	rows := p.Rows()
	if len(rows) != 1 || rows[0].Name != "editor.save" || rows[0].Cells[2] != "1" {
		t.Fatalf("rows = %+v, want editor.save once from a keybind", rows)
	}
	if !strings.Contains(p.View(), "editor.save") {
		t.Errorf("view missing the command:\n%s", p.View())
	}
	// The same read also fed the Time report on the model.
	if m.timeReport == nil || len(m.timeReport.Usage) == 0 {
		t.Error("the shared report holds no usage days")
	}
}

func TestUsageExportWritesCSVScratch(t *testing.T) {
	m, _ := timeApp(t)
	out, _ := m.Update(usagepanel.ExportMsg{Label: "Keys · Today", CSV: "tab,period\nKeys,Today\n"})
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
	if string(data) != "tab,period\nKeys,Today\n" {
		t.Errorf("scratch content = %q", data)
	}
}
