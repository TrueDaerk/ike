package app

import (
	"strings"
	"testing"

	"ike/internal/config"
)

// keymap_shadow_diag_test.go covers the #1875 surfacing: a user binding that
// shadows a default across contexts (the "editor.f1 hides the global
// cheatsheet chord" shape of the cmd+e repro) toasts a warning on reload,
// deduped per session like the other config diagnostics.

func TestKeymapShadowSurfacesAsNotification(t *testing.T) {
	m := sized(t, 100, 40)
	base := len(m.history)
	cfg := *config.Get()
	cfg.Keymap.Bindings = map[string]string{"editor.f1": "http.selectEnvironment"}
	out, _ := m.Update(config.ConfigReloadedMsg{Config: &cfg})
	m = out.(Model)
	var hit string
	for _, h := range m.history[:len(m.history)-base] {
		if strings.Contains(h.text, "keymap shadow") {
			hit = h.text
		}
	}
	if hit == "" {
		t.Fatalf("no keymap-shadow notification after reload: %+v", m.history)
	}
	// The warning names both commands and the resolution key.
	for _, want := range []string{"http.selectEnvironment", "palette.keymapHelp", "editor.f1"} {
		if !strings.Contains(hit, want) {
			t.Errorf("notification %q missing %q", hit, want)
		}
	}
	// A second reload with the same config does not re-toast.
	grown := len(m.history)
	out, _ = m.Update(config.ConfigReloadedMsg{Config: &cfg})
	m = out.(Model)
	if len(m.history) != grown {
		t.Fatalf("repeated shadow diagnostic must dedupe, history grew to %d", len(m.history)-grown)
	}
}
