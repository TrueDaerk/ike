package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"ike/internal/editor"
	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/plugin"
	"ike/internal/registry"
)

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
	if out.(Model).editor.HasFile() {
		t.Fatal("editor should not load a handler-claimed file")
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

	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
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

func newSized() Model {
	m := New()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return tm.(Model)
}

func TestTabSwitchesFocus(t *testing.T) {
	m := newSized()
	if m.focus != focusExplorer {
		t.Fatal("should start focused on explorer")
	}
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = tm.(Model)
	if m.focus != focusEditor {
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
	if !m.editor.HasFile() {
		t.Fatal("editor should have loaded the file")
	}
	if m.focus != focusEditor {
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
	if m.editor.HasFile() {
		t.Fatal("close should detach the file")
	}
	if m.focus != focusExplorer {
		t.Fatal("close should focus explorer")
	}
}

func TestQuitFromExplorer(t *testing.T) {
	m := newSized()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("q in explorer should quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected QuitMsg, got %T", cmd())
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
	if m.focus != focusEditor {
		t.Fatal("opening a file should focus the editor")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
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
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")}) // insert mode
	m = tm.(Model)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
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

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = tm.(Model)
	if !m.shell.IsOpen() {
		t.Fatal(`"?" should open the help overlay`)
	}
	if !strings.Contains(m.View(), "Hello") {
		t.Fatal("open overlay should render registered command")
	}

	// While open, tab is consumed by the overlay and must not switch focus.
	before := m.focus
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = tm.(Model)
	if m.focus != before {
		t.Fatal("overlay should swallow keys; focus changed")
	}

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = tm.(Model)
	if m.shell.IsOpen() {
		t.Fatal(`"esc" should dismiss the help overlay`)
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
	v := m.View()
	if !strings.Contains(v, "PLUGIN MODAL") || !strings.Contains(v, "modal body") {
		t.Fatalf("modal content should be composited onto the canvas: %q", v)
	}
	if !strings.Contains(v, "EXPLORER") {
		t.Fatal("base layout should remain visible around the modal")
	}
	// The shell swallows keys and esc dismisses it.
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
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
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = tm.(Model)

	v := m.View()
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
	if !strings.Contains(v, "EXPLORER") {
		t.Fatal("base layout should remain visible around the floating pane")
	}
	if !strings.Contains(v, "HELP") || !strings.Contains(v, "Hello") {
		t.Fatal("help pane and its content should be composited onto the canvas")
	}
}
