package testdata

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// store.go persists the generator's user state (#2134, reworked in #2392):
// the last spec used — so the next "Generate Test Data…" starts where the
// previous one left off — and the user's named templates (format-free DSL
// bodies). The store is **user-level** — a test-data shape is a habit, not a
// property of a project — and follows the same IKE_CONFIG_DIR redirection
// seam as every other user state file (cf. internal/app/layouts.go).
//
// A missing or malformed store is an empty store, never an error: failing to
// remember a spec must not stop a generation. The pre-#2392 per-format preset
// schema is simply unreadable under the new one — those presets were
// low-value and start over as the stock default.

// configDirEnv mirrors config.Discover's user-layer override, so a sandboxed
// IKE keeps its state in the sandbox too.
const configDirEnv = "IKE_CONFIG_DIR"

// storeSchema is the on-disk shape: the last spec plus the user templates,
// name → DSL body.
type storeSchema struct {
	Last      *Spec             `json:"last,omitempty"`
	Templates map[string]string `json:"templates,omitempty"`
}

// StoreFile returns the path of the store, or "" when the home directory is
// undiscoverable (in which case nothing persists).
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

// loadStore reads the store; anything unreadable yields an empty store.
func loadStore() storeSchema {
	path := StoreFile()
	if path == "" {
		return storeSchema{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return storeSchema{}
	}
	var on storeSchema
	if json.Unmarshal(data, &on) != nil {
		return storeSchema{}
	}
	return on
}

// writeStore persists the store. Errors are swallowed like the layout
// store's: not persisting a preference must never disrupt the session.
func writeStore(s storeSchema) {
	path := StoreFile()
	if path == "" {
		return
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, data, 0o644)
}

// LastSpec returns the spec the dialog opens with: the last one used when the
// store holds a *still valid* one, the stock default otherwise. Validating on
// read is what keeps a hand-edited store — or one written by an older IKE
// whose DSL this build rejects — from putting an unusable spec into the
// dialog.
func LastSpec() Spec {
	s := loadStore()
	if s.Last == nil || s.Last.Validate() != nil {
		return Default(Formats()[0])
	}
	return s.Last.Normalized()
}

// SaveLast remembers spec as the next dialog's starting point.
func SaveLast(spec Spec) {
	if spec.Validate() != nil {
		return
	}
	s := loadStore()
	spec = spec.Normalized()
	s.Last = &spec
	writeStore(s)
}

// Template is one named, format-free spec body. Built-ins ship with IKE and
// cannot be changed; user templates persist in the store.
type Template struct {
	Name    string
	DSL     string
	BuiltIn bool
}

// Templates lists what the dialog's template picker offers: the built-ins in
// their curated order, then the user's templates sorted by name.
func Templates() []Template {
	out := append([]Template(nil), builtinTemplates...)
	user := loadStore().Templates
	names := make([]string, 0, len(user))
	for n := range user {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		out = append(out, Template{Name: n, DSL: user[n]})
	}
	return out
}

// SaveTemplate stores dsl under name. The name must be non-empty and must not
// shadow a built-in; the body must parse — a template that cannot load back
// into the editor is not worth keeping.
func SaveTemplate(name, dsl string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("a template name is required")
	}
	for _, b := range builtinTemplates {
		if strings.EqualFold(b.Name, name) {
			return fmt.Errorf("%q is a built-in template — pick another name", b.Name)
		}
	}
	if _, err := ParseDSL(dsl); err != nil {
		return fmt.Errorf("the spec must be valid to save it: %w", err)
	}
	s := loadStore()
	if s.Templates == nil {
		s.Templates = map[string]string{}
	}
	s.Templates[name] = dsl
	writeStore(s)
	return nil
}

// DeleteTemplate removes a user template; built-ins and unknown names are
// no-ops.
func DeleteTemplate(name string) {
	s := loadStore()
	if _, ok := s.Templates[name]; !ok {
		return
	}
	delete(s.Templates, name)
	writeStore(s)
}
