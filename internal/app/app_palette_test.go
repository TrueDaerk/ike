package app

import (
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
	m := sized(t, 100, 40)
	if m.palette.IsOpen() {
		t.Fatal("palette should start closed")
	}
	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("ctrl+p should open the palette")
	}
	// A key while open is consumed by the palette, not routed to panes.
	out, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)
	if m.palette.IsOpen() {
		t.Fatal("esc should close the palette")
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
	if _, ok := cmd().(sentinelMsg); !ok {
		t.Fatalf("want sentinelMsg from command, got %T", cmd())
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
