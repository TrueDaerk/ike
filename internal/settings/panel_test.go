package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/config"
	"ike/internal/theme"
)

func testOpts(t *testing.T) config.Options {
	t.Helper()
	return config.Options{UserPath: filepath.Join(t.TempDir(), "settings.toml")}
}

// restoreConfig snapshots the process-wide config and restores it after the
// test, since edits go through config.Set.
func restoreConfig(t *testing.T) {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir()) // isolates the per-project state files (#890)
	prev := config.Get()
	// Pin a clean baseline: the default layer is host-dependent (it
	// preconfigures lazygit when the binary is on PATH, #750), and the tools
	// page tests assert exact entry lists.
	clean, _ := config.Load(config.Options{})
	clean.Tools.Custom = nil
	config.Set(clean)
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
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
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

// commit applies the panel's staged batch (#1296): schema edits reach disk
// only through apply now, so every write test stages and then commits.
func commit(t *testing.T, m *Model) {
	t.Helper()
	apply(t, m.applyChanges())
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

// stubPage is a minimal custom PageModel for panel-hosting tests.
type stubPage struct{ got []string }

func (s *stubPage) Update(k tea.KeyPressMsg) tea.Cmd { s.got = append(s.got, k.String()); return nil }
func (s *stubPage) View(w, h int) string             { return "stub page" }
func (s *stubPage) SetPalette(*theme.Palette)        {}
func (s *stubPage) Capturing() bool                  { return false }

// TestFilterIndicatesCustomPages guards #383: a filtered view names the
// custom pages the filter cannot search.
func TestFilterIndicatesCustomPages(t *testing.T) {
	restoreConfig(t)
	pages := append(testPages(), Page{Title: "Keymap", Custom: &stubPage{}})
	m := New(pages, testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.Update(key("/"))
	m.Update(key("m"))
	if v := m.View(); !strings.Contains(v, "not searched: Keymap") {
		t.Fatalf("filtered view must name unsearched custom pages:\n%s", v)
	}
}

// TestCustomPageArrowLeftReturnsToCategories guards #383: on a hosted custom
// page arrow-left goes back to the category column, while plain "h" is still
// forwarded to the page (it may be filter text there).
func TestCustomPageArrowLeftReturnsToCategories(t *testing.T) {
	restoreConfig(t)
	stub := &stubPage{}
	pages := append(testPages(), Page{Title: "Keymap", Custom: stub})
	m := New(pages, testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.Update(key("down"))
	m.Update(key("down")) // Keymap page
	m.Update(key("right"))
	if m.focus != formColumn {
		t.Fatal("right must focus the custom page")
	}
	m.Update(key("h"))
	if m.focus != formColumn || len(stub.got) == 0 || stub.got[len(stub.got)-1] != "h" {
		t.Fatalf("h must be forwarded to the page, got %v", stub.got)
	}
	m.Update(key("left"))
	if m.focus != catColumn {
		t.Fatal("arrow-left must return to the categories")
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
	// The value marker replaced the [x] affordance and the @-column (#1295):
	// provenance moved into the detail column.
	for _, want := range []string{"Interface", "Appearance", "Menu bar", "true ◉", "4 ‹›", "SETTINGS"} {
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
	m.Update(key("enter"))
	if !m.Dirty() {
		t.Fatal("a toggle must stage a change")
	}
	commit(t, m)
	if config.Get().UI.MenuBar {
		t.Fatal("toggle must flip ui.menu_bar to false")
	}
	if got := config.Origin(opts, "ui.menu_bar"); got != "user" {
		t.Fatalf("origin after write = %q, want user", got)
	}
	m.focus = catColumn // the detail column reports provenance (#1295)
	if !strings.Contains(m.View(), "Interface") {
		t.Fatal("the panel must keep rendering after a write")
	}
}

// TestScopeSelectorWritesProjectLayer (0380, #794): "s" cycles the write
// scope; with "project" forced, a toggle writes .ike/settings.toml (created
// on demand), the row shows @project, and reset removes the project key so
// the value falls back immediately.
func TestScopeSelectorWritesProjectLayer(t *testing.T) {
	restoreConfig(t)
	opts := testOpts(t)
	opts.ProjectRoot = t.TempDir()
	m := New(testPages(), opts)
	m.SetSize(90, 20)
	m.Open()
	m.Update(key("tab"))

	// auto → user → project.
	m.Update(key("s"))
	m.Update(key("s"))
	if !strings.Contains(m.View(), "scope: project") {
		t.Fatalf("title must show the forced scope:\n%s", m.View())
	}
	m.Update(key("enter")) // toggle ui.menu_bar in project scope
	commit(t, m)
	if config.Get().UI.MenuBar {
		t.Fatal("toggle must flip ui.menu_bar to false")
	}
	if got := config.Origin(opts, "ui.menu_bar"); got != "project" {
		t.Fatalf("origin after project write = %q, want project", got)
	}
	if _, err := os.Stat(filepath.Join(opts.ProjectRoot, ".ike", "settings.toml")); err != nil {
		t.Fatal("the first project-scope write must create .ike/settings.toml")
	}
	if !strings.Contains(m.View(), "set in project") {
		t.Fatalf("the detail column must report the project override:\n%s", m.View())
	}

	// Reset in project scope removes the key; the value falls back.
	m.Update(key("r"))
	commit(t, m)
	if !config.Get().UI.MenuBar {
		t.Fatal("reset must fall back (default true)")
	}
	if got := config.Origin(opts, "ui.menu_bar"); got != "default" {
		t.Fatalf("origin after project reset = %q, want default", got)
	}

	// One more "s" wraps back to auto — the chip stays visible (clickable
	// chrome, #885) and reads auto again.
	m.Update(key("s"))
	if !strings.Contains(m.View(), "scope: auto") {
		t.Fatal("selector must wrap back to auto")
	}
}

// TestScopeSelectorProjectOverridesUser (#794): a project-scope write shadows
// an existing user value; removing the project key falls back to the user
// value, not the default.
func TestScopeSelectorProjectOverridesUser(t *testing.T) {
	restoreConfig(t)
	opts := testOpts(t)
	opts.ProjectRoot = t.TempDir()
	m := New(testPages(), opts)
	m.SetSize(90, 20)
	m.Open()
	m.Update(key("tab"))

	m.Update(key("s"))     // user
	m.Update(key("enter")) // menu_bar -> false @user
	commit(t, m)
	m.Update(key("s"))     // project
	m.Update(key("enter")) // menu_bar -> true @project
	commit(t, m)
	if !config.Get().UI.MenuBar {
		t.Fatal("project layer must win")
	}
	if got := config.Origin(opts, "ui.menu_bar"); got != "project" {
		t.Fatalf("origin = %q, want project", got)
	}
	m.Update(key("r")) // remove the project key
	commit(t, m)
	if config.Get().UI.MenuBar {
		t.Fatal("removing the project key must fall back to the user value (false)")
	}
	if got := config.Origin(opts, "ui.menu_bar"); got != "user" {
		t.Fatalf("origin after fallback = %q, want user", got)
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
	if m.focus != detailColumn {
		t.Fatal("enter on an int entry must focus the stepper in the detail column")
	}
	ed, ok := m.editor.(*intEditor)
	if !ok {
		t.Fatalf("editor = %T, want *intEditor", m.editor)
	}
	// Non-numeric input is rejected with an inline error, no write.
	ed.tf.Set("abc")
	if cmd := m.Update(key("enter")); cmd != nil || ed.err == "" {
		t.Fatalf("invalid int must not write (err=%q)", ed.err)
	}
	// A too-large value clamps to Max.
	ed.tf.Set("99")
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Editor.TabWidth; got != 16 {
		t.Fatalf("tab_width = %d, want clamped 16", got)
	}
}

// TestEnumEditor guards #383/#1295: enter on an enum row focuses the option
// list in the detail column; ↑↓ move, enter commits, esc returns without a
// write.
func TestEnumEditor(t *testing.T) {
	restoreConfig(t)
	m := New(testPages(), testOpts(t))
	m.SetSize(130, 20)
	m.Open()
	m.Update(key("down")) // Appearance page
	m.Update(key("tab"))
	if cmd := m.Update(key("enter")); cmd != nil || m.focus != detailColumn {
		t.Fatalf("enter on an enum must focus the detail editor, focus=%v", m.focus)
	}
	if _, ok := m.editor.(*enumEditor); !ok {
		t.Fatalf("editor = %T, want *enumEditor", m.editor)
	}
	if !strings.Contains(m.View(), "type to filter") {
		t.Fatalf("the option list must render:\n%s", m.View())
	}
	// Esc leaves the editor without writing.
	if cmd := m.Update(key("esc")); cmd != nil || m.focus != formColumn {
		t.Fatal("esc must return to the settings column without a write")
	}
	if got := config.Get().Theme.Name; got != "default" {
		t.Fatalf("cancel must not change the value, got %q", got)
	}
	// Re-enter, move down, commit.
	m.Update(key("enter"))
	m.Update(key("down"))
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Theme.Name; got != "tokyo-night" {
		t.Fatalf("enter must write the highlighted option, got %q", got)
	}
}

// TestEnumEditorFilters guards #1295: typing narrows a long option list
// instead of forcing a scroll through it.
func TestEnumEditorFilters(t *testing.T) {
	restoreConfig(t)
	m := New(testPages(), testOpts(t))
	m.SetSize(130, 20)
	m.Open()
	m.Update(key("down"))
	m.Update(key("tab"))
	m.Update(key("enter"))
	ed := m.editor.(*enumEditor)
	for _, r := range "tokyo" {
		m.Update(keyRune(r))
	}
	if got := ed.matches(); len(got) != 1 || got[0] != "tokyo-night" {
		t.Fatalf("matches = %v, want only tokyo-night", got)
	}
	m.Update(key("enter"))
	commit(t, m)
	if got := config.Get().Theme.Name; got != "tokyo-night" {
		t.Fatalf("enter must write the filtered option, got %q", got)
	}
}

// TestEnumQuickCycle guards #383/#533: →/l on a selected enum row cycle the
// value (wrapping) without opening the picker; ← never cycles — it returns to
// the category column like on every other row.
func TestEnumQuickCycle(t *testing.T) {
	restoreConfig(t)
	m := New(testPages(), testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.Update(key("down")) // Appearance page
	m.Update(key("tab"))
	m.Update(key("right"))
	commit(t, m)
	if got := config.Get().Theme.Name; got != "tokyo-night" {
		t.Fatalf("right must cycle to the next option, got %q", got)
	}
	if m.focus != formColumn {
		t.Fatal("quick cycle must not move the focus into the detail column")
	}
	m.Update(key("l"))
	commit(t, m)
	if got := config.Get().Theme.Name; got != "default" {
		t.Fatalf("l must wrap to the first option, got %q", got)
	}
	// ← mirrors → as a value change on enum rows (#889): it cycles back and
	// keeps the focus in the form column.
	m.Update(key("left"))
	commit(t, m)
	if got := config.Get().Theme.Name; got != "tokyo-night" {
		t.Fatalf("left must cycle to the previous option, got %q", got)
	}
	if m.focus != formColumn {
		t.Fatal("left on an enum row must stay in the form column")
	}
}

// TestArrowColumnNavigation guards #383: →/l enter the form, ←/h return to
// the categories (on non-enum rows).
func TestArrowColumnNavigation(t *testing.T) {
	restoreConfig(t)
	m := New(testPages(), testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	if m.Update(key("right")); m.focus != formColumn || m.sel != 0 {
		t.Fatalf("right must focus the form, focus=%v sel=%d", m.focus, m.sel)
	}
	if m.Update(key("h")); m.focus != catColumn {
		t.Fatal("h must return to the categories")
	}
	if m.Update(key("l")); m.focus != formColumn {
		t.Fatal("l must focus the form")
	}
	// Row 0 is a bool: arrow-left leaves the column (no enum to cycle).
	if cmd := m.Update(key("left")); cmd != nil || m.focus != catColumn {
		t.Fatal("left on a non-enum row must return to the categories")
	}
}

// TestScrollFollowsSelection guards #383: on a short window both columns
// scroll so the selection (and its detail line) stay visible.
func TestScrollFollowsSelection(t *testing.T) {
	restoreConfig(t)
	var entries []Entry
	var pages []Page
	for i := 0; i < 12; i++ {
		entries = append(entries, Entry{Key: "ui.menu_bar", Type: Bool,
			Title: "Entry " + string(rune('A'+i)), Scope: config.UserScope})
		pages = append(pages, Page{Title: "Page " + string(rune('A'+i)),
			Entries: entries[:1]})
	}
	pages[0].Entries = entries
	m := New(pages, testOpts(t))
	m.SetSize(80, 10) // inner body height = 6
	m.Open()

	// Category column: move to the last page; its label must be visible.
	// Steps wrap since #1666, so walk exactly to the last row.
	for i := 0; i < len(pages)-1; i++ {
		m.Update(key("down"))
	}
	if v := m.View(); !strings.Contains(v, "Page L") {
		t.Fatalf("category list must scroll to the selected page:\n%s", v)
	}
	// Form column: back to page 0 (12 entries), walk to the last entry.
	m.Update(key("home"))
	m.Update(key("tab"))
	for i := 0; i < len(entries)-1; i++ {
		m.Update(key("down"))
	}
	if v := m.View(); !strings.Contains(v, "Entry L") {
		t.Fatalf("form must scroll to the selected entry:\n%s", v)
	}
	// Scrolling back up must reveal the first entry again.
	for i := 0; i < len(entries)-1; i++ {
		m.Update(key("up"))
	}
	if v := m.View(); !strings.Contains(v, "Entry A") {
		t.Fatalf("form must scroll back up with the selection:\n%s", v)
	}
}

func TestResetRemovesOverride(t *testing.T) {
	restoreConfig(t)
	opts := testOpts(t)
	m := New(testPages(), opts)
	m.SetSize(90, 20)
	m.Open()
	m.Update(key("tab"))
	m.Update(key("enter")) // menu_bar -> false (user layer)
	commit(t, m)
	m.Update(key("r")) // reset to default
	commit(t, m)
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
	if cmd := m.Click(2, bodyTop+1); cmd != nil || m.cat != 1 {
		t.Fatalf("category click must switch to page 1, cat=%d", m.cat)
	}
	// Back to Editor-page equivalent (page 0).
	m.Click(2, bodyTop)
	if m.cat != 0 {
		t.Fatalf("category click must switch back, cat=%d", m.cat)
	}

	// Click the second entry row in the settings column: rows map 1:1 to
	// lines (#1295), so the second entry renders on body row 1.
	formX := formX() + 1
	if cmd := m.Click(formX, bodyTop+1); cmd != nil {
		t.Fatal("first click must only select")
	}
	if m.sel != 1 || m.focus != formColumn {
		t.Fatalf("entry click must select row 1, sel=%d focus=%v", m.sel, m.focus)
	}
	// Second click on the same row: row 1 is the Int entry (tab width),
	// activation focuses its stepper in the detail column.
	m.Click(formX, bodyTop+1)
	if m.focus != detailColumn {
		t.Fatal("second click must activate the entry")
	}

	// Row 0 is the bool: activating it focuses the toggle, and enter there
	// returns the write command.
	m.focus = formColumn
	m.Click(formX, bodyTop+0) // select row 0 (bool)
	m.Click(formX, bodyTop+0) // second click activates: the toggle stages
	commit(t, m)
	if config.Get().UI.MenuBar {
		t.Fatal("bool click-activation must toggle the value")
	}
}

// TestDetailColumnFollowsSelection guards #535/#1295: the selected entry's
// documentation lives in the detail column, so moving the selection never
// shifts the settings rows, and the detail always describes what is selected.
func TestDetailColumnFollowsSelection(t *testing.T) {
	restoreConfig(t)
	pages := []Page{{Title: "Interface", Entries: []Entry{
		{Key: "ui.menu_bar", Type: Bool, Title: "First entry", Scope: config.UserScope, Description: "first description"},
		{Key: "editor.tab_width", Type: Int, Title: "Second entry", Scope: config.UserScope, Description: "second description"},
		{Key: "ui.menu_bar", Type: Bool, Title: "Third entry", Scope: config.UserScope, Description: "third description"},
	}}}
	m := New(pages, testOpts(t))
	m.SetSize(110, 20)
	m.Open()
	m.Update(key("tab"))

	lineOf := func(v, needle string) int {
		for i, l := range strings.Split(v, "\n") {
			if strings.Contains(l, needle) {
				return i
			}
		}
		return -1
	}
	v := m.View()
	if lineOf(v, "first description") < 0 {
		t.Fatalf("the detail column must document the selection:\n%s", v)
	}
	third := lineOf(v, "Third entry")
	m.Update(key("down"))
	v = m.View()
	if got := lineOf(v, "Third entry"); got != third {
		t.Fatalf("moving the selection must not shift other rows: line %d -> %d\n%s", third, got, v)
	}
	if lineOf(v, "second description") < 0 {
		t.Fatalf("the detail must follow the selection:\n%s", v)
	}
}

// TestDetailColumnShowsPageWithoutSelection guards #1295: the third column is
// never blank — with the rail focused it explains the category.
func TestDetailColumnShowsPageWithoutSelection(t *testing.T) {
	restoreConfig(t)
	pages := []Page{{Title: "Interface", Description: "Everything about the chrome.", Entries: []Entry{
		{Key: "ui.menu_bar", Type: Bool, Title: "First entry", Scope: config.UserScope},
	}}}
	m := New(pages, testOpts(t))
	m.SetSize(110, 20)
	m.Open() // opens on the rail
	if !strings.Contains(m.View(), "Everything about the chrome.") {
		t.Fatalf("the detail column must describe the page:\n%s", m.View())
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
	if !strings.Contains(stripANSI(m.View()), "Appearance › Theme") {
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

// TestDetailWraps guards #549/#1295: a long description word-wraps inside the
// detail column instead of clipping at its edge.
func TestDetailWraps(t *testing.T) {
	restoreConfig(t)
	long := "This is a deliberately long help text that cannot possibly fit into a single narrow form column line and therefore must wrap"
	pages := []Page{{Title: "Interface", Entries: []Entry{
		{Key: "ui.menu_bar", Type: Bool, Title: "First", Scope: config.UserScope, Description: long},
		{Key: "editor.tab_width", Type: Int, Title: "Second", Scope: config.UserScope, Description: "short"},
	}}}
	m := New(pages, testOpts(t))
	m.SetSize(110, 20)
	m.Open()
	m.Update(key("tab"))

	v := m.View()
	if !strings.Contains(v, "This is a deliberately") {
		t.Fatalf("the description must render in the detail column:\n%s", v)
	}
	if !strings.Contains(v, "wrap") {
		t.Fatalf("a long description must wrap rather than clip:\n%s", v)
	}
	if !strings.Contains(v, "ui.menu_bar") {
		t.Fatalf("the detail meta row must name the key:\n%s", v)
	}

	// A short description keeps the detail column intact.
	m.Update(key("down"))
	if v := m.View(); !strings.Contains(v, "short") {
		t.Fatalf("short description must render:\n%s", v)
	}
}

// TestWheelScrollsColumns guards #673: the wheel moves the selection of the
// column under the pointer — categories on the left, the form on the right —
// clamped at both ends.
func TestWheelScrollsColumns(t *testing.T) {
	restoreConfig(t)
	m := New(testPages(), testOpts(t))
	m.SetSize(90, 20)
	m.Open()

	formX := formX() + 1

	// Wheel scrolls viewports now, never selections (#885). The test pages
	// fit their windows, so the offsets stay clamped at 0 and nothing jumps.
	m.Wheel(2, bodyTop, 1)
	if m.cat != 0 {
		t.Fatalf("category wheel must not move the selection, cat=%d", m.cat)
	}
	if m.catOff != 0 {
		t.Fatalf("catOff must clamp with everything visible, off=%d", m.catOff)
	}
	m.Wheel(formX, bodyTop, 1)
	if m.sel != 0 {
		t.Fatalf("form wheel must not move the selection, sel=%d", m.sel)
	}
	if m.formOff != 0 {
		t.Fatalf("formOff must clamp with everything visible, off=%d", m.formOff)
	}

	// Wheel is inert while the filter input is active.
	m.filtering = true
	m.Wheel(formX, bodyTop, 1)
	if m.sel != 0 {
		t.Fatal("wheel must be inert while filtering")
	}
	m.filtering = false
}

// TestDetailEditorKeysRoute guards #1295: while the detail column has the
// focus, every key goes to its editor — including the ones the panel would
// otherwise claim — and esc hands the focus back.
func TestDetailEditorKeysRoute(t *testing.T) {
	restoreConfig(t)
	m := New(testPages(), testOpts(t))
	m.SetSize(110, 20)
	m.Open()
	m.Update(key("tab"))
	m.Update(key("down")) // Int row (tab width)
	m.Update(key("enter"))
	ed, ok := m.editor.(*intEditor)
	if !ok {
		t.Fatalf("editor = %T, want *intEditor", m.editor)
	}
	// "r" would reset in the settings column; here it is text input.
	m.Update(key("r"))
	if !strings.Contains(ed.tf.text, "r") {
		t.Fatalf("keys must reach the editor, text = %q", ed.tf.text)
	}
	m.Update(key("esc"))
	if m.focus != formColumn {
		t.Fatalf("esc must return to the settings column, focus = %v", m.focus)
	}
	// The cancelled edit did not write.
	if got := config.Get().Editor.TabWidth; got != 4 {
		t.Fatalf("cancel must not write, tab_width = %d", got)
	}
}

// TestBoolEditorWrites guards #1295: the toggle is a pair of radio rows, and
// space on either writes.
func TestBoolEditorWrites(t *testing.T) {
	restoreConfig(t)
	m := New(testPages(), testOpts(t))
	m.SetSize(110, 20)
	m.Open()
	m.Update(key("tab")) // settings column
	m.Update(key("tab")) // detail column: the bool row's ◉/○ editor
	if m.focus != detailColumn {
		t.Fatalf("tab must reach the detail column, focus = %v", m.focus)
	}
	if _, ok := m.editor.(*boolEditor); !ok {
		t.Fatalf("editor = %T, want *boolEditor", m.editor)
	}
	m.Update(key("space"))
	commit(t, m)
	if config.Get().UI.MenuBar {
		t.Fatal("space in the toggle editor must write the other value")
	}
}

// Size reports the rendered box dimensions without rendering (#1396): the
// app's mouse dispatch hit-tests with it on every motion event.
func TestPanelSizeMirrorsView(t *testing.T) {
	restoreConfig(t)
	m := New(testPages(), testOpts(t))
	if w, h := m.Size(); w != 0 || h != 0 {
		t.Fatalf("closed panel Size = %d×%d, want 0×0", w, h)
	}
	m.SetSize(100, 30)
	m.Open()
	w, h := m.Size()
	if w != 100 || h != 30 {
		t.Fatalf("open panel Size = %d×%d, want 100×30", w, h)
	}
	v := m.View()
	if gw, gh := lipgloss.Width(v), lipgloss.Height(v); gw != w || gh != h {
		t.Fatalf("View renders %d×%d, Size says %d×%d", gw, gh, w, h)
	}
	m.SetSize(10, 4) // too small to render — View returns "", Size mirrors it
	if w, h := m.Size(); w != 0 || h != 0 {
		t.Fatalf("tiny panel Size = %d×%d, want 0×0", w, h)
	}
}
