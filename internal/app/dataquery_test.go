package app

// dataquery_test.go covers the root model's half of the data viewer's sort
// and export (#2248): both palette commands reach the focused viewer, and a
// finished export writes its file and confirms itself.

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/pane"
	"ike/internal/registry"
)

func TestDataSortAndExportAreRegisteredAndPaneScoped(t *testing.T) {
	for _, id := range []string{"data.sortColumn", "data.export"} {
		cmd, ok := registry.Global().Command(id)
		if !ok {
			t.Fatalf("%s must be registered", id)
		}
		if got := cmd.Command.Scope.ContextID; got != "data" {
			t.Fatalf("%s scope = %q, want the data pane context", id, got)
		}
	}
	// With no data viewer focused the commands are inert rather than wrong.
	m := newSized()
	if c := m.dataPaneUpdate(struct{}{}); c != nil {
		t.Fatal("without a data viewer the commands must do nothing")
	}
}

func TestDataSortCommandSortsTheFocusedGrid(t *testing.T) {
	m := newSized()
	m = openData(t, m, writeTestDB(t, "app.db"))
	inst := m.focusedContent()
	if inst == nil || inst.Kind() != pane.KindData {
		t.Fatal("the data viewer must be focused after the open")
	}
	m = settle(t, m, inst.Update(tea.KeyPressMsg{Code: tea.KeyTab})) // → grid
	m = settle(t, m, func() tea.Msg { return DataSortColumnMsg{} })
	m = settle(t, m, func() tea.Msg { return DataSortColumnMsg{} }) // descending

	view := ansi.Strip(m.focusedContent().Data().View())
	if !strings.Contains(view, "sort: id ▼") {
		t.Fatalf("the sort must reach the pane:\n%s", view)
	}
}

func TestDataExportCommandWritesTheFile(t *testing.T) {
	m := newSized()
	m = openData(t, m, writeTestDB(t, "app.db"))
	inst := m.focusedContent()
	if inst == nil || inst.Kind() != pane.KindData {
		t.Fatal("the data viewer must be focused after the open")
	}
	m = settle(t, m, inst.Update(tea.KeyPressMsg{Code: tea.KeyTab})) // → grid
	m = settle(t, m, func() tea.Msg { return DataExportMsg{} })
	data := m.focusedContent().Data()
	if !data.Exporting() {
		t.Fatal("data.export must open the viewer's export line")
	}
	// Retype the prefilled path into the test's own directory.
	for range data.ExportInput() {
		m = settle(t, m, data.Update(tea.KeyPressMsg{Code: tea.KeyBackspace}))
	}
	out := filepath.Join(t.TempDir(), "notes.csv")
	for _, r := range out {
		m = settle(t, m, data.Update(tea.KeyPressMsg{Code: r, Text: string(r)}))
	}
	m = settle(t, m, data.Update(tea.KeyPressMsg{Code: tea.KeyEnter}))
	if data.Exporting() {
		t.Fatalf("the export must finish and close its line (err %v)", data.ExportErr())
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("the export must have written the file: %v", err)
	}
	defer f.Close()
	recs, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("the export must parse as CSV: %v", err)
	}
	if len(recs) != 3 || recs[0][0] != "id" || recs[1][1] != "hello" || recs[2][1] != "" {
		t.Fatalf("unexpected export: %v", recs)
	}
}
