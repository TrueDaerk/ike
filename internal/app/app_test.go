package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/editor"
	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/keymap"
	"ike/internal/layout"
	ilsp "ike/internal/lsp"
	"ike/internal/menu"
	"ike/internal/palette"
	"ike/internal/pane"
	"ike/internal/plugin"
	"ike/internal/registry"
)

// TestMain redirects the layout/session store and user-config discovery to a
// throwaway directory: several tests persist state (saveLayout on file opens,
// splits and drags; saveSession on quit) and New() restores it, so without
// isolation one test's artifacts leak into the next run — and into the
// developer's real .ike. newSized additionally rotates to a fresh subdirectory
// per model, so tests never see each other's persisted state either.
var testStoreRoot string

func TestMain(m *testing.M) {
	// Every test model on a fresh config dir is a "first start"; without this
	// the welcome tour (#658) would auto-open and swallow scripted input. The
	// first-run tests re-enable it locally.
	tourAutoOpen = false
	dir, err := os.MkdirTemp("", "ike-app-test")
	if err == nil {
		testStoreRoot = dir
		os.Setenv("IKE_CONFIG_DIR", dir)
	}
	code := m.Run()
	if err == nil {
		os.RemoveAll(dir)
	}
	os.Exit(code)
}

type fakePlugin struct {
	id   string
	caps plugin.Capabilities
}

func (f fakePlugin) ID() string                        { return f.id }
func (f fakePlugin) Capabilities() plugin.Capabilities { return f.caps }

// TestOpenRoutesThroughHandlerAndHooks verifies a claiming handler intercepts an
// open and that EventFileOpened hooks fire regardless.
func TestOpenRoutesThroughHandlerAndHooks(t *testing.T) {
	reg := registry.New()
	reg.Add(fakePlugin{id: "p", caps: plugin.Capabilities{
		FileHandlers: []plugin.FileHandler{{
			ID: "p.h", Extensions: []string{".special"},
			Open: func(h host.API, path string) tea.Cmd {
				h.SetStatus("handled " + filepath.Base(path))
				return nil
			},
		}},
		Hooks: []plugin.Hook{{
			ID: "p.hook", Event: plugin.EventFileOpened,
			Notify: func(h host.API, payload any) tea.Cmd {
				h.SetStatus(h.(*host.Host).Status() + " | hook")
				return nil
			},
		}},
	}})
	m := NewWith(reg, host.MapConfig{})

	out, _ := m.Update(explorer.OpenFileMsg{Path: "a/b.special"})
	if got := out.(Model).host.Status(); got != "handled b.special | hook" {
		t.Fatalf("handler+hook chain wrong: %q", got)
	}
	if out.(Model).activeEditor().HasFile() {
		t.Fatal("editor should not load a handler-claimed file")
	}
}

// TestInitFiresFileOpenedForRestoredFiles guards #332: files already open at
// startup are loaded straight into editors by restoreLayout/restoreSession,
// bypassing openPath, so Init must announce them via EventFileOpened (the LSP
// didOpen trigger) — once per file even when it is shared across tabs (#142).
func TestInitFiresFileOpenedForRestoredFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "restored.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var opened []string
	reg := registry.New()
	reg.Add(fakePlugin{id: "p", caps: plugin.Capabilities{
		Hooks: []plugin.Hook{{
			ID: "p.hook", Event: plugin.EventFileOpened,
			Notify: func(h host.API, payload any) tea.Cmd {
				opened = append(opened, payload.(string))
				return nil
			},
		}},
	}})
	m := NewWith(reg, host.MapConfig{})
	// Simulate a startup restore: the same file loaded straight into two tabs of
	// the active editor, exactly as restoreLayout does (never through openPath).
	inst := m.activeWS().Panes.Get(m.activeEditorKey())
	if err := inst.Editor().Load(path); err != nil {
		t.Fatal(err)
	}
	if err := inst.AddTab().Load(path); err != nil {
		t.Fatal(err)
	}
	m.Init()
	if len(opened) != 1 || opened[0] != path {
		t.Fatalf("Init must fire EventFileOpened exactly once for the restored file, got %v", opened)
	}
}

// TestPluginKeymapOverridesCore checks a high-priority plugin binding wins over
// a core key.
func TestPluginKeymapOverridesCore(t *testing.T) {
	reg := registry.New()
	reg.Add(fakePlugin{id: "p", caps: plugin.Capabilities{Keymaps: []plugin.Keymap{{
		Keys: "q", Scope: plugin.GlobalScope(), Priority: plugin.CorePriority + 1,
		Action: func(h host.API) tea.Cmd { h.SetStatus("plugin-q"); return nil },
	}}}})
	m := NewWith(reg, host.MapConfig{})

	out, _ := m.Update(tea.KeyPressMsg{Text: "q", Code: 'q'})
	if s := out.(Model).host.Status(); s != "plugin-q" {
		t.Fatalf("high-priority plugin binding should fire, status=%q", s)
	}
}

// TestPluginConfigDisable verifies "plugins.<id>.enabled=false" hides a plugin.
func TestPluginConfigDisable(t *testing.T) {
	reg := registry.New()
	reg.Add(fakePlugin{id: "p", caps: plugin.Capabilities{Keymaps: []plugin.Keymap{{
		Keys: "ctrl+e", Scope: plugin.GlobalScope(), Priority: plugin.CorePriority + 1,
		Action: func(h host.API) tea.Cmd { return nil },
	}}}})
	NewWith(reg, host.MapConfig{"plugins.p.enabled": "false"})
	if _, ok := reg.ResolveKey("ctrl+e", ""); ok {
		t.Fatal("disabled plugin keymap should not resolve")
	}
}

// TestMenuBarToggleAndDispatch guards the #90 wiring: f10's ToggleMenuMsg
// opens the menu, an open menu owns the keys, and menu.RunMsg routes into
// RunCommand. ui.menu_bar=false hides the bar and disables the toggle.
func TestMenuBarToggleAndDispatch(t *testing.T) {
	m := newSized()
	tm, _ := m.Update(ToggleMenuMsg{})
	m = tm.(Model)
	if !m.menu.IsOpen() {
		t.Fatal("ToggleMenuMsg must open the menu")
	}
	// The underlined letter-jump hint styles the title's first rune separately,
	// so match on stripped text.
	if !strings.Contains(ansi.Strip(m.render()), "File") {
		t.Fatal("menu bar missing from the frame")
	}
	// Esc closes via the menu's key ownership.
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m = tm.(Model); m.menu.IsOpen() {
		t.Fatal("esc must close the dropdown")
	}
	// RunMsg routes through the registry (unknown ids are a silent no-op).
	tm, cmd := m.Update(menu.RunMsg{Command: "editor.saveAll"})
	m = tm.(Model)
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		tm, cmd = m.Update(msg)
		m = tm.(Model)
	}

	hidden := NewWith(registry.New(), host.MapConfig{"ui.menu_bar": "false"})
	tm, _ = hidden.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	hidden = tm.(Model)
	tm, _ = hidden.Update(ToggleMenuMsg{})
	if hidden = tm.(Model); hidden.menu.IsOpen() {
		t.Fatal("hidden menu bar must not open")
	}
	if hidden.bodyRect().Y != 0 {
		t.Fatal("hidden menu bar must not reserve a row")
	}
}

// TestSplitFocusedCommands guards #114: pane.splitDown/up split the focused
// editor leaf with a fresh empty editor and move focus onto it.
func TestSplitFocusedCommands(t *testing.T) {
	// SplitFocused persists the layout; redirect the store so the multi-pane
	// tree never leaks into later tests (which restore it at New()).
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	tm, _ := m.Update(ToggleExplorerFocusMsg{}) // focus the editor
	m = tm.(Model)
	before := m.activeWS().Panes.Focused()
	editors := len(m.activeWS().Panes.Keys())

	tm, _ = m.Update(SplitFocusedMsg{Zone: layout.ZoneBottom})
	m = tm.(Model)
	if len(m.activeWS().Panes.Keys()) != editors+1 {
		t.Fatalf("split must add one editor pane, have %d", len(m.activeWS().Panes.Keys()))
	}
	after := m.activeWS().Panes.Focused()
	if after == before || after == "" {
		t.Fatal("focus must move to the new editor")
	}
	rb, ok := m.lay.Panes[before]
	ra, ok2 := m.lay.Panes[after]
	if !ok || !ok2 {
		t.Fatal("both panes must lay out")
	}
	if ra.Y <= rb.Y {
		t.Fatalf("ZoneBottom must place the new pane below (new y=%d, old y=%d)", ra.Y, rb.Y)
	}

	tm, _ = m.Update(SplitFocusedMsg{Zone: layout.ZoneTop})
	m = tm.(Model)
	top := m.activeWS().Panes.Focused()
	rt := m.lay.Panes[top]
	if rt.Y >= m.lay.Panes[after].Y {
		t.Fatalf("ZoneTop must place the new pane above (new y=%d, old y=%d)", rt.Y, m.lay.Panes[after].Y)
	}

	// The horizontal pair (#121): right lands right of, left lands left of.
	tm, _ = m.Update(SplitFocusedMsg{Zone: layout.ZoneRight})
	m = tm.(Model)
	right := m.activeWS().Panes.Focused()
	if m.lay.Panes[right].X <= rt.X {
		t.Fatalf("ZoneRight must place the new pane to the right (new x=%d, old x=%d)", m.lay.Panes[right].X, rt.X)
	}
	tm, _ = m.Update(SplitFocusedMsg{Zone: layout.ZoneLeft})
	m = tm.(Model)
	left := m.activeWS().Panes.Focused()
	if m.lay.Panes[left].X >= m.lay.Panes[right].X {
		t.Fatalf("ZoneLeft must place the new pane to the left (new x=%d, old x=%d)", m.lay.Panes[left].X, m.lay.Panes[right].X)
	}
}

// TestClickOutsideDismissesOverlays guards #116: a mouse press outside an
// open floating overlay closes it, a press inside keeps it open (and never
// falls through to the panes below).
func TestClickOutsideDismissesOverlays(t *testing.T) {
	press := func(x, y int) tea.MouseClickMsg {
		return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
	}
	// Settings panel: inside keeps, outside closes.
	m := newSized()
	tm, _ := m.Update(OpenSettingsMsg{})
	m = tm.(Model)
	tm, _ = m.Update(press(m.width/2, m.height/2)) // dead center = inside
	if m = tm.(Model); !m.settings.IsOpen() {
		t.Fatal("click inside must keep the settings panel open")
	}
	tm, _ = m.Update(press(0, m.height-1)) // corner = outside
	if m = tm.(Model); m.settings.IsOpen() {
		t.Fatal("click outside must close the settings panel")
	}

	// Floating shell (help overlay).
	tm, _ = m.Update(ShowKeymapHelpMsg{})
	m = tm.(Model)
	if !m.shell.IsOpen() {
		t.Fatal("precondition: shell open")
	}
	tm, _ = m.Update(press(m.width/2, m.height/2))
	if m = tm.(Model); !m.shell.IsOpen() {
		t.Fatal("click inside must keep the shell open")
	}
	tm, _ = m.Update(press(0, m.height-1))
	if m = tm.(Model); m.shell.IsOpen() {
		t.Fatal("click outside must close the shell")
	}

	// Command palette (centered).
	m.palette.SetSize(m.width, m.height)
	m.palette.Open(palette.Context{ContextID: m.focusContext(), Root: "."})
	tm, _ = m.Update(press(0, m.height-1))
	if m = tm.(Model); m.palette.IsOpen() {
		t.Fatal("click outside must close the palette")
	}
}

// TestRunCommand drives the command-palette seam.
func TestRunCommand(t *testing.T) {
	reg := registry.New()
	reg.Add(fakePlugin{id: "p", caps: plugin.Capabilities{Commands: []plugin.Command{{
		ID: "p.go", Scope: plugin.GlobalScope(),
		Run: func(h host.API) tea.Cmd { h.SetStatus("ran"); return nil },
	}}}})
	m := NewWith(reg, host.MapConfig{})
	m.RunCommand("p.go")
	if m.host.Status() != "ran" {
		t.Fatalf("RunCommand did not invoke command, status=%q", m.host.Status())
	}
}

var testStoreSeq atomic.Int64

// newSized builds a freshly isolated, sized model: each call rotates the state
// store to an empty directory so persisted layouts/sessions never couple tests.
func newSized() Model {
	if testStoreRoot != "" {
		os.Setenv("IKE_CONFIG_DIR", filepath.Join(testStoreRoot, strconv.FormatInt(testStoreSeq.Add(1), 10)))
	}
	m := New()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return tm.(Model)
}

// drainKey feeds a key into the app and runs the Cmd tree it produces (a
// keymap-resolved command dispatches an ActionMsg/Msg back into Update), so a
// test sees the end-to-end effect of a key press. Batches are expanded by
// drainCmd — every command dispatch is a batch since the command-executed
// signal (#679).
func drainKey(m Model, k tea.KeyPressMsg) Model {
	tm, cmd := m.Update(k)
	return drainCmd(tm.(Model), cmd)
}

// cmdMsgs flattens the message tree cmd produces (following tea.Batch nesting,
// without feeding anything into Update), for asserting that a dispatch emitted
// a particular message.
func cmdMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if msg == nil {
		return nil
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, cmdMsgs(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// TestCtrlZUndoesInEditor guards the deliverable undo binding: ctrl+z (cmd+z is
// undeliverable in a macOS terminal) must resolve through the keymap layer to
// editor.undo and revert the last edit.
func TestCtrlZUndoesInEditor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = tm.(Model)
	// Delete the first rune in normal mode: "hello" -> "ello". The focused editor
	// highlights the cursor cell with reverse-video escapes (and lipgloss v2 always
	// emits them), so strip ANSI before matching the logical text.
	m = drainKey(m, tea.KeyPressMsg{Text: "x", Code: 'x'})
	if strings.Contains(ansi.Strip(m.activeEditor().View()), "hello") {
		t.Fatalf("edit did not apply: %q", m.activeEditor().View())
	}
	// ctrl+z restores it.
	m = drainKey(m, tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	if !strings.Contains(ansi.Strip(m.activeEditor().View()), "hello") {
		t.Fatalf("ctrl+z did not undo: %q", m.activeEditor().View())
	}
}

// TestCtrlSSavesInEditor guards the deliverable save binding: ctrl+s (cmd+s is
// undeliverable in a macOS terminal) must resolve through the keymap layer to
// editor.write and persist the buffer — from insert mode too, since modifier
// chords stay eligible for the keymap layer while the editor captures text.
func TestCtrlSSavesInEditor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = tm.(Model)
	// Enter insert mode and type: "hello" -> "hihello".
	m = drainKey(m, tea.KeyPressMsg{Text: "i", Code: 'i'})
	m = drainKey(m, tea.KeyPressMsg{Text: "h", Code: 'h'})
	m = drainKey(m, tea.KeyPressMsg{Text: "i", Code: 'i'})
	// ctrl+s from insert mode writes the buffer to disk.
	m = drainKey(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "hihello") {
		t.Fatalf("ctrl+s did not save the edit; file = %q", got)
	}
}

// TestCmdWClosesTabThroughKeymap guards the editor.closeTab command: cmd+w must
// resolve through the keymap layer to the registered app-level command and
// close the focused editor pane (falling back to the explorer), like the
// hardcoded ctrl+w. GOOS is pinned to darwin so the table keeps the meta chord
// the test feeds, regardless of the build platform.
func TestCmdWClosesTabThroughKeymap(t *testing.T) {
	oldGOOS := keymap.GOOS
	keymap.GOOS = "darwin"
	defer func() { keymap.GOOS = oldGOOS }()
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = tm.(Model)
	if m.activeEditor() == nil {
		t.Fatal("precondition: editor should be open")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: 'w', Mod: tea.ModMeta})
	if m.activeEditor() != nil {
		t.Fatal("cmd+w should close the focused editor pane")
	}
	if m.activeWS().Panes.FocusedInstance().Kind() != pane.KindExplorer {
		t.Fatal("focus should fall back to the explorer")
	}
}

// TestF1OpensCheatsheetThroughKeymap guards palette.keymapHelp: f1 must resolve
// through the keymap layer to the registered app-level command and open the
// help overlay (the hardcoded "?" shares the same helper).
func TestF1OpensCheatsheetThroughKeymap(t *testing.T) {
	m := newSized()
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyF1})
	if !m.shell.IsOpen() {
		t.Fatal("f1 should open the keymap cheatsheet overlay")
	}
}

// TestCtrlTabCyclesFocusThroughKeymap guards pane.switcher: ctrl+tab must
// resolve through the keymap layer to the registered app-level command and
// cycle pane focus, like the hardcoded tab.
func TestCtrlTabCyclesFocusThroughKeymap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = tm.(Model)
	if m.activeWS().Panes.FocusedInstance().Kind() != pane.KindEditor {
		t.Fatal("precondition: editor focused after open")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModCtrl})
	if m.activeWS().Panes.FocusedInstance().Kind() != pane.KindExplorer {
		t.Fatal("ctrl+tab should cycle focus to the explorer")
	}
}

// TestGoToFileOpensLockedFilePalette guards project.goToFile: the command must
// open the centered palette locked to the fuzzy file mode from any context
// (here: the explorer, where the "@" editor key is unavailable).
func TestGoToFileOpensLockedFilePalette(t *testing.T) {
	m := newSized()
	tm, _ := m.Update(GoToFileMsg{})
	m = tm.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("project.goToFile should open the palette")
	}
	if m.palette.Anchored() {
		t.Fatal("go-to-file palette should be centered, not anchored")
	}
}

// TestSaveAllWritesDirtyEditors guards editor.saveAll: SaveAllMsg must write
// every dirty editor pane to disk.
func TestSaveAllWritesDirtyEditors(t *testing.T) {
	m := newSized()
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.openPath(path, false)
	m = tm.(Model)
	// Type "iX<esc>" into the focused editor to dirty the buffer.
	for _, k := range []tea.KeyPressMsg{
		{Code: 'i', Text: "i"},
		{Code: 'X', Text: "X"},
		{Code: tea.KeyEscape},
	} {
		m = drainKey(m, k)
	}
	tm, _ = m.Update(SaveAllMsg{})
	m = tm.(Model)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "Xone") {
		t.Fatalf("save-all wrote %q, want it to start with Xone", data)
	}
}

// TestToggleExplorerFocus guards explorer.toggle: focus flips from the
// explorer to the active editor and back.
func TestToggleExplorerFocus(t *testing.T) {
	m := newSized()
	if m.activeWS().Panes.Focused() != pane.ExplorerKey {
		t.Fatalf("precondition: explorer focused, got %q", m.activeWS().Panes.Focused())
	}
	tm, _ := m.Update(ToggleExplorerFocusMsg{})
	m = tm.(Model)
	if got := m.activeWS().Panes.Focused(); got == pane.ExplorerKey {
		t.Fatal("toggle should move focus off the explorer")
	}
	tm, _ = m.Update(ToggleExplorerFocusMsg{})
	m = tm.(Model)
	if got := m.activeWS().Panes.Focused(); got != pane.ExplorerKey {
		t.Fatalf("second toggle should focus the explorer again, got %q", got)
	}
}

// TestDisplayPathRelativeInsideProject guards the status-line file segment:
// project files render relative to the working directory, outside files
// render absolute.
func TestDisplayPathRelativeInsideProject(t *testing.T) {
	cwd, _ := os.Getwd()
	if got := displayPath(filepath.Join(cwd, "internal", "app", "app.go")); got != filepath.Join("internal", "app", "app.go") {
		t.Fatalf("inside project: got %q", got)
	}
	outside := filepath.Join(t.TempDir(), "x.go")
	if got := displayPath(outside); got != outside {
		t.Fatalf("outside project should stay absolute, got %q", got)
	}
}

// TestDeletingFileClosesItsEditor guards that removing a file in the explorer
// (delete, or undo of a create) closes any editor still showing it, rather than
// leaving a stale pane open on a gone file.
func TestDeletingFileClosesItsEditor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gone.txt")
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = tm.(Model)
	if m.editorWithFile(path) == "" {
		t.Fatal("precondition: editor should be open on the file")
	}
	tm, _ = m.Update(explorer.FileDeletedMsg{Path: path})
	m = tm.(Model)
	if m.editorWithFile(path) != "" {
		t.Fatal("editor should close when its file is deleted")
	}
	if m.activeWS().Panes.FocusedInstance().Kind() != pane.KindExplorer {
		t.Fatal("focus should fall back to the explorer after the editor closes")
	}
}

func TestTabSwitchesFocus(t *testing.T) {
	m := newSized()
	if m.activeWS().Panes.FocusedInstance().Kind() != pane.KindExplorer {
		t.Fatal("should start focused on explorer")
	}
	tm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = tm.(Model)
	if m.activeWS().Panes.FocusedInstance().Kind() != pane.KindEditor {
		t.Fatal("tab should focus editor")
	}
}

func TestOpenFileRoutesToEditor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = tm.(Model)
	if !m.activeEditor().HasFile() {
		t.Fatal("editor should have loaded the file")
	}
	if m.activeWS().Panes.FocusedInstance().Kind() != pane.KindEditor {
		t.Fatal("opening a file should focus the editor")
	}
}

func TestCloseMsgResetsToExplorer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = tm.(Model)
	tm, _ = m.Update(editor.CloseMsg{})
	m = tm.(Model)
	// :q closes the focused editor leaf entirely (Roadmap 0037): the editor pane is
	// gone, the explorer (sole remaining leaf) takes focus, and no editor is open.
	if m.activeEditor() != nil {
		t.Fatal("close should remove the editor pane")
	}
	if m.activeWS().Panes.FocusedInstance().Kind() != pane.KindExplorer {
		t.Fatal("close should focus explorer")
	}
}

func TestQuitFromExplorer(t *testing.T) {
	m := newSized()
	_, cmd := m.Update(tea.KeyPressMsg{Text: "q", Code: 'q'})
	if cmd == nil {
		t.Fatal("q in explorer should quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected QuitMsg, got %T", cmd())
	}
}

// TestSessionPersistsAndRestores verifies quitting writes session.json and a
// fresh model reopens the same file, cursor, focus, and explorer state.
func TestSessionPersistsAndRestores(t *testing.T) {
	proj := t.TempDir()
	state := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", state)
	t.Chdir(proj)

	sub := filepath.Join(proj, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(sub, "main.go")
	if err := os.WriteFile(file, []byte("line0\nline1\nline2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewWith(registry.New(), host.MapConfig{})
	// Expand the directory so the open file's row is visible, then open it.
	m.explorer().Restore(explorer.State{Expanded: []string{sub}})
	tm, _ := m.Update(explorer.OpenFileMsg{Path: file})
	m = tm.(Model)
	m.activeEditor().SetCursor(2, 3)

	if _, cmd := m.quit(); cmd == nil {
		t.Fatal("quit should return a command")
	}
	if _, err := os.Stat(filepath.Join(state, "session.json")); err != nil {
		t.Fatalf("session file not written: %v", err)
	}

	// A fresh model in the same dirs restores the workspace.
	m2 := NewWith(registry.New(), host.MapConfig{})
	if got := m2.activeEditor().Path(); got != file {
		t.Fatalf("restored editor path = %q, want %q", got, file)
	}
	if line, col := m2.activeEditor().CursorPos(); line != 2 || col != 3 {
		t.Fatalf("restored cursor = (%d,%d), want (2,3)", line, col)
	}
	if m2.activeWS().Panes.FocusedInstance().Kind() != pane.KindEditor {
		t.Fatal("restoring an open file should focus the editor")
	}
	snap := m2.explorer().Snapshot()
	if len(snap.Expanded) != 1 || snap.Expanded[0] != sub {
		t.Fatalf("restored explorer expansion = %v, want [%q]", snap.Expanded, sub)
	}
}

// TestSessionRestoresViewportFraming guards against the regression where only
// the cursor was restored: Top is sticky (not derivable from the cursor), so a
// file left scrolled with the cursor mid-screen must reopen framed identically —
// otherwise on-screen rows map to the wrong lines and mouse clicks miss.
func TestSessionRestoresViewportFraming(t *testing.T) {
	proj := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	t.Chdir(proj)

	var b strings.Builder
	for i := 0; i < 60; i++ {
		b.WriteString("L")
		b.WriteString(strconv.Itoa(i))
		b.WriteByte('\n')
	}
	file := filepath.Join(proj, "f.txt")
	if err := os.WriteFile(file, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewWith(registry.New(), host.MapConfig{})
	if err := m.activeEditor().Load(file); err != nil {
		t.Fatal(err)
	}
	m.setFocus(m.activeEditorKey())
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = out.(Model)
	// Scroll deep, then move the cursor back up so Top stays sticky above it.
	m.activeEditor().SetCursor(45, 0)
	m.activeEditor().SetCursor(20, 0)
	wantTop, wantLeft := m.activeEditor().ScrollOffset()
	if wantTop == 0 {
		t.Fatal("test setup: expected a non-zero sticky Top")
	}
	m.quit()

	m2 := NewWith(registry.New(), host.MapConfig{})
	out2, _ := m2.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m2 = out2.(Model)
	if top, left := m2.activeEditor().ScrollOffset(); top != wantTop || left != wantLeft {
		t.Fatalf("restored viewport = (top=%d,left=%d), want (top=%d,left=%d)", top, left, wantTop, wantLeft)
	}
}

// When the editor is focused in normal mode, "q" quits the app like it does
// from the explorer.
func TestQuitFromEditorNormalMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = tm.(Model)
	if m.activeWS().Panes.FocusedInstance().Kind() != pane.KindEditor {
		t.Fatal("opening a file should focus the editor")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Text: "q", Code: 'q'})
	if cmd == nil {
		t.Fatal("q in editor normal mode should quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected QuitMsg, got %T", cmd())
	}
}

// When the editor is in insert mode, a literal "q" must reach the buffer rather
// than quitting the app.
func TestQNotQuitWhileTyping(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newSized()
	tm, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = tm.(Model)
	tm, _ = m.Update(tea.KeyPressMsg{Text: "i", Code: 'i'}) // insert mode
	m = tm.(Model)
	_, cmd := m.Update(tea.KeyPressMsg{Text: "q", Code: 'q'})
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("q while typing should not quit")
		}
	}
}

// TestHelpOverlayToggle verifies "?" opens the help overlay, that while open it
// swallows keys (tab does not switch focus), and that "esc" dismisses it.
func TestHelpOverlayToggle(t *testing.T) {
	reg := registry.New()
	reg.Add(fakePlugin{id: "p", caps: plugin.Capabilities{Commands: []plugin.Command{
		{ID: "p.hello", Title: "Hello", Scope: plugin.GlobalScope()},
	}}})
	m := NewWith(reg, host.MapConfig{})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = tm.(Model)

	tm, _ = m.Update(tea.KeyPressMsg{Text: "?", Code: '?'})
	m = tm.(Model)
	if !m.shell.IsOpen() {
		t.Fatal(`"?" should open the help overlay`)
	}
	if !strings.Contains(m.render(), "Hello") {
		t.Fatal("open overlay should render registered command")
	}

	// While open, tab is consumed by the overlay and must not switch focus.
	before := m.activeWS().Panes.Focused()
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = tm.(Model)
	if m.activeWS().Panes.Focused() != before {
		t.Fatal("overlay should swallow keys; focus changed")
	}

	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if m.shell.IsOpen() {
		t.Fatal(`"esc" should dismiss the help overlay`)
	}

	// F1 is an alias for "?" and opens the same overlay (here via the hardcoded
	// fallback — this test's registry does not register palette.keymapHelp).
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyF1})
	if !m.shell.IsOpen() {
		t.Fatal(`F1 should open the help overlay`)
	}
}

// TestOpenModalRequestFloatsPluginContent verifies the additive plugin seam:
// dispatching host.OpenModalRequest hosts arbitrary content in the floating
// shell, composited centered over the base layout.
func TestOpenModalRequestFloatsPluginContent(t *testing.T) {
	m := newSized()
	tm, _ := m.Update(host.OpenModalRequest{
		Title: "PLUGIN MODAL",
		View:  func() string { return "modal body" },
	})
	m = tm.(Model)
	if !m.shell.IsOpen() {
		t.Fatal("OpenModalRequest should open the floating shell")
	}
	// The underlined shell title styles every rune, so match on stripped text.
	v := ansi.Strip(m.render())
	if !strings.Contains(v, "PLUGIN MODAL") || !strings.Contains(v, "modal body") {
		t.Fatalf("modal content should be composited onto the canvas: %q", v)
	}
	if !strings.Contains(v, "EXPLORER") {
		t.Fatal("base layout should remain visible around the modal")
	}
	// The shell swallows keys and esc dismisses it.
	tm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = tm.(Model)
	if m.shell.IsOpen() {
		t.Fatal("esc should dismiss the modal")
	}
}

// TestHelpOverlayFloatsCentered verifies the help pane is composited as a
// centered floating box: the base canvas keeps its full width/height and the
// overlaid region carries the rounded border, while base content survives at the
// edges (the pane does not cover the whole screen).
func TestHelpOverlayFloatsCentered(t *testing.T) {
	reg := registry.New()
	reg.Add(fakePlugin{id: "p", caps: plugin.Capabilities{Commands: []plugin.Command{
		{ID: "p.hello", Title: "Hello", Scope: plugin.GlobalScope()},
	}}})
	m := NewWith(reg, host.MapConfig{})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = tm.(Model)
	tm, _ = m.Update(tea.KeyPressMsg{Text: "?", Code: '?'})
	m = tm.(Model)

	v := m.render()
	lines := strings.Split(v, "\n")
	if len(lines) != 30 {
		t.Fatalf("canvas height = %d, want 30", len(lines))
	}
	// Every canvas row keeps the full terminal width — the pane is spliced in,
	// not concatenated.
	for i, l := range lines {
		if w := lipgloss.Width(l); w != 100 {
			t.Fatalf("row %d width = %d, want 100", i, w)
		}
	}
	// Floating, not full-screen: the base layout (EXPLORER pane) survives around
	// the pane while the help pane and its content appear composited in the middle.
	// The underlined shell title styles every rune, so match on stripped text.
	stripped := ansi.Strip(v)
	if !strings.Contains(stripped, "EXPLORER") {
		t.Fatal("base layout should remain visible around the floating pane")
	}
	if !strings.Contains(stripped, "HELP") || !strings.Contains(stripped, "Hello") {
		t.Fatal("help pane and its content should be composited onto the canvas")
	}
}

// TestApplyPluginConfigSymmetric guards #133: a reload re-enables a plugin
// whose toggle flipped back, not just disables.
func TestApplyPluginConfigSymmetric(t *testing.T) {
	reg := registry.New()
	reg.Add(togglePlugin{})
	applyPluginConfig(reg, host.MapConfig{"plugins.toggle.enabled": "false"})
	if reg.IsEnabled("toggle") {
		t.Fatal("false should disable")
	}
	applyPluginConfig(reg, host.MapConfig{"plugins.toggle.enabled": "true"})
	if !reg.IsEnabled("toggle") {
		t.Fatal("true should re-enable")
	}
	applyPluginConfig(reg, host.MapConfig{})
	if !reg.IsEnabled("toggle") {
		t.Fatal("an absent key should mean enabled")
	}
}

type togglePlugin struct{}

func (togglePlugin) ID() string                        { return "toggle" }
func (togglePlugin) Capabilities() plugin.Capabilities { return plugin.Capabilities{} }

// TestCheatsheetLiveAndBlocked guards 0081/40: the help sheet reads the live
// table and shows blocked bindings with their
// dependency instead of hiding them.
func TestCheatsheetLiveAndBlocked(t *testing.T) {
	// The real blocked ledger emptied with 0320 (#466): stub one entry so the
	// sheet's blocked group stays covered.
	defer keymap.StubBlockedForTest("vcs.revertFile", "unit-test dependency")()
	m := sized(t, 120, 40)
	m.openHelp()
	m.help.Snapshot("")
	view := m.help.Render(200)
	plain := stripForTest(view)
	if !strings.Contains(plain, "blocked (dependency not landed)") {
		t.Fatalf("blocked group missing:\n%s", plain)
	}
	if !strings.Contains(plain, "✗ needs") {
		t.Fatal("blocked entries should carry their dependency")
	}
}

func stripForTest(s string) string { return ansiStripRe.ReplaceAllString(s, "") }

var ansiStripRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// TestExplorerToggleHidesAndRestores guards the JetBrains cmd+1 state machine
// (#268): focused tree hides (leaf gone, editors reflow), the next toggle
// brings it back at the remembered ratio with focus on it.
func TestExplorerToggleHidesAndRestores(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	if !m.explorerVisible() || m.activeWS().Panes.Focused() != pane.ExplorerKey {
		t.Fatal("precondition: visible focused explorer")
	}
	wantRatio, ok := explorerSplitRatio(m.activeWS().Tree)
	if !ok {
		t.Fatal("precondition: explorer split ratio resolvable")
	}

	tm, _ := m.Update(ToggleExplorerFocusMsg{}) // focused → hide
	m = tm.(Model)
	if m.explorerVisible() {
		t.Fatal("toggle on the focused explorer must hide it")
	}
	if m.activeWS().Panes.Focused() == pane.ExplorerKey {
		t.Fatal("hiding must move focus off the explorer")
	}
	if !m.activeWS().Panes.Has(pane.ExplorerKey) {
		t.Fatal("hiding must keep the pane instance registered")
	}

	tm, _ = m.Update(ToggleExplorerFocusMsg{}) // hidden → show + focus
	m = tm.(Model)
	if !m.explorerVisible() || m.activeWS().Panes.Focused() != pane.ExplorerKey {
		t.Fatal("toggle on a hidden explorer must show and focus it")
	}
	if got, _ := explorerSplitRatio(m.activeWS().Tree); got != wantRatio {
		t.Fatalf("restored ratio = %v, want remembered %v", got, wantRatio)
	}

	tm, _ = m.Update(ToggleExplorerFocusMsg{}) // focused again → hide again
	m = tm.(Model)
	if m.explorerVisible() {
		t.Fatal("third toggle must hide again")
	}
}

// TestRestoreLayoutAcceptsHiddenExplorer: a persisted tree without the
// explorer leaf (hidden via toggle) restores as saved instead of falling back
// to the default layout (#268).
func TestRestoreLayoutAcceptsHiddenExplorer(t *testing.T) {
	m := newSized() // rotates IKE_CONFIG_DIR to a fresh store
	dir := os.Getenv("IKE_CONFIG_DIR")
	tm, _ := m.Update(ToggleExplorerFocusMsg{}) // hide (persists the layout)
	m = tm.(Model)
	if m.explorerVisible() {
		t.Fatal("setup: explorer should be hidden")
	}

	// A fresh model over the same store restores the persisted layout
	// (newSized would rotate the store dir away, so pin it and call New).
	t.Setenv("IKE_CONFIG_DIR", dir)
	tm, _ = New().Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m2 := tm.(Model)
	if m2.explorerVisible() {
		t.Fatal("restored session must keep the explorer hidden")
	}
	if !m2.activeWS().Panes.Has(pane.ExplorerKey) {
		t.Fatal("restored session must still register the explorer instance")
	}
	tm, _ = m2.Update(ToggleExplorerFocusMsg{})
	m2 = tm.(Model)
	if !m2.explorerVisible() || m2.activeWS().Panes.Focused() != pane.ExplorerKey {
		t.Fatal("toggle must bring the restored hidden explorer back")
	}
}

// TestDefinitionCandidatesOpenPicker guards the multi-target go-to-definition
// picker (#279): several candidates open the palette list (with the
// definitions placeholder) instead of guessing the first location.
func TestDefinitionCandidatesOpenPicker(t *testing.T) {
	m := newSized()
	tm, _ := m.Update(ilsp.DefinitionCandidatesMsg{Refs: []ilsp.Reference{
		{Path: "a.go", Line: 1, Preview: "func A()"},
		{Path: "b.go", Line: 9, Preview: "func A()"},
	}})
	m = tm.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("two definition candidates must open the picker palette")
	}
	if got := m.refs.Placeholder(); got != "Definitions — pick a target…" {
		t.Fatalf("placeholder = %q, want the definitions hint", got)
	}

	// A later find-references run resets the hint to the usages default.
	tm, _ = m.Update(ilsp.ReferencesMsg{Refs: []ilsp.Reference{
		{Path: "a.go", Line: 1}, {Path: "b.go", Line: 2},
	}})
	m = tm.(Model)
	if got := m.refs.Placeholder(); got != "2 usages — filter by file or text…" {
		t.Fatalf("placeholder after references = %q, want the counted usages default (#860)", got)
	}
}

// TestSymbolLiveMode guards the live workspace-symbol flow (0250 phase 2,
// #295): SymbolPromptMsg installs the re-query hook and opens the palette
// locked to the symbol mode; typing debounces into QueryChanged; results
// cache into rows (stale queries dropped) and refresh the open palette;
// no-provider stays an honest toast.
func TestSymbolLiveMode(t *testing.T) {
	m := newSized()
	var applied []string
	tm, _ := m.Update(ilsp.SymbolPromptMsg{Apply: func(q string) tea.Cmd {
		applied = append(applied, q)
		return nil
	}})
	m = tm.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("SymbolPromptMsg must open the symbol palette")
	}

	// Typing schedules a debounce tick; delivering it re-queries the source.
	var tick tea.Cmd
	for _, r := range "Help" {
		tm, tick = m.Update(tea.KeyPressMsg{Text: string(r), Code: r})
		m = tm.(Model)
	}
	if tick == nil {
		t.Fatal("typing in a live mode must schedule the debounce tick")
	}
	tm, _ = m.Update(palette.LiveTickMsg{Gen: 4}) // 4 edits -> gen 4
	m = tm.(Model)
	if len(applied) != 1 || applied[0] != "Help" {
		t.Fatalf("settled tick must re-query once with the query, got %v", applied)
	}

	// Results for the latest query cache into rows; stale replies drop.
	tm, _ = m.Update(ilsp.SymbolResultsMsg{Query: "Help", Hits: []ilsp.SymbolHit{
		{Name: "Helper", Ref: ilsp.Reference{Path: "a.go", Line: 3, Preview: "func Helper() string {"}},
	}})
	m = tm.(Model)
	if len(m.symbols.items) != 1 || m.symbols.items[0].item.Title != "Helper" {
		t.Fatalf("hits must cache as rows, got %+v", m.symbols.items)
	}
	tm, _ = m.Update(ilsp.SymbolResultsMsg{Query: "stale", Hits: []ilsp.SymbolHit{
		{Name: "Wrong", Ref: ilsp.Reference{Path: "b.go"}},
	}})
	m = tm.(Model)
	if m.symbols.items[0].item.Title != "Helper" {
		t.Fatal("a stale reply must not overwrite newer rows")
	}

	// No provider toasts and leaves the palette alone.
	tm, _ = m.Update(ilsp.SymbolResultsMsg{NoProvider: true})
	m = tm.(Model)
	tm, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = tm.(Model)
	if len(m.toasts) != 1 {
		t.Fatalf("no-provider must toast, got %d", len(m.toasts))
	}
}

// TestSearchEverywherePrimesSymbolHook guards the #295 search-everywhere
// seat: the first ShowSearchEverywhereMsg silently primes the bridge
// continuation via project.goToClass without opening the symbol palette.
func TestSearchEverywherePrimesSymbolHook(t *testing.T) {
	m := newSized()
	prompt := func(host.API) tea.Cmd {
		return func() tea.Msg {
			return ilsp.SymbolPromptMsg{Apply: func(string) tea.Cmd { return nil }}
		}
	}
	m.reg.Add(fakePlugin{id: "lsptest", caps: plugin.Capabilities{Commands: []plugin.Command{{
		ID: "project.goToClass", Title: "Go to symbol", Scope: plugin.GlobalScope(), Run: prompt,
	}}}})

	tm, cmd := m.Update(ShowSearchEverywhereMsg{})
	m = tm.(Model)
	if cmd == nil {
		t.Fatal("the first search-everywhere open must prime the symbol hook")
	}
	tm, _ = m.Update(cmd())
	m = tm.(Model)
	if m.symbols.request == nil {
		t.Fatal("priming must install the bridge continuation")
	}
	if got := m.palette.View(); !strings.Contains(got, "Search everywhere") {
		t.Fatal("priming must keep the search-everywhere palette open")
	}
}
