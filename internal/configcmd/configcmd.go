// Package configcmd holds the bubbletea commands around internal/config: the
// reload seam and the write-back-then-reload helpers the settings UI uses.
// They lived in config's watch.go until #2939 moved them here, so config
// stays free of bubbletea and the complexity-only build can link it without
// the UI runtime. Actual file-system watching (debounced fsnotify) is left to
// the owning roadmap; this provides the reload seam so callers can wire a
// trigger — a key binding, a SIGHUP, or a watcher — without new types.
package configcmd

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
)

// Reload returns a tea.Cmd that re-runs config.Load(opts) and delivers the
// result as a config.ConfigReloadedMsg. It does not mutate the global
// config.Get() state itself — the root model decides when to commit via Set —
// keeping reload explicit and testable.
func Reload(opts config.Options) tea.Cmd {
	return func() tea.Msg {
		c, diags := config.Load(opts)
		return config.ConfigReloadedMsg{Config: c, Diags: diags}
	}
}

// WriteAndReload persists key=value to scope's layer (config.WriteKey) and
// then runs the normal reload pipeline, so a settings-UI change applies
// through exactly the flow a manual file edit takes. A write failure surfaces
// as a Diagnostic on the reloaded message rather than an error — the UI shows
// it, the app keeps running on the prior values.
func WriteAndReload(opts config.Options, scope config.Scope, key string, value any) tea.Cmd {
	return applyAndReload(opts, key, func() error { return config.WriteKey(opts, scope, key, value) })
}

// RemoveAndReload removes key from scope's layer ('reset to default') and
// reloads, mirroring WriteAndReload.
func RemoveAndReload(opts config.Options, scope config.Scope, key string) tea.Cmd {
	return applyAndReload(opts, key, func() error { return config.RemoveKey(opts, scope, key) })
}

// ApplyAndReload persists a whole batch of mutations and reloads **once**
// (#1296): the settings panel stages edits and applies them together, so the
// app must not re-theme and rebuild its keymaps once per changed key. Every
// failure surfaces as a Diagnostic on the reloaded message, and a failing
// mutation never stops the rest — the batch is a best-effort write of
// independent keys, exactly like the same edits made by hand.
func ApplyAndReload(opts config.Options, muts []config.Mutation) tea.Cmd {
	return func() tea.Msg {
		var diags []config.Diagnostic
		for _, m := range muts {
			var err error
			if m.Remove {
				err = config.RemoveKey(opts, m.Scope, m.Key)
			} else {
				err = config.WriteKey(opts, m.Scope, m.Key, m.Value)
			}
			if err != nil {
				diags = append(diags, config.Diagnostic{Field: m.Key, Message: err.Error()})
			}
		}
		c, loadDiags := config.Load(opts)
		return config.ConfigReloadedMsg{Config: c, Diags: append(diags, loadDiags...)}
	}
}

// applyAndReload runs one write-back mutation and always follows with Load.
func applyAndReload(opts config.Options, key string, fn func() error) tea.Cmd {
	return func() tea.Msg {
		werr := fn()
		c, diags := config.Load(opts)
		if werr != nil {
			diags = append(diags, config.Diagnostic{Field: key, Message: werr.Error()})
		}
		return config.ConfigReloadedMsg{Config: c, Diags: diags}
	}
}
