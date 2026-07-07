package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// write.go exposes the typed setter seam and the disk write-back layer
// (Roadmap 0160, #89). Setters mutate an in-memory *Config and define the
// bounded semantics; WriteKey/RemoveKey persist a single dotted key to the
// user or project settings file via a real TOML round-trip, so unknown keys
// survive untouched (comments do not — the TOML library re-emits the document;
// the spec tolerates that). After a write the caller re-runs the normal reload
// path (WriteAndReload in watch.go) so the change applies exactly like a
// manual file edit.

// Scope names the config layer a key persists to.
type Scope int

const (
	// UserScope writes to the user settings file (~/.ike/settings.toml or
	// $IKE_CONFIG_DIR/settings.toml).
	UserScope Scope = iota
	// ProjectScope writes to the project settings file (<root>/.ike/settings.toml).
	ProjectScope
)

// DefaultScope returns the layer a dotted key conventionally persists to:
// project-bound keys (recent projects, per-language servers and toolchains) go
// to the project file, everything else — theme, keymap, editor look & feel —
// to the user file. The settings UI may still pass an explicit scope; this is
// the default it offers.
func DefaultScope(key string) Scope {
	switch {
	case strings.HasPrefix(key, "project."),
		strings.HasPrefix(key, "lsp.servers."),
		strings.HasPrefix(key, "toolchain."):
		return ProjectScope
	}
	return UserScope
}

// layerPath resolves the file a scope writes to. An empty result means the
// layer is not addressable (no home dir / no project root).
func layerPath(opts Options, scope Scope) string {
	if scope == ProjectScope {
		if opts.ProjectRoot == "" {
			return ""
		}
		return filepath.Join(opts.ProjectRoot, dotDir, fileName)
	}
	return opts.UserPath
}

// WriteKey sets the dotted key (e.g. "editor.tab_width") to value in scope's
// settings file, creating the file (and its directory) when missing. Only the
// touched key changes; every other key in the file round-trips. A file that no
// longer parses is left untouched and the parse error returned — write-back
// never destroys a broken-but-recoverable config.
func WriteKey(opts Options, scope Scope, key string, value any) error {
	return rewrite(opts, scope, key, func(parent map[string]any, leaf string) {
		parent[leaf] = value
	})
}

// RemoveKey deletes the dotted key from scope's settings file — the 'reset to
// default' operation: the value falls back through defaults < user < project.
// Emptied parent tables are pruned. Removing from a missing file is a no-op.
func RemoveKey(opts Options, scope Scope, key string) error {
	path := layerPath(opts, scope)
	if path == "" {
		return fmt.Errorf("config: no %s layer to remove %q from", scopeName(scope), key)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	return rewrite(opts, scope, key, func(parent map[string]any, leaf string) {
		delete(parent, leaf)
	})
}

// rewrite loads scope's file as a raw table, lets mutate touch the key's
// parent table, prunes empties and writes the document back.
func rewrite(opts Options, scope Scope, key string, mutate func(parent map[string]any, leaf string)) error {
	path := layerPath(opts, scope)
	if path == "" {
		return fmt.Errorf("config: no %s layer to write %q to", scopeName(scope), key)
	}
	raw, err := decodeFile(path)
	if err != nil {
		return err
	}
	if raw == nil {
		raw = map[string]any{}
	}

	parts := strings.Split(key, ".")
	parent := raw
	for _, p := range parts[:len(parts)-1] {
		child, ok := parent[p].(map[string]any)
		if !ok {
			child = map[string]any{}
			parent[p] = child
		}
		parent = child
	}
	mutate(parent, parts[len(parts)-1])
	prune(raw)

	data, err := toml.Marshal(raw)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// prune drops empty sub-tables so a removed last key does not leave a dangling
// section header behind.
func prune(table map[string]any) {
	for k, v := range table {
		if child, ok := v.(map[string]any); ok {
			prune(child)
			if len(child) == 0 {
				delete(table, k)
			}
		}
	}
}

// scopeName renders a Scope for error messages.
func scopeName(s Scope) string {
	if s == ProjectScope {
		return "project"
	}
	return "user"
}

// PushHistory records root as the most-recent project: it is moved (or added) to
// the front, de-duplicated, and the list is trimmed to MaxHistory. It returns the
// modified config for chaining.
func (c *Config) PushHistory(root string) *Config {
	if root == "" {
		return c
	}
	out := []string{root}
	for _, h := range c.Project.History {
		if h != root {
			out = append(out, h)
		}
	}
	if n := c.Project.MaxHistory; n >= 0 && len(out) > n {
		out = out[:n]
	}
	c.Project.History = out
	return c
}
