package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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
	apply(t, m.Update(key("enter"))) // toggle ui.menu_bar in project scope
	if config.Get().UI.MenuBar {
		t.Fatal("toggle must flip ui.menu_bar to false")
	}
	if got := config.Origin(opts, "ui.menu_bar"); got != "project" {
		t.Fatalf("origin after project write = %q, want project", got)
	}
	if _, err := os.Stat(filepath.Join(opts.ProjectRoot, ".ike", "settings.toml")); err != nil {
		t.Fatal("the first project-scope write must create .ike/settings.toml")
	}
	if !strings.Contains(m.View(), "@project") {
		t.Fatal("layer badge must show the project override")
	}

	// Reset in project scope removes the key; the value falls back.
	apply(t, m.Update(key("r")))
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

	m.Update(key("s"))               // user
	apply(t, m.Update(key("enter"))) // menu_bar -> false @user
	m.Update(key("s"))               // project
	apply(t, m.Update(key("enter"))) // menu_bar -> true @project
	if !config.Get().UI.MenuBar {
		t.Fatal("project layer must win")
	}
	if got := config.Origin(opts, "ui.menu_bar"); got != "project" {
		t.Fatalf("origin = %q, want project", got)
	}
	apply(t, m.Update(key("r"))) // remove the project key
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

// TestEnumPicker guards #383: enter on an enum row opens a picker list;
// ↑↓ move, enter commits, esc cancels without a write.
func TestEnumPicker(t *testing.T) {
	restoreConfig(t)
	m := New(testPages(), testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	m.Update(key("down")) // Appearance page
	m.Update(key("tab"))
	if cmd := m.Update(key("enter")); cmd != nil || !m.picking {
		t.Fatalf("enter on an enum must open the picker, picking=%v", m.picking)
	}
	if !strings.Contains(m.View(), "▸") {
		t.Fatalf("picker must render its options:\n%s", m.View())
	}
	// Esc cancels without writing.
	if cmd := m.Update(key("esc")); cmd != nil || m.picking {
		t.Fatal("esc must close the picker without a write")
	}
	if got := config.Get().Theme.Name; got != "default" {
		t.Fatalf("cancel must not change the value, got %q", got)
	}
	// Reopen, move down, commit.
	m.Update(key("enter"))
	m.Update(key("down"))
	apply(t, m.Update(key("enter")))
	if got := config.Get().Theme.Name; got != "tokyo-night" {
		t.Fatalf("picker enter must write the highlighted option, got %q", got)
	}
	if m.picking {
		t.Fatal("commit must close the picker")
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
	apply(t, m.Update(key("right")))
	if got := config.Get().Theme.Name; got != "tokyo-night" {
		t.Fatalf("right must cycle to the next option, got %q", got)
	}
	if m.focus != formColumn || m.picking {
		t.Fatal("quick cycle must not move focus or open the picker")
	}
	apply(t, m.Update(key("l")))
	if got := config.Get().Theme.Name; got != "default" {
		t.Fatalf("l must wrap to the first option, got %q", got)
	}
	// ← is the mirror of →: back to the categories, no config write (#533).
	if cmd := m.Update(key("left")); cmd != nil || m.focus != catColumn {
		t.Fatal("left on an enum row must leave the column without cycling")
	}
	if got := config.Get().Theme.Name; got != "default" {
		t.Fatalf("left must not change the value, got %q", got)
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
	for i := 0; i < len(pages); i++ {
		m.Update(key("down"))
	}
	if v := m.View(); !strings.Contains(v, "Page L") {
		t.Fatalf("category list must scroll to the selected page:\n%s", v)
	}
	// Form column: back to page 0 (12 entries), walk to the last entry.
	for i := 0; i < len(pages); i++ {
		m.Update(key("up"))
	}
	m.Update(key("tab"))
	for i := 0; i < len(entries); i++ {
		m.Update(key("down"))
	}
	if v := m.View(); !strings.Contains(v, "Entry L") {
		t.Fatalf("form must scroll to the selected entry:\n%s", v)
	}
	// Scrolling back up must reveal the first entry again.
	for i := 0; i < len(entries); i++ {
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

	// Click the second entry row in the form column: the description sits in
	// the pinned footer (#535), so list lines map 1:1 to rows — the second
	// entry renders on body row 1.
	formX := 1 + catWidth + 4
	if cmd := m.Click(formX, 2+1); cmd != nil {
		t.Fatal("first click must only select")
	}
	if m.sel != 1 || m.focus != formColumn {
		t.Fatalf("entry click must select row 1, sel=%d focus=%v", m.sel, m.focus)
	}
	// Second click on the same row: row 1 is the Int entry (tab width),
	// activation opens an inline edit.
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

// TestDetailFooterPinned guards #535: the selected entry's description renders
// in a footer pinned to the bottom of the form column, so moving the selection
// never shifts the other rows.
func TestDetailFooterPinned(t *testing.T) {
	restoreConfig(t)
	pages := []Page{{Title: "Interface", Entries: []Entry{
		{Key: "ui.menu_bar", Type: Bool, Title: "First entry", Scope: config.UserScope, Description: "first description"},
		{Key: "editor.tab_width", Type: Int, Title: "Second entry", Scope: config.UserScope, Description: "second description"},
		{Key: "ui.menu_bar", Type: Bool, Title: "Third entry", Scope: config.UserScope, Description: "third description"},
	}}}
	m := New(pages, testOpts(t))
	m.SetSize(90, 20)
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
	if lineOf(v, "first description") != 20-4 { // first footer line (2-line footer #549) above hint+border
		t.Fatalf("description must be pinned to the bottom of the form column:\n%s", v)
	}
	third := lineOf(v, "Third entry")
	m.Update(key("down"))
	v = m.View()
	if got := lineOf(v, "Third entry"); got != third {
		t.Fatalf("moving the selection must not shift other rows: line %d -> %d\n%s", third, got, v)
	}
	if lineOf(v, "second description") != 20-4 {
		t.Fatalf("footer must follow the selection:\n%s", v)
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

// TestDetailFooterWraps guards #549: a long description word-wraps over the
// two pinned footer lines instead of clipping at the column edge, and the
// footer height stays constant for short descriptions.
func TestDetailFooterWraps(t *testing.T) {
	restoreConfig(t)
	long := "This is a deliberately long help text that cannot possibly fit into a single narrow form column line and therefore must wrap"
	pages := []Page{{Title: "Interface", Entries: []Entry{
		{Key: "ui.menu_bar", Type: Bool, Title: "First", Scope: config.UserScope, Description: long},
		{Key: "editor.tab_width", Type: Int, Title: "Second", Scope: config.UserScope, Description: "short"},
	}}}
	m := New(pages, testOpts(t))
	m.SetSize(90, 16) // wide enough that the hint row stays a single line
	m.Open()
	m.Update(key("tab"))

	v := m.View()
	lines := strings.Split(v, "\n")
	// The two footer lines sit above the hint row and the bottom border.
	first, second := lines[len(lines)-4], lines[len(lines)-3]
	if !strings.Contains(first, "This is a deliberately") {
		t.Fatalf("first footer line = %q", first)
	}
	if !strings.Contains(second, "wrap") && !strings.Contains(second, "(ui.menu_bar)") {
		t.Fatalf("second footer line must carry the wrapped remainder, got %q", second)
	}

	// A short description keeps the same footer height (second line blank-ish).
	m.Update(key("down"))
	v = m.View()
	lines = strings.Split(v, "\n")
	if !strings.Contains(lines[len(lines)-4], "short") {
		t.Fatalf("short description must sit on the first footer line:\n%s", v)
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

	formX := 1 + catWidth + 4

	// Wheel scrolls viewports now, never selections (#885). The test pages
	// fit their windows, so the offsets stay clamped at 0 and nothing jumps.
	m.Wheel(2, 1)
	if m.cat != 0 {
		t.Fatalf("category wheel must not move the selection, cat=%d", m.cat)
	}
	if m.catOff != 0 {
		t.Fatalf("catOff must clamp with everything visible, off=%d", m.catOff)
	}
	m.Wheel(formX, 1)
	if m.sel != 0 {
		t.Fatalf("form wheel must not move the selection, sel=%d", m.sel)
	}
	if m.formOff != 0 {
		t.Fatalf("formOff must clamp with everything visible, off=%d", m.formOff)
	}

	// Wheel is inert while a picker or edit is open.
	m.picking = true
	m.Wheel(formX, 1)
	if m.sel != 0 {
		t.Fatal("wheel must be inert while picking")
	}
	m.picking = false
}

// TestPickerClicks guards #673: with an enum picker open, clicking an option
// applies it and clicking anywhere else closes the picker.
func TestPickerClicks(t *testing.T) {
	restoreConfig(t)
	m := New(testPages(), testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	formX := 1 + catWidth + 4

	openPicker := func() {
		m.Update(key("down")) // Appearance page
		m.Update(key("tab"))
		if m.Update(key("enter")); !m.picking {
			t.Fatal("enter on the enum row must open the picker")
		}
	}
	openPicker()

	// The options render directly under the selected row (body row 0), so
	// option 1 ("tokyo-night") sits on body row 2 = y 4.
	cmd := m.Click(formX, 2+2)
	if m.picking {
		t.Fatal("clicking an option must close the picker")
	}
	apply(t, cmd)
	if got := config.Get().Theme.Name; got != "tokyo-night" {
		t.Fatalf("option click must apply the value, got %q", got)
	}

	// A click outside the options (category column) only closes the picker.
	m.Open()
	openPicker()
	if cmd := m.Click(2, 3); cmd != nil || m.picking {
		t.Fatalf("outside click must close the picker without writing, picking=%v", m.picking)
	}
}

// TestEditClicks guards #673: with an inline edit active, a click on the row
// keeps the edit, a click elsewhere commits (or cancels when invalid).
func TestEditClicks(t *testing.T) {
	restoreConfig(t)
	m := New(testPages(), testOpts(t))
	m.SetSize(90, 20)
	m.Open()
	formX := 1 + catWidth + 4

	startEdit := func() {
		m.Update(key("tab"))
		m.Update(key("down")) // Int row (tab width)
		m.Update(key("enter"))
		if !m.editing {
			t.Fatal("enter must start the inline edit")
		}
	}
	startEdit()

	// Click on the edited row itself: the edit stays active.
	if m.Click(formX, 2+1); !m.editing {
		t.Fatal("clicking the edited row must keep the edit")
	}

	// Click elsewhere: the input commits like enter.
	m.input = "8"
	cmd := m.Click(formX, 2+0)
	if m.editing {
		t.Fatal("outside click must end the edit")
	}
	apply(t, cmd)
	if got := config.Get().Editor.TabWidth; got != 8 {
		t.Fatalf("outside click must commit the input, got %d", got)
	}

	// Invalid input: the outside click cancels instead of committing.
	m.Open()
	startEdit()
	m.input = "not a number"
	if cmd := m.Click(2, 3); cmd != nil || m.editing || m.invalid != "" {
		t.Fatalf("invalid input must cancel on outside click, editing=%v invalid=%q", m.editing, m.invalid)
	}
}
