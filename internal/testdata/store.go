package testdata

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// store.go persists the last spec used per format (#2134), so the second
// "generate a CSV" starts from the field list of the first instead of the
// stock defaults. The store is **user-level** — a test-data shape is a habit,
// not a property of a project — and follows the same IKE_CONFIG_DIR
// redirection seam as every other user state file (cf. internal/app/layouts.go).
//
// A missing or malformed store is an empty store, never an error: failing to
// remember a preset must not stop a generation.

// configDirEnv mirrors config.Discover's user-layer override, so a sandboxed
// IKE keeps its presets in the sandbox too.
const configDirEnv = "IKE_CONFIG_DIR"

// presets is the on-disk schema: one saved spec per format id.
type presets struct {
	Specs map[Format]Spec `json:"specs"`
}

// StoreFile returns the path of the preset store, or "" when the home
// directory is undiscoverable (in which case presets simply do not persist).
func StoreFile() string {
	if d := os.Getenv(configDirEnv); d != "" {
		return filepath.Join(d, "testdata.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ike", "testdata.json")
}

// loadPresets reads the store; anything unreadable yields an empty store.
func loadPresets() presets {
	p := presets{Specs: map[Format]Spec{}}
	path := StoreFile()
	if path == "" {
		return p
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return p
	}
	var on presets
	if json.Unmarshal(data, &on) != nil || on.Specs == nil {
		return p
	}
	return on
}

// Preset returns the spec to start a generation of format from: the last one
// used for it when the store holds a *still valid* one, the stock Default
// otherwise. Validating on read is what keeps a hand-edited store — or one
// written by an older IKE whose catalog had a kind this build dropped — from
// putting an unusable spec into the wizard.
func Preset(format Format) Spec {
	saved, ok := loadPresets().Specs[format]
	if !ok {
		return Default(format)
	}
	saved.Format = format
	if saved.Validate() != nil {
		return Default(format)
	}
	return saved.Normalized()
}

// SavePreset remembers spec as the starting point for its format. Errors are
// swallowed like the layout store's: not persisting a preference must never
// disrupt the session.
func SavePreset(spec Spec) {
	if !spec.Format.Valid() {
		return
	}
	path := StoreFile()
	if path == "" {
		return
	}
	p := loadPresets()
	if p.Specs == nil {
		p.Specs = map[Format]Spec{}
	}
	p.Specs[spec.Format] = spec.Normalized()
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, data, 0o644)
}
