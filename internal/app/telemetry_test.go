package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/layout"
	"ike/internal/menu"
	"ike/internal/palette"
	"ike/internal/plugin"
	"ike/internal/registry"
	"ike/internal/telemetry"
)

// usageEvents flushes m's recorder and returns every event written for this
// model's isolated config dir.
func usageEvents(t *testing.T, m Model) []telemetry.Event {
	t.Helper()
	m.usage.Flush()
	dir := filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "telemetry")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []telemetry.Event
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			if line == "" {
				continue
			}
			var ev telemetry.Event
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				t.Fatalf("bad JSONL line %q: %v", line, err)
			}
			out = append(out, ev)
		}
	}
	return out
}

// eventsOf filters by type.
func eventsOf(evs []telemetry.Event, typ string) []telemetry.Event {
	var out []telemetry.Event
	for _, ev := range evs {
		if ev.Type == typ {
			out = append(out, ev)
		}
	}
	return out
}

// telemetryModel builds an isolated model with one bindable test command.
func telemetryModel(t *testing.T, cfg host.MapConfig) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	reg := registry.New()
	reg.Add(fakePlugin{id: "tm", caps: plugin.Capabilities{Commands: []plugin.Command{{
		ID: "tm.fire", Title: "Fire", Scope: plugin.GlobalScope(),
		Run: func(host.API) tea.Cmd { return nil },
	}}}})
	m := NewWith(reg, cfg)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	return tm.(Model)
}

// TestTelemetryKeybindRecordsCommandAndKey drives a bound chord and expects
// both a key "resolved" event and a command event sourced "keybind".
func TestTelemetryKeybindRecordsCommandAndKey(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{"keymap.bindings.ctrl+y": "tm.fire"})
	tm, _ := m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	m = tm.(Model)

	evs := usageEvents(t, m)
	cmds := eventsOf(evs, telemetry.TypeCommand)
	if len(cmds) != 1 || cmds[0].Data["id"] != "tm.fire" || cmds[0].Data["source"] != telemetry.SourceKeybind {
		t.Fatalf("want one keybind-sourced command event for tm.fire, got %v", cmds)
	}
	keys := eventsOf(evs, telemetry.TypeKey)
	if len(keys) != 1 || keys[0].Data["chord"] != "ctrl+y" || keys[0].Data["status"] != "resolved" ||
		keys[0].Data["command"] != "tm.fire" {
		t.Fatalf("want one resolved key event for ctrl+y, got %v", keys)
	}
}

// TestTelemetryPaletteAndMenuSources checks the invocation source survives the
// palette and menu dispatch paths.
func TestTelemetryPaletteAndMenuSources(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	tm, _ := m.Update(palette.RunCommandMsg{ID: "tm.fire"})
	m = tm.(Model)
	tm, _ = m.Update(menu.RunMsg{Command: "tm.fire"})
	m = tm.(Model)

	cmds := eventsOf(usageEvents(t, m), telemetry.TypeCommand)
	var sources []string
	for _, ev := range cmds {
		if ev.Data["id"] == "tm.fire" {
			sources = append(sources, ev.Data["source"])
		}
	}
	if len(sources) != 2 || sources[0] != telemetry.SourcePalette || sources[1] != telemetry.SourceMenu {
		t.Fatalf("want palette then menu sources, got %v", sources)
	}
}

// TestTelemetryUnboundChordRecorded expects a modified chord without a binding
// to land as an "unbound" key event — the missing-keybind signal.
func TestTelemetryUnboundChordRecorded(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	// ctrl+alt+0 is bound to nothing in any preset.
	tm, _ := m.Update(tea.KeyPressMsg{Code: '0', Mod: tea.ModCtrl | tea.ModAlt})
	m = tm.(Model)

	keys := eventsOf(usageEvents(t, m), telemetry.TypeKey)
	found := false
	for _, ev := range keys {
		if ev.Data["status"] == "unbound" && strings.Contains(ev.Data["chord"], "0") {
			found = true
		}
	}
	if !found {
		t.Fatalf("unbound modified chord not recorded; key events: %v", keys)
	}
}

// TestTelemetryPlainTypingNeverRecorded is the privacy line: unmodified (and
// shift-only) presses must never appear in the log, bound or not.
func TestTelemetryPlainTypingNeverRecorded(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	m.cycleFocus() // focus the editor: plain keys are typing
	for _, r := range "secret" {
		tm, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = tm.(Model)
	}
	tm, _ := m.Update(tea.KeyPressMsg{Code: 'S', Text: "S", Mod: tea.ModShift})
	m = tm.(Model)

	for _, ev := range eventsOf(usageEvents(t, m), telemetry.TypeKey) {
		if ev.Data["status"] != "unbound" {
			continue
		}
		c := ev.Data["chord"]
		if !strings.Contains(c, "ctrl") && !strings.Contains(c, "alt") && !strings.Contains(c, "cmd") {
			t.Fatalf("plain press %q leaked into the log", c)
		}
	}
}

// TestTelemetryLayoutEvents checks the structural layout hooks: split, pane
// focus and resize land as layout events with structural payloads only.
func TestTelemetryLayoutEvents(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	m.cycleFocus()
	m.SplitFocused(layout.ZoneRight)
	m.cycleFocus()
	m.resizeFocusedPane(layout.ZoneRight)

	evs := eventsOf(usageEvents(t, m), telemetry.TypeLayout)
	ops := map[string]int{}
	for _, ev := range evs {
		ops[ev.Data["op"]]++
	}
	if ops["split"] != 1 {
		t.Fatalf("want one split event, got ops %v", ops)
	}
	if ops["pane.focus"] == 0 {
		t.Fatalf("want pane.focus events, got ops %v", ops)
	}
	if ops["resize"] != 1 {
		t.Fatalf("want one resize event, got ops %v", ops)
	}
	for _, ev := range evs {
		if ev.Data["op"] == "split" && ev.Data["zone"] != "right" {
			t.Fatalf("split zone = %q, want right", ev.Data["zone"])
		}
	}
}

// TestTelemetryDisabledWritesNothing flips the setting off and expects no
// telemetry file at all.
func TestTelemetryDisabledWritesNothing(t *testing.T) {
	prev := config.Get()
	off := *prev
	off.Telemetry.Enabled = false
	config.Set(&off)
	t.Cleanup(func() { config.Set(prev) })

	m := telemetryModel(t, host.MapConfig{"keymap.bindings.ctrl+y": "tm.fire"})
	tm, _ := m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	m = tm.(Model)
	m.cycleFocus()
	m.SplitFocused(layout.ZoneRight)
	m.usage.Flush()

	dir := filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "telemetry")
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		t.Fatalf("telemetry disabled but files were written: %v", entries)
	}
}

// TestTelemetryNoClearTextPaths opens a real file, exercises hooks, and scans
// every written line for the file's path segments — none may appear.
func TestTelemetryNoClearTextPaths(t *testing.T) {
	m := telemetryModel(t, host.MapConfig{})
	dir := t.TempDir()
	path := filepath.Join(dir, "very-secret-name.txt")
	if err := os.WriteFile(path, []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = tm.(Model)
	m.cycleFocus()
	m.SplitFocused(layout.ZoneRight)
	m.usage.Flush()

	tdir := filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "telemetry")
	entries, err := os.ReadDir(tdir)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(tdir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "very-secret-name") || strings.Contains(string(raw), "content") {
			t.Fatalf("telemetry leaked file path or content:\n%s", raw)
		}
	}
}

// TestTelemetryRecorderRidesProjectSwitch: the recorder is session state; the
// rebuilt model must carry the same one (same session id, one file per run).
func TestTelemetryRecorderRidesProjectSwitch(t *testing.T) {
	t.Chdir(t.TempDir()) // performSwitch chdirs into the target; restore after
	m := telemetryModel(t, host.MapConfig{})
	m.usage.Command("before.switch", telemetry.SourceInternal)
	sid := m.usage.SessionID()

	other := t.TempDir()
	tm, _ := m.performSwitch(other)
	fresh := tm.(Model)
	if fresh.usage != m.usage {
		t.Fatal("project switch built a new recorder; session state must ride across")
	}
	if fresh.usage.SessionID() != sid {
		t.Fatalf("session id changed across switch: %q -> %q", sid, fresh.usage.SessionID())
	}
	evs := eventsOf(usageEvents(t, fresh), telemetry.TypeLayout)
	found := false
	for _, ev := range evs {
		if ev.Data["op"] == "project.switch" {
			found = true
		}
	}
	if !found {
		t.Fatalf("project switch not recorded; layout events: %v", evs)
	}
}
