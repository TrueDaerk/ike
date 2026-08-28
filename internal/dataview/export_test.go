package dataview

// export_test.go covers the pane's half of the export (#2248): the line owns
// the keyboard, validates before it scans, writes what the grid shows, and
// hands the confirmation to the root model.

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// exportTo opens the export line, replaces the prefilled path with path, and
// settles the export it starts. It returns whatever the pane sent on to the
// root model.
func exportTo(t *testing.T, m *Model, path string) []tea.Msg {
	t.Helper()
	pump(t, m, m.Update(key("E")))
	if !m.Exporting() {
		t.Fatal("E must open the export line")
	}
	for range m.ExportInput() {
		m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	typeInto(t, m, path)
	return pump(t, m, m.Update(key("enter")))
}

func TestExportWritesTheGridAsCSV(t *testing.T) {
	m := sqlGridPane(t, writeFixtureDB(t))
	out := filepath.Join(t.TempDir(), "users.csv")
	msgs := exportTo(t, &m, out)
	if m.Exporting() {
		t.Fatalf("a finished export closes its line (err %v)", m.ExportErr())
	}
	var done *ExportedMsg
	for _, msg := range msgs {
		if e, ok := msg.(ExportedMsg); ok {
			done = &e
		}
	}
	if done == nil {
		t.Fatal("a finished export must report itself to the root model")
	}
	if done.Rows != 1200 || done.Capped || done.Format != "CSV" {
		t.Fatalf("ExportedMsg = %+v, want 1200 uncapped CSV rows", *done)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	recs, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("the export must parse as CSV: %v", err)
	}
	if len(recs) != 1201 || recs[0][0] != "id" || recs[1][1] != "user-1" {
		t.Fatalf("unexpected export: %d records, head %v", len(recs), recs[0])
	}
}

func TestExportFollowsTheFilterAndSort(t *testing.T) {
	m := sqlGridPane(t, writeFixtureDB(t))
	pump(t, &m, m.Update(key("S")))
	pump(t, &m, m.Update(key("S"))) // id descending
	pump(t, &m, m.Update(key("/")))
	typeInto(t, &m, "id <= 3")
	pump(t, &m, m.Update(key("enter")))

	out := filepath.Join(t.TempDir(), "users.csv")
	exportTo(t, &m, out)
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 4 {
		t.Fatalf("the export must hold the filtered rows only:\n%s", data)
	}
	if !strings.HasPrefix(lines[1], "3,") || !strings.HasPrefix(lines[3], "1,") {
		t.Fatalf("the export must keep the grid's order:\n%s", data)
	}
}

func TestExportRejectsABadPathWithoutScanning(t *testing.T) {
	m := sqlGridPane(t, writeFixtureDB(t))
	dir := t.TempDir()
	pump(t, &m, m.Update(key("E")))
	for range m.ExportInput() {
		m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	typeInto(t, &m, filepath.Join(dir, "users.xlsx"))
	pump(t, &m, m.Update(key("enter")))
	if !m.Exporting() || m.ExportErr() == nil {
		t.Fatal("an unknown extension keeps the line open with its error")
	}
	if !strings.Contains(m.ExportErr().Error(), ".csv") {
		t.Fatalf("the error must name the two formats, got %v", m.ExportErr())
	}
	// Editing the path clears the error the old text earned.
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.ExportErr() != nil {
		t.Fatal("editing the path drops the stale error")
	}
	// A directory that does not exist is caught before any rows are read.
	for range m.ExportInput() {
		m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	typeInto(t, &m, filepath.Join(dir, "nope", "users.csv"))
	pump(t, &m, m.Update(key("enter")))
	if m.ExportErr() == nil || !strings.Contains(m.ExportErr().Error(), "no such directory") {
		t.Fatalf("a missing directory must be named, got %v", m.ExportErr())
	}
}

func TestExportAsksBeforeOverwriting(t *testing.T) {
	m := sqlGridPane(t, writeFixtureDB(t))
	out := filepath.Join(t.TempDir(), "users.csv")
	if err := os.WriteFile(out, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pump(t, &m, m.Update(key("E")))
	for range m.ExportInput() {
		m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	typeInto(t, &m, out)
	pump(t, &m, m.Update(key("enter")))
	if m.ExportErr() == nil || !strings.Contains(m.ExportErr().Error(), "exists") {
		t.Fatalf("an existing file must be reported once, got %v", m.ExportErr())
	}
	if data, _ := os.ReadFile(out); string(data) != "keep me\n" {
		t.Fatal("the first enter must not have written anything")
	}
	pump(t, &m, m.Update(key("enter"))) // confirmed
	if m.Exporting() {
		t.Fatalf("the confirmed export must run (err %v)", m.ExportErr())
	}
	if data, _ := os.ReadFile(out); !strings.HasPrefix(string(data), "id,name") {
		t.Fatalf("the confirmed export must overwrite, got %.20q", data)
	}
}

func TestExportLineOwnsTheKeyboard(t *testing.T) {
	m := sqlGridPane(t, writeFixtureDB(t))
	before := m.Cursor()
	pump(t, &m, m.Update(key("E")))
	// j/n/S would move the grid; inside the line they are plain characters.
	typeInto(t, &m, "jnS")
	if m.Cursor() != before || m.PageOffset() != 0 || m.Sort().Active() {
		t.Fatal("the grid must not move while the export line is open")
	}
	if !strings.HasSuffix(m.ExportInput(), "jnS") {
		t.Fatalf("the keys belong to the path, got %q", m.ExportInput())
	}
	// A click is inert while the line holds a half-typed path.
	m.Click(m.SidebarWidth()+2, 3)
	if !m.Exporting() {
		t.Fatal("a click must not close the export line")
	}
	pump(t, &m, m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}))
	if m.Exporting() {
		t.Fatal("esc closes the line")
	}
}

func TestExportMsgOpensTheLineLikeTheKey(t *testing.T) {
	m := sqlGridPane(t, writeFixtureDB(t))
	pump(t, &m, m.Update(ExportMsg{}))
	if !m.Exporting() {
		t.Fatal("data.export must open the line like E")
	}
	if !strings.HasSuffix(m.ExportInput(), "users.csv") {
		t.Fatalf("the line is prefilled with a path next to the database, got %q", m.ExportInput())
	}
}

func TestExportIsAsyncAndCancelable(t *testing.T) {
	src := &fakeSource{rows: 1200, est: 1200}
	m := gridPane(t, src)
	src.pageBlock = make(chan struct{})
	defer func() {
		if src.pageBlock != nil {
			close(src.pageBlock) // an early failure must not wedge the goroutine
		}
	}()

	pump(t, &m, m.Update(key("E")))
	for range m.ExportInput() {
		m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	typeInto(t, &m, filepath.Join(t.TempDir(), "big.csv"))
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter must return the background export command")
	}
	if !m.ExportRunning() {
		t.Fatal("the export runs in the background, not on the UI thread")
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()

	// esc cancels the job and closes the line; the result that eventually
	// lands has nowhere to go and is dropped.
	pump(t, &m, m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}))
	if m.Exporting() {
		t.Fatal("esc closes the export line")
	}
	close(src.pageBlock)
	src.pageBlock = nil // the deferred close must not double-close
	msg := <-done
	res, ok := msg.(ResultMsg)
	if !ok || res.export == nil {
		t.Fatalf("the command must return an export result, got %#v", msg)
	}
	if m.applyExport(res.export) != nil || m.Exporting() {
		t.Fatal("a result whose line is gone must be dropped")
	}
}

func TestExportPasteFillsThePath(t *testing.T) {
	m := sqlGridPane(t, writeFixtureDB(t))
	pump(t, &m, m.Update(key("E")))
	for range m.ExportInput() {
		m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	if !m.PasteText("/tmp/out.json") {
		t.Fatal("the export line must consume a paste — a path is pasted, not typed")
	}
	if m.ExportInput() != "/tmp/out.json" {
		t.Fatalf("pasted path = %q", m.ExportInput())
	}
}
