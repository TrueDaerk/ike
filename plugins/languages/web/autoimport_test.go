package langweb

import (
	"testing"

	"ike/internal/lang"
)

// TestVtslsAutoImportsOn guards #2610: vtsls must offer exports of other
// modules with the import statement (delivered by completionItem/resolve),
// for TypeScript and JavaScript alike: suggest.autoImports pinned on and
// package.json dependencies included. The settings ride along with the
// toolchain's typescript.tsdk without displacing it.
func TestVtslsAutoImportsOn(t *testing.T) {
	l, ok := lang.ByID("typescript")
	if !ok || l.Server == nil {
		t.Fatal("typescript: no language/server registered")
	}
	merged := lang.MergeSettings(map[string]any{"typescript": map[string]any{"tsdk": "/w/node_modules/typescript/lib"}}, l.Server.Settings)
	for _, section := range []string{"typescript", "javascript"} {
		s, _ := merged[section].(map[string]any)
		suggest, _ := s["suggest"].(map[string]any)
		if v, ok := suggest["autoImports"].(bool); !ok || !v {
			t.Errorf("%s.suggest.autoImports = %v, want true (settings %v)", section, suggest["autoImports"], l.Server.Settings)
		}
		prefs, _ := s["preferences"].(map[string]any)
		if v, _ := prefs["includePackageJsonAutoImports"].(string); v != "auto" {
			t.Errorf("%s.preferences.includePackageJsonAutoImports = %q, want auto", section, v)
		}
	}
	if ts, _ := merged["typescript"].(map[string]any); ts["tsdk"] != "/w/node_modules/typescript/lib" {
		t.Errorf("toolchain tsdk lost in merge: %v", merged["typescript"])
	}
}
