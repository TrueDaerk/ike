package app

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	_ "modernc.org/sqlite"

	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/registry"
)

// writeTestDB builds a tiny SQLite database with one populated table.
func writeTestDB(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO notes (body) VALUES ('hello'), (NULL)`); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestOpenPathRoutesDatabasesToHandler: every claimed extension dispatches
// the data pane message instead of loading a buffer, and so does a database
// with a foreign extension via the magic sniff.
func TestOpenPathRoutesDatabasesToHandler(t *testing.T) {
	for _, name := range []string{"app.db", "app.sqlite", "app.sqlite3", "app.data"} {
		t.Run(name, func(t *testing.T) {
			m := newSized()
			p := writeTestDB(t, name)
			_, cmd := m.openPath(p, false)
			found := false
			for _, msg := range dispatched(cmd) {
				if open, ok := msg.(OpenDataMsg); ok && open.Path == p {
					found = true
				}
			}
			if !found {
				t.Fatalf("openPath on %s must dispatch OpenDataMsg", name)
			}
		})
	}
}

// TestPlainDBExtensionWithoutMagicStillClaimed: a .db file that is no SQLite
// database is still claimed by extension — the pane then shows the engine's
// error instead of a binary buffer.
func TestPlainDBExtensionWithoutMagicStillClaimed(t *testing.T) {
	p := filepath.Join(t.TempDir(), "legacy.db")
	if err := os.WriteFile(p, []byte("dBASE or whatever"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	h, ok := m.reg.ResolveHandler(p, readHead(p))
	if !ok || h.Owner != "data" {
		t.Fatalf("a .db file must resolve to the data handler, got %+v ok=%v", h, ok)
	}
}

// writeDuckTestDB builds a tiny DuckDB database through the CLI, skipping the
// test when the binary is not installed — the DuckDB engine (#1765) is a soft
// dependency.
func writeDuckTestDB(t *testing.T, name string) string {
	t.Helper()
	bin, err := exec.LookPath("duckdb")
	if err != nil {
		t.Skip("duckdb CLI not installed")
	}
	p := filepath.Join(t.TempDir(), name)
	out, err := exec.Command(bin, p,
		`CREATE TABLE notes (id INTEGER, body VARCHAR); INSERT INTO notes VALUES (1, 'hello'), (2, NULL);`).CombinedOutput()
	if err != nil {
		t.Fatalf("building the fixture failed: %v\n%s", err, out)
	}
	return p
}

// TestOpenPathRoutesDuckDBToHandler: the DuckDB extensions dispatch the data
// pane, and so does a DuckDB database carrying a foreign extension — the
// magic sniff claims it before any engine runs.
func TestOpenPathRoutesDuckDBToHandler(t *testing.T) {
	for _, name := range []string{"app.duckdb", "app.ddb", "app.warehouse"} {
		t.Run(name, func(t *testing.T) {
			m := newSized()
			p := writeDuckTestDB(t, name)
			_, cmd := m.openPath(p, false)
			found := false
			for _, msg := range dispatched(cmd) {
				if open, ok := msg.(OpenDataMsg); ok && open.Path == p {
					found = true
				}
			}
			if !found {
				t.Fatalf("openPath on %s must dispatch OpenDataMsg", name)
			}
		})
	}
}

// TestOpenDuckDataPaneShowsTables: a DuckDB file lands in the same pane, with
// its tables listed and the first page loaded.
func TestOpenDuckDataPaneShowsTables(t *testing.T) {
	m := newSized()
	p := writeDuckTestDB(t, "app.duckdb")
	m.openDataPane(p)
	inst := m.activeWS().Panes.Get(m.activeWS().Panes.Focused())
	if inst == nil || inst.Kind() != pane.KindData {
		t.Fatal("a DuckDB file must open in a data pane")
	}
	dv := inst.Data()
	if dv.Err() != nil {
		t.Fatalf("pane error = %v", dv.Err())
	}
	if dv.Tables() != 1 || dv.SelectedTable() != "notes" || dv.PageRows() != 2 {
		t.Fatalf("tables=%d selected=%q rows=%d", dv.Tables(), dv.SelectedTable(), dv.PageRows())
	}
}

// TestOpenDataPaneShowsTables: the pane opens focused, bound to the file,
// with the table loaded into the grid.
func TestOpenDataPaneShowsTables(t *testing.T) {
	m := newSized()
	p := writeTestDB(t, "app.db")
	m.openDataPane(p)
	key := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindData {
		t.Fatalf("focused pane = %q", key)
	}
	dv := inst.Data()
	if dv.Path() != p || dv.Err() != nil {
		t.Fatalf("pane path=%q err=%v", dv.Path(), dv.Err())
	}
	if dv.Tables() != 1 || dv.SelectedTable() != "notes" {
		t.Fatalf("tables=%d selected=%q", dv.Tables(), dv.SelectedTable())
	}
	if dv.PageRows() != 2 {
		t.Fatalf("page rows = %d", dv.PageRows())
	}
	// Re-opening the same database refocuses instead of splitting again.
	m.setFocus(m.fileEditorKey())
	m.openDataPane(p)
	if m.activeWS().Panes.Focused() != key {
		t.Fatal("re-open must refocus the existing pane")
	}
}

// TestShowTableSchemaOpensReadOnlyBuffer: the schema message lands in a
// read-only editor tab holding the DDL under a virtual .sql path.
func TestShowTableSchemaOpensReadOnlyBuffer(t *testing.T) {
	m := newSized()
	p := writeTestDB(t, "app.db")
	m.openDataPane(p)
	inst := m.activeWS().Panes.Get(m.activeWS().Panes.Focused())
	cmd := inst.Data().Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	if cmd == nil {
		t.Fatal("s must emit the schema message")
	}
	out, _ := m.Update(cmd())
	m = out.(Model)
	ed := m.activeWS().Panes.Get(m.activeWS().Panes.Focused()).Editor()
	if ed == nil || !ed.ReadOnly() {
		t.Fatal("the schema must open in a read-only editor buffer")
	}
	if !strings.Contains(ed.Text(), "CREATE TABLE notes") {
		t.Fatalf("buffer = %q", ed.Text())
	}
	if !strings.HasSuffix(ed.Path(), "!notes.sql") {
		t.Fatalf("virtual path = %q", ed.Path())
	}
}

// TestDataPaneRestoresByPath: the pane persists as kind "data" plus the file
// path and restores by re-opening the database.
func TestDataPaneRestoresByPath(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	p := writeTestDB(t, "app.db")

	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	m.openDataPane(p)
	key := m.activeWS().Panes.Focused()
	if inst := m.activeWS().Panes.Get(key); inst == nil || inst.Kind() != pane.KindData {
		t.Fatalf("setup: focused = %q", key)
	}
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)

	_, ids, ok := loadLayout()
	if !ok {
		t.Fatal("the layout must persist")
	}
	if id := ids[key]; id.Kind != "data" || id.Path != p {
		t.Fatalf("persisted identity = %+v", id)
	}

	m2 := NewWith(registry.New(), host.MapConfig{})
	inst := m2.activeWS().Panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindData {
		t.Fatalf("the data pane did not restore under %q", key)
	}
	if inst.Data().Path() != p || inst.Data().Tables() != 1 {
		t.Fatalf("restored pane = %q with %d tables", inst.Data().Path(), inst.Data().Tables())
	}
	found := false
	for _, leaf := range layout.Leaves(m2.activeWS().Tree) {
		if leaf == key {
			found = true
		}
	}
	if !found {
		t.Fatal("the data leaf is missing from the restored tree")
	}
}
