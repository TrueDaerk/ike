package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/pane"
	"ike/internal/registry"

	// The csv language plugin is what makes a .csv buffer a table-rendered
	// one (#1589); the shipped binary compiles it in, so the test does too.
	_ "ike/plugins/languages/csv"
)

// columnprofile_test.go covers the root model's half of the column profile
// (#1940): the data viewer's command and copy path end to end against a real
// SQLite database, and the csv buffer's scan in the floating shell.

// TestDataColumnProfileRunsInTheFocusedViewer drives the palette command
// against an open data viewer and checks the popup ends up on screen with the
// numbers the engine computed.
func TestDataColumnProfileRunsInTheFocusedViewer(t *testing.T) {
	m := newSized()
	m = openData(t, m, writeTestDB(t, "app.db"))
	inst := m.focusedContent()
	if inst == nil || inst.Kind() != pane.KindData {
		t.Fatal("the data viewer must be focused after the open")
	}
	// tab moves the pane's region focus to the grid, whose column the profile
	// acts on.
	m = settle(t, m, inst.Update(tea.KeyPressMsg{Code: tea.KeyTab}))
	m = settle(t, m, func() tea.Msg { return DataColumnProfileMsg{} })

	view := ansi.Strip(m.focusedContent().Data().View())
	for _, want := range []string{"column: id", "rows", "distinct", "esc close"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the profile popup is missing %q:\n%s", want, view)
		}
	}
}

// TestDataColumnProfileIsPaneScoped: the command only exists while a data
// viewer has the focus — it is a grid action, not a global one.
func TestDataColumnProfileIsPaneScoped(t *testing.T) {
	cmd, ok := registry.Global().Command("data.columnProfile")
	if !ok {
		t.Fatal("data.columnProfile must be registered")
	}
	if got := cmd.Command.Scope.ContextID; got != "data" {
		t.Fatalf("scope = %q, want the data pane context", got)
	}
	csv, ok := registry.Global().Command("csv.columnProfile")
	if !ok {
		t.Fatal("csv.columnProfile must be registered")
	}
	if got := csv.Command.Scope.ContextID; got != "editor" {
		t.Fatalf("csv scope = %q, want the editor context", got)
	}
	// With no data viewer focused the command is inert rather than wrong.
	m := newSized()
	if c := m.profileColumn(); c != nil {
		t.Fatal("without a data viewer the profile command must do nothing")
	}
}

// TestCSVColumnProfileScansTheBuffer profiles the caret's column of a csv
// buffer and shows it in the shell; y copies exactly what is shown.
func TestCSVColumnProfileScansTheBuffer(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sales.csv")
	body := "region,qty\nnorth,1\nsouth,\nnorth,3\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.openPath(p, false)
	m = tm.(Model)
	m = settle(t, m, func() tea.Msg { return CSVColumnProfileMsg{} })

	if !m.csvProfileOpen() {
		t.Fatal("the csv profile must open the floating shell")
	}
	view := ansi.Strip(m.shell.View())
	for _, want := range []string{"column: region", "rows", "3", "north"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the csv profile is missing %q:\n%s", want, view)
		}
	}

	copied := ""
	orig := clipboardWrite
	clipboardWrite = func(s string) { copied = s }
	t.Cleanup(func() { clipboardWrite = orig })
	m.floats.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if !strings.Contains(copied, "column: region") {
		t.Fatalf("y must copy the profile, got %q", copied)
	}

	// esc dismisses it and leaves the workspace as it was.
	m.floats.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.csvProfileOpen() {
		t.Fatal("esc must close the csv profile")
	}
}

// TestCSVColumnProfileIgnoresANonTableBuffer: the command is scoped to the
// editor context, so it can fire on a plain file — it must then say so rather
// than profile nonsense.
func TestCSVColumnProfileIgnoresANonTableBuffer(t *testing.T) {
	p := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(p, []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.openPath(p, false)
	m = tm.(Model)
	m = settle(t, m, func() tea.Msg { return CSVColumnProfileMsg{} })
	if m.csvProfileOpen() {
		t.Fatal("a plain text buffer has no columns to profile")
	}
}
