package app

import (
	"os"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/palette"
	"ike/internal/plugin"
	"ike/internal/registry"
)

// sentinelMsg is emitted by a test command to prove the palette drove it.
type sentinelMsg struct{}

// palettePlugin registers a single global command emitting sentinelMsg.
type palettePlugin struct{}

func (palettePlugin) ID() string { return "test" }
func (palettePlugin) Capabilities() plugin.Capabilities {
	return plugin.Capabilities{Commands: []plugin.Command{{
		ID:    "test.ping",
		Title: "Ping",
		Scope: plugin.GlobalScope(),
		Run:   func(host.API) tea.Cmd { return func() tea.Msg { return sentinelMsg{} } },
	}}}
}

func TestPaletteToggleKeyOpens(t *testing.T) {
	// The toggle chord defaults to empty (#523: ctrl+p belongs to
	// lsp.parameterInfo); a configured key still opens the palette.
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := NewWith(registry.New(), host.MapConfig{"palette.toggle_key": "ctrl+g"})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = out.(Model)
	if m.palette.IsOpen() {
		t.Fatal("palette should start closed")
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("the configured toggle key should open the palette")
	}
	// A key while open is consumed by the palette, not routed to panes.
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.palette.IsOpen() {
		t.Fatal("esc should close the palette")
	}
}

func TestPaletteToggleKeyDefaultsEmpty(t *testing.T) {
	m := sized(t, 100, 40)
	out, _ := m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	m = out.(Model)
	if m.palette.IsOpen() {
		t.Fatal("ctrl+p must not open the palette by default (#523: bound to lsp.parameterInfo)")
	}
}

func TestPaletteRunCommandMsgRunsCommand(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	reg := registry.New()
	reg.Add(palettePlugin{})
	m := NewWith(reg, host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = out.(Model)

	_, cmd := m.Update(palette.RunCommandMsg{ID: "test.ping"})
	if cmd == nil {
		t.Fatal("RunCommandMsg should run the command")
	}
	var ran, executed bool
	for _, msg := range cmdMsgs(cmd) {
		switch v := msg.(type) {
		case sentinelMsg:
			ran = true
		case CommandExecutedMsg:
			executed = v.ID == "test.ping"
		}
	}
	if !ran {
		t.Fatal("want sentinelMsg from command dispatch")
	}
	// #679: palette dispatch emits the in-app command-executed signal.
	if !executed {
		t.Fatal("want CommandExecutedMsg{test.ping} from palette dispatch")
	}
}

func TestEscEscOpensPalette(t *testing.T) {
	m := sized(t, 100, 40)
	m.cycleFocus() // focus the editor (normal mode, not capturing)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m.nowFn = func() time.Time { return now }
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.palette.IsOpen() {
		t.Fatal("a single esc should not open the palette")
	}
	// Second esc arrives well within escEscTimeout (#1750).
	now = now.Add(100 * time.Millisecond)
	m.nowFn = func() time.Time { return now }
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("esc-esc should open the palette")
	}
	if m.palette.Anchored() {
		t.Fatal("esc-esc opens the centered palette, not anchored")
	}
}

func TestEscEscOutsideTimeoutDoesNotOpenPalette(t *testing.T) {
	m := sized(t, 100, 40)
	m.cycleFocus() // focus the editor (normal mode, not capturing)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m.nowFn = func() time.Time { return now }
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.palette.IsOpen() {
		t.Fatal("a single esc should not open the palette")
	}
	// Second esc arrives well after escEscTimeout (#1750): it should be
	// treated as a fresh first esc, not the second half of a double-tap.
	now = now.Add(1 * time.Second)
	m.nowFn = func() time.Time { return now }
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.palette.IsOpen() {
		t.Fatal("esc-esc beyond the timeout should not open the palette")
	}
	// A third esc arriving right after the second should now open it, since
	// the second esc re-armed the detector.
	now = now.Add(100 * time.Millisecond)
	m.nowFn = func() time.Time { return now }
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("esc-esc within the timeout after re-arming should open the palette")
	}
}

func TestAtKeyOpensAnchoredFileFinder(t *testing.T) {
	m := sized(t, 100, 40)
	m.cycleFocus() // focus the editor in normal mode
	out, _ := m.Update(tea.KeyPressMsg{Text: "@", Code: '@'})
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("@ in editor normal mode should open the file finder")
	}
	if !m.palette.Anchored() {
		t.Fatal("the editor file finder should be anchored to the pane")
	}
}

func TestPaletteOpenFileMsgOpensPath(t *testing.T) {
	m := sized(t, 100, 40)
	dir := t.TempDir()
	path := dir + "/note.txt"
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(palette.OpenFileMsg{Path: path})
	m = out.(Model)
	if ed := m.activeEditor(); ed == nil || !ed.HasFile() || ed.Path() != path {
		t.Fatalf("OpenFileMsg should load the file into an editor")
	}
}
