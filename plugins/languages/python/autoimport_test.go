package langpython

import (
	"testing"

	"ike/internal/lang"
)

// TestPyrightAutoImportCompletionsOn guards #2610: pyright (and basedpyright,
// which defaults it off) must offer unimported symbols, so
// python.analysis.autoImportCompletions travels in the server settings —
// both as initializationOptions and as the workspace/configuration answer for
// the "python" section — and survives the toolchain merge that adds the
// interpreter path.
func TestPyrightAutoImportCompletionsOn(t *testing.T) {
	l, ok := lang.ByID("python")
	if !ok || l.Server == nil {
		t.Fatal("python: no language/server registered")
	}
	check := func(settings map[string]any) {
		t.Helper()
		py, _ := settings["python"].(map[string]any)
		an, _ := py["analysis"].(map[string]any)
		if v, ok := an["autoImportCompletions"].(bool); !ok || !v {
			t.Errorf("settings = %v, want python.analysis.autoImportCompletions: true", settings)
		}
	}
	check(l.Server.Settings)
	check(lang.MergeSettings(toolchain{}.Explicit("/usr/bin/python3"), l.Server.Settings))
	if got := string(l.Server.SettingsJSON()); got == "" {
		t.Fatal("SettingsJSON empty")
	}
}
