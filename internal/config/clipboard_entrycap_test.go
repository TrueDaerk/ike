package config

// clipboard_entrycap_test.go covers editor.clipboard_history_max_kb (#2250):
// the per-entry size cap on the paste-from-history ring defaults to 256 KB and
// is clamped to a range that keeps the ring a picker rather than a memory leak.

import "testing"

func TestClipboardHistoryMaxKBDefaultAndClamp(t *testing.T) {
	c, _ := Load(Options{})
	if c.Editor.ClipboardHistoryMaxKB != 256 {
		t.Errorf("default clipboard_history_max_kb should be 256, got %d", c.Editor.ClipboardHistoryMaxKB)
	}

	proj := writeProject(t, "[editor]\nclipboard_history_max_kb = 0\n")
	c, diags := Load(Options{ProjectRoot: proj})
	if c.Editor.ClipboardHistoryMaxKB != 1 {
		t.Errorf("clipboard_history_max_kb should clamp to 1, got %d", c.Editor.ClipboardHistoryMaxKB)
	}
	if len(diags) != 1 {
		t.Errorf("expected one clamp diagnostic, got %v", diags)
	}

	proj = writeProject(t, "[editor]\nclipboard_history_max_kb = 999999\n")
	c, diags = Load(Options{ProjectRoot: proj})
	if c.Editor.ClipboardHistoryMaxKB != 10240 {
		t.Errorf("clipboard_history_max_kb should clamp to 10240, got %d", c.Editor.ClipboardHistoryMaxKB)
	}
	if len(diags) != 1 {
		t.Errorf("expected one clamp diagnostic, got %v", diags)
	}

	proj = writeProject(t, "[editor]\nclipboard_history_max_kb = 64\n")
	c, diags = Load(Options{ProjectRoot: proj})
	if c.Editor.ClipboardHistoryMaxKB != 64 {
		t.Errorf("an in-range cap must stick, got %d", c.Editor.ClipboardHistoryMaxKB)
	}
	if len(diags) != 0 {
		t.Errorf("an in-range cap must not warn, got %v", diags)
	}
	if got := c.Flat()["editor.clipboard_history_max_kb"]; got != "64" {
		t.Errorf("flat view = %q, want \"64\"", got)
	}
}
