package langphp

import (
	"testing"

	"ike/internal/lang"
)

// TestIntelephenseInsertUseDeclarationOn guards #2610: accepting a class
// from another namespace must add its `use` declaration, so
// intelephense.completion.insertUseDeclaration is pinned on — under the same
// "intelephense" section the toolchain's phpVersion lands in, which the
// merge must keep.
func TestIntelephenseInsertUseDeclarationOn(t *testing.T) {
	l, ok := lang.ByID("php")
	if !ok || l.Server == nil {
		t.Fatal("php: no language/server registered")
	}
	merged := lang.MergeSettings(map[string]any{
		"intelephense": map[string]any{"environment": map[string]any{"phpVersion": "8.2"}},
	}, l.Server.Settings)
	in, _ := merged["intelephense"].(map[string]any)
	comp, _ := in["completion"].(map[string]any)
	if v, ok := comp["insertUseDeclaration"].(bool); !ok || !v {
		t.Errorf("settings = %v, want intelephense.completion.insertUseDeclaration: true", l.Server.Settings)
	}
	env, _ := in["environment"].(map[string]any)
	if env["phpVersion"] != "8.2" {
		t.Errorf("toolchain phpVersion lost in merge: %v", merged)
	}
}
