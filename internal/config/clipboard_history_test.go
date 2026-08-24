package config

// clipboard_history_test.go covers editor.clipboard_history_size (#2061): the
// ring the paste-from-history picker lists defaults to JetBrains' 20 entries
// and is clamped to a usable range — 0 would make cmd+shift+v useless, and a
// five-digit ring is an archive, not a picker.

import "testing"

func TestClipboardHistorySizeDefaultAndClamp(t *testing.T) {
	c, _ := Load(Options{})
	if c.Editor.ClipboardHistorySize != 20 {
		t.Errorf("default clipboard_history_size should be 20, got %d", c.Editor.ClipboardHistorySize)
	}

	proj := writeProject(t, "[editor]\nclipboard_history_size = 0\n")
	c, diags := Load(Options{ProjectRoot: proj})
	if c.Editor.ClipboardHistorySize != 1 {
		t.Errorf("clipboard_history_size should clamp to 1, got %d", c.Editor.ClipboardHistorySize)
	}
	if len(diags) != 1 {
		t.Errorf("expected one clamp diagnostic, got %v", diags)
	}

	proj = writeProject(t, "[editor]\nclipboard_history_size = 5000\n")
	c, diags = Load(Options{ProjectRoot: proj})
	if c.Editor.ClipboardHistorySize != 200 {
		t.Errorf("clipboard_history_size should clamp to 200, got %d", c.Editor.ClipboardHistorySize)
	}
	if len(diags) != 1 {
		t.Errorf("expected one clamp diagnostic, got %v", diags)
	}

	proj = writeProject(t, "[editor]\nclipboard_history_size = 42\n")
	c, diags = Load(Options{ProjectRoot: proj})
	if c.Editor.ClipboardHistorySize != 42 {
		t.Errorf("an in-range size must stick, got %d", c.Editor.ClipboardHistorySize)
	}
	if len(diags) != 0 {
		t.Errorf("an in-range size must not warn, got %v", diags)
	}
	if got := c.Flat()["editor.clipboard_history_size"]; got != "42" {
		t.Errorf("flat view = %q, want \"42\"", got)
	}
}
