// Package config is IKE's single typed configuration system. Settings live in
// TOML files that merge across three layers — built-in defaults < user-global <
// project — and every subsystem reads strongly typed structs through Load/Get.
//
// The package is leaf-level: it depends on nothing in IKE (only the TOML library,
// isolated in load.go, and bubbletea for the reload message in watch.go), so any
// package can import it without cycles. internal/host backs host.API on top of
// it; plugins read config as plain data and never touch this package directly.
package config

import (
	"fmt"
	"strings"
	"sync"
)

// Load resolves the merged, validated configuration for opts. The pipeline is:
// defaults (plus extension defaults) → merge user/project override layers →
// decode onto defaults → clamp-and-warn validate. It never returns an error: a
// file that fails to parse is dropped with a diagnostic and the lower layers
// still apply, so a broken config can never prevent IKE from starting.
func Load(opts Options) (*Config, []Diagnostic) {
	c := defaults()
	applyExtensionDefaults(c)

	var diags []Diagnostic
	merged := map[string]any{}
	for _, path := range opts.layerPaths() {
		raw, err := decodeFile(path)
		if err != nil {
			diags = append(diags, Diagnostic{Source: path, Field: "(file)", Message: err.Error()})
			continue
		}
		if raw != nil {
			mergeMaps(merged, raw)
		}
	}

	if err := decodeOnto(merged, c); err != nil {
		// A merge that decodes back into the struct should not fail; if it does,
		// keep the defaults and report it rather than crashing.
		diags = append(diags, Diagnostic{Field: "(merge)", Message: err.Error()})
		c = defaults()
		applyExtensionDefaults(c)
	}

	diags = append(diags, validate(c)...)
	return c, diags
}

var (
	mu     sync.RWMutex
	loaded *Config
)

// Set installs c as the process-wide configuration returned by Get. The root
// model calls it once after Load (and again on reload). A nil c is ignored.
func Set(c *Config) {
	if c == nil {
		return
	}
	mu.Lock()
	loaded = c
	mu.Unlock()
}

// Get returns the process-wide configuration. Before the first Set it returns
// the pure defaults, so a caller that reads config early still gets valid values.
func Get() *Config {
	mu.RLock()
	c := loaded
	mu.RUnlock()
	if c != nil {
		return c
	}
	d := defaults()
	applyExtensionDefaults(d)
	return d
}

// Flat renders the scalar configuration as dotted string keys. It backs the
// read-only key/value view that internal/host exposes to plugins, keeping the
// typed schema the single source of truth for those keys. Slot-map entries
// (explorer.colors.*, keymap.bindings.*, lsp.servers.*) are included so a plugin
// can read whatever downstream roadmaps register.
func (c *Config) Flat() map[string]string {
	m := map[string]string{}
	put := func(k string, v any) { m[k] = fmt.Sprint(v) }

	put("editor.auto_save", c.Editor.AutoSave)
	put("editor.tab_width", c.Editor.TabWidth)
	put("editor.use_spaces", c.Editor.UseSpaces)
	put("editor.line_numbers", c.Editor.LineNumbers)
	put("editor.relative_line_numbers", c.Editor.RelativeLineNumbers)
	put("editor.wrap", c.Editor.Wrap)
	put("editor.scroll_off", c.Editor.ScrollOff)
	put("editor.auto_indent", c.Editor.AutoIndent)
	put("editor.trim_trailing_whitespace", c.Editor.TrimTrailingWhitespace)
	put("editor.insert_final_newline", c.Editor.InsertFinalNewline)
	put("editor.show_whitespace", c.Editor.ShowWhitespace)

	put("explorer.show_hidden", c.Explorer.ShowHidden)
	put("explorer.git_status", c.Explorer.GitStatus)
	put("explorer.tree_indent", c.Explorer.TreeIndent)
	put("explorer.sort", c.Explorer.Sort)
	for k, v := range c.Explorer.Colors {
		put("explorer.colors."+k, v)
	}

	put("keymap.preset", c.Keymap.Preset)
	for k, v := range c.Keymap.Bindings {
		put("keymap.bindings."+k, v)
	}

	put("lsp.enabled", c.LSP.Enabled)
	put("lsp.log_level", c.LSP.LogLevel)
	for srv, kv := range c.LSP.Servers {
		for k, v := range kv {
			put("lsp.servers."+srv+"."+k, v)
		}
	}

	put("theme.name", c.Theme.Name)
	put("theme.dark", c.Theme.Dark)

	put("project.max_history", c.Project.MaxHistory)
	put("project.restore_last", c.Project.RestoreLast)
	put("project.history", strings.Join(c.Project.History, ","))

	put("notifications.timeout_seconds", c.Notifications.TimeoutSeconds)
	put("notifications.min_severity", c.Notifications.MinSeverity)

	put("files.watch", c.Files.Watch)
	put("files.auto_reload", c.Files.AutoReload)

	put("backup.enable", c.Backup.Enable)
	put("backup.debounce_ms", c.Backup.DebounceMs)
	put("backup.max_age_days", c.Backup.MaxAgeDays)

	put("ui.menu_bar", c.UI.MenuBar)

	for id, kv := range c.Lang {
		for k, v := range kv {
			put("lang."+id+"."+k, v)
		}
	}

	put("palette.max_results", c.Palette.MaxResults)
	put("palette.default_mode", c.Palette.DefaultMode)
	put("palette.off_context", c.Palette.OffContext)
	put("palette.toggle_key", c.Palette.ToggleKey)

	return m
}
