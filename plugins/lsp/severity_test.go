package lsp

import (
	"reflect"
	"testing"

	"ike/internal/config"
	"ike/internal/lang"
)

// TestResolveSpecFoldsNativeSeverityOverrides guards the native pass-through
// (#1503): a server declaring SeverityOverridesPath gets the exact-code
// lsp.diagnostics_severity rules nested into its settings at that path, in
// the server's vocabulary, with explicit [lsp.servers.<id>] settings winning.
func TestResolveSpecFoldsNativeSeverityOverrides(t *testing.T) {
	lang.Register(lang.Language{
		ID: "sevtest",
		Server: &lang.ServerSpec{
			Language:              "sevtest",
			Command:               "sev-ls",
			SeverityOverridesPath: []string{"python", "analysis", "diagnosticSeverityOverrides"},
		},
	})
	defer config.Set(config.Get())

	c := &config.Config{}
	c.LSP.Enabled = true
	c.LSP.DiagnosticsSeverity = []string{
		"reportArgumentType warning",
		"reportUnusedImport off",
		"code=report* error", // wildcard: client-side only
	}
	config.Set(c)

	spec, ok := resolveSpec("sevtest")
	if !ok {
		t.Fatal("spec must resolve")
	}
	want := map[string]any{
		"reportArgumentType": "warning",
		"reportUnusedImport": "none",
	}
	got := spec.Settings["python"].(map[string]any)["analysis"].(map[string]any)["diagnosticSeverityOverrides"]
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("native overrides = %#v, want %#v", got, want)
	}

	// A user's explicit server settings win over the derived overrides.
	c.LSP.Servers = map[string]map[string]any{"sevtest": {"settings": map[string]any{
		"python": map[string]any{"analysis": map[string]any{"diagnosticSeverityOverrides": map[string]any{
			"reportArgumentType": "error",
		}}},
	}}}
	config.Set(c)
	spec, _ = resolveSpec("sevtest")
	got = spec.Settings["python"].(map[string]any)["analysis"].(map[string]any)["diagnosticSeverityOverrides"]
	m := got.(map[string]any)
	if m["reportArgumentType"] != "error" || m["reportUnusedImport"] != "none" {
		t.Fatalf("user settings must win per key, got %#v", got)
	}

	// Without a declared path nothing is injected.
	lang.Register(lang.Language{
		ID:     "sevtest2",
		Server: &lang.ServerSpec{Language: "sevtest2", Command: "sev2-ls"},
	})
	spec, _ = resolveSpec("sevtest2")
	if spec.Settings != nil {
		t.Fatalf("no path declared, settings must stay nil, got %#v", spec.Settings)
	}
}
