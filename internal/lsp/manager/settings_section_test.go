package manager

import (
	"reflect"
	"testing"
)

// TestSettingsSectionAbsentAnswersEmptyObject guards #1061: an absent
// workspace/configuration section answers {} — never null. A null answer
// silently disables vscode-css-language-server's validation; VS Code
// effectively provides a defaults object, so {} is what servers expect.
func TestSettingsSectionAbsentAnswersEmptyObject(t *testing.T) {
	settings := map[string]any{
		"python": map[string]any{"pythonPath": "/venv/bin/python"},
	}
	// Present sections keep the merged settings (#563 path).
	if got := settingsSection(settings, "python"); !reflect.DeepEqual(got, settings["python"]) {
		t.Fatalf("python section = %#v", got)
	}
	if got := settingsSection(settings, ""); !reflect.DeepEqual(got, any(settings)) {
		t.Fatalf("empty section = %#v", got)
	}
	// Absent sections — top-level and dotted, including a path through a
	// non-map — all answer an empty object.
	for _, section := range []string{"css", "python.analysis.missing", "python.pythonPath.x"} {
		got := settingsSection(settings, section)
		mm, ok := got.(map[string]any)
		if !ok || len(mm) != 0 {
			t.Errorf("settingsSection(%q) = %#v, want empty object", section, got)
		}
	}
}
