package app

import (
	"os"
	"path/filepath"

	"ike/internal/layout"
)

// Layout persistence is runtime UI state, not user configuration, so it lives in
// its own per-project state file rather than settings.toml. The discovery seam
// mirrors what Roadmap 0040 will expose: IKE_CONFIG_DIR (or an explicit path)
// overrides the default location so tests can redirect writes. Save is called
// only on drag release, never per motion frame.

// layoutFile returns the path of the per-project layout state file. When
// IKE_CONFIG_DIR is set its value is used as the base directory; otherwise the
// store lives under the project's own ".ike" directory.
func layoutFile() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "layout.json")
	}
	return filepath.Join(".ike", "layout.json")
}

// loadLayout reads the saved tree, validating it against the live pane set. It
// returns ok=false on any missing, unreadable, or stale file so the caller falls
// back to the default layout.
func loadLayout(valid map[string]bool) (layout.Node, bool) {
	data, err := os.ReadFile(layoutFile())
	if err != nil {
		return nil, false
	}
	return layout.Decode(data, valid)
}

// saveLayout persists root to the per-project state file, creating the parent
// directory as needed. Errors are swallowed: failing to persist layout must
// never disrupt the session.
func saveLayout(root layout.Node) {
	data, err := layout.Encode(root)
	if err != nil {
		return
	}
	path := layoutFile()
	if dir := filepath.Dir(path); dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	_ = os.WriteFile(path, data, 0o644)
}
