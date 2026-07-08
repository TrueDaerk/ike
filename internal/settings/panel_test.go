package settings

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
)

func testOpts(t *testing.T) config.Options {
	t.Helper()
	return config.Options{UserPath: filepath.Join(t.TempDir(), "settings.toml")}
}

// restoreConfig snapshots the process-wide config and restores it after the
// test, since edits go through config.Set.
func restoreConfig(t *testing.T) {
	t.Helper()
	prev := config.Get()
	t.Cleanup(func() { config.Set(prev) })
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	return tea.KeyPressMsg{Text: s, Code: rune(s[0])}
}

// apply runs a returned write-reload command and commits its config.
func apply(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a write-reload command")
	}
	msg, ok := cmd().(config.ConfigReloadedMsg)
	if !ok {
		t.Fatalf("expected ConfigReloadedMsg, got %#v", msg)
	}
	config.Set(msg.Config)
}

func testPages() []Page {
	return []Page{
		{Title: "Interface", Entries: []Entry{
			{Key: "ui.menu_bar", Type: Bool, Title: "Menu bar", Scope: config.UserScope},
			{Key: "editor.tab_width", Type: Int, Title: "Tab width", Scope: config.UserScope, Min: 1, Max: 16},
		}},
		{Title: "Appearance", Entries: []Entry{
			{Key: "theme.name", Type: Enum, Title: "Theme", Scope: config.UserScope, Options: []string{"default", "tokyo-night"}},
		}},
	}
}

// TestViewIsBoundedFloatingBox guards #115: the panel renders as a bordered
// box of exactly the configured size, not a full-screen sheet.
func TestViewIsBoundedFloatingBox(t *testing.T) {
	restoreConfig(t)
	m := New(testPages(), testOpts(t))
	m.SetSize(80, 18)
	m.Open()
	v := m.View()
	lines := strings.Split(v, "\n")
	if len(lines) != 18 {
		t.Fatalf("box height = %d lines, want 18", len(lines))
	}
	if !strings.Contains(lines[0], "╭") || !strings.Contains(lines[len(lines)-1], "╰") {
		t.Fatalf("box must carry a rounded border, first line %q", lines[0])
	}
}

func TestSchemaRendersValuesAndLayer(t *testing.T) {
	restoreConfig(t)
	m := New(testPages(), testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	v := m.View()
	for _, want := range []string{"Interface", "Appearance", "Menu bar", "true", "@default", "SETTINGS"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view missing %q:\n%s", want, v)
		}
	}
}

func TestBoolToggleWritesAndReloads(t *testing.T) {
	restoreConfig(t)
	opts := testOpts(t)
	m := New(testPages(), opts)
	m.SetSize(90, 20)
	m.Open()
	m.Update(key("tab")) // focus the form
	apply(t, m.Update(key("enter")))
	if config.Get().UI.MenuBar {
		t.Fatal("toggle must flip ui.menu_bar to false")
	}
	if got := config.Origin(opts, "ui.menu_bar"); got != "user" {
		t.Fatalf("origin after write = %q, want user", got)
	}
	if !strings.Contains(m.View(), "@user") {
		t.Fatal("layer badge must show the user override")
	}
}

func TestIntEditValidatesAndClamps(t *testing.T) {
	restoreConfig(t)
	m := New(testPages(), testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.Update(key("tab"))
	m.Update(key("down")) // Tab width
	m.Update(key("enter"))
	if !m.editing {
		t.Fatal("enter on an int entry must start an edit")
	}
	// Non-numeric input is rejected with an inline error, no write.
	m.input = "abc"
	if cmd := m.Update(key("enter")); cmd != nil || m.invalid == "" {
		t.Fatalf("invalid int must not write (invalid=%q)", m.invalid)
	}
	// A too-large value clamps to Max.
	m.input = "99"
	apply(t, m.Update(key("enter")))
	if got := config.Get().Editor.TabWidth; got != 16 {
		t.Fatalf("tab_width = %d, want clamped 16", got)
	}
}

func TestEnumCycles(t *testing.T) {
	restoreConfig(t)
	m := New(testPages(), testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.Update(key("down")) // Appearance page
	m.Update(key("tab"))
	apply(t, m.Update(key("enter")))
	if got := config.Get().Theme.Name; got != "tokyo-night" {
		t.Fatalf("enum must cycle default -> tokyo-night, got %q", got)
	}
}

func TestResetRemovesOverride(t *testing.T) {
	restoreConfig(t)
	opts := testOpts(t)
	m := New(testPages(), opts)
	m.SetSize(90, 20)
	m.Open()
	m.Update(key("tab"))
	apply(t, m.Update(key("enter"))) // menu_bar -> false (user layer)
	apply(t, m.Update(key("r")))     // reset to default
	if !config.Get().UI.MenuBar {
		t.Fatal("reset must fall back to the default (true)")
	}
	if got := config.Origin(opts, "ui.menu_bar"); got != "default" {
		t.Fatalf("origin after reset = %q, want default", got)
	}
}

// TestMouseClicksDriveThePanel guards #127: category clicks switch pages,
// entry clicks select, and a second click on the selection activates (enter
// semantics — a bool toggle returns the write command).
func TestMouseClicksDriveThePanel(t *testing.T) {
	restoreConfig(t)
	m := New(testPages(), testOpts(t))
	m.SetSize(90, 20)
	m.Open()

	// Row 3 in the category column = second page ("Appearance", body top is 2).
	if cmd := m.Click(2, 3); cmd != nil || m.cat != 1 {
		t.Fatalf("category click must switch to page 1, cat=%d", m.cat)
	}
	// Back to Editor-page equivalent (page 0).
	m.Click(2, 2)
	if m.cat != 0 {
		t.Fatalf("category click must switch back, cat=%d", m.cat)
	}

	// Click the second entry row in the form column: selection has a detail
	// line under it, so with sel=0 the second entry renders on body row 2.
	formX := 1 + catWidth + 4
	if cmd := m.Click(formX, 2+2); cmd != nil {
		t.Fatal("first click must only select")
	}
	if m.sel != 1 || m.focus != formColumn {
		t.Fatalf("entry click must select row 1, sel=%d focus=%v", m.sel, m.focus)
	}
	// Second click on the now-selected row (it moved up to body row 1... it is
	// row index 1 → line 1 since the selection sits below row 0's line).
	// Row 1 is the Int entry (tab width): activation opens an inline edit.
	m.Click(formX, 2+1)
	if !m.editing {
		t.Fatal("second click must activate the entry")
	}

	// Bool activation via double click returns the write command.
	m.editing = false
	m.Click(formX, 2+0) // select row 0 (bool)
	wcmd := m.Click(formX, 2+0)
	if wcmd == nil {
		t.Fatal("activating the bool entry must return the write command")
	}
	apply(t, wcmd)
	if config.Get().UI.MenuBar {
		t.Fatal("bool click-activation must toggle the value")
	}
}

func TestFilterSpansAllPages(t *testing.T) {
	restoreConfig(t)
	m := New(testPages(), testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.Update(key("/"))
	for _, r := range "theme" {
		m.Update(key(string(r)))
	}
	rows := m.rows()
	if len(rows) != 1 || rows[0].entry.Key != "theme.name" {
		t.Fatalf("filter 'theme' should match exactly theme.name, got %+v", rows)
	}
	if !strings.Contains(m.View(), "Appearance › Theme") {
		t.Fatalf("filtered rows must show their page:\n%s", m.View())
	}
	// Esc clears the filter, second esc closes the panel.
	m.Update(key("esc"))
	m.Update(key("esc"))
	if m.filter != "" {
		t.Fatal("esc must clear the filter")
	}
	m.Update(key("esc"))
	if m.IsOpen() {
		t.Fatal("esc must close the panel")
	}
}
