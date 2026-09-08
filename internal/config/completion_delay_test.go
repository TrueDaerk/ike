package config

import "testing"

// completion_delay_test.go covers lsp.completion_delay_ms (#2541): the
// identifier-rune completion debounce is a bounded, defaulted config key.

func TestCompletionDelayDefaultsAndClamp(t *testing.T) {
	c, _ := Load(Options{})
	if c.LSP.CompletionDelayMs != 100 {
		t.Errorf("completion_delay_ms default = %d, want 100", c.LSP.CompletionDelayMs)
	}
	if v, ok := c.Flat()["lsp.completion_delay_ms"]; !ok || v != "100" {
		t.Errorf("Flat must expose lsp.completion_delay_ms, got %q,%v", v, ok)
	}
	proj := writeProject(t, "[lsp]\ncompletion_delay_ms = -5\n")
	c, diags := Load(Options{ProjectRoot: proj})
	if c.LSP.CompletionDelayMs != 0 {
		t.Errorf("a negative delay should clamp to 0, got %d", c.LSP.CompletionDelayMs)
	}
	if len(diags) != 1 || diags[0].Field != "lsp.completion_delay_ms" {
		t.Errorf("expected one clamp diagnostic on the key, got %v", diags)
	}
	proj = writeProject(t, "[lsp]\ncompletion_delay_ms = 9999\n")
	c, diags = Load(Options{ProjectRoot: proj})
	if c.LSP.CompletionDelayMs != 2000 {
		t.Errorf("an oversized delay should clamp to 2000, got %d", c.LSP.CompletionDelayMs)
	}
	if len(diags) != 1 {
		t.Errorf("expected one clamp diagnostic, got %v", diags)
	}
	proj = writeProject(t, "[lsp]\ncompletion_delay_ms = 0\n")
	c, diags = Load(Options{ProjectRoot: proj})
	if c.LSP.CompletionDelayMs != 0 || len(diags) != 0 {
		t.Errorf("0 is a valid immediate request, got %d %v", c.LSP.CompletionDelayMs, diags)
	}
}
