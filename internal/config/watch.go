package config

import tea "charm.land/bubbletea/v2"

// watch.go is the only place config touches bubbletea. It defines the message
// the root model routes when configuration changes, and a command that re-runs
// the Load pipeline. Actual file-system watching (debounced fsnotify) is left to
// the owning roadmap; this provides the reload seam so callers can wire a
// trigger — a key binding, a SIGHUP, or a future watcher — without new types.

// ConfigReloadedMsg carries a freshly loaded configuration into the Update loop.
// The root model installs Config via Set and re-themes / rebuilds keymaps from
// it. Diags lets the UI surface non-fatal problems from the reload.
type ConfigReloadedMsg struct {
	Config *Config
	Diags  []Diagnostic
}

// Reload returns a tea.Cmd that re-runs Load(opts) and delivers the result as a
// ConfigReloadedMsg. It does not mutate the global Get() state itself — the root
// model decides when to commit via Set — keeping reload explicit and testable.
func Reload(opts Options) tea.Cmd {
	return func() tea.Msg {
		c, diags := Load(opts)
		return ConfigReloadedMsg{Config: c, Diags: diags}
	}
}

// WriteAndReload persists key=value to scope's layer (WriteKey) and then runs
// the normal reload pipeline, so a settings-UI change applies through exactly
// the flow a manual file edit takes. A write failure surfaces as a Diagnostic
// on the reloaded message rather than an error — the UI shows it, the app
// keeps running on the prior values.
func WriteAndReload(opts Options, scope Scope, key string, value any) tea.Cmd {
	return applyAndReload(opts, key, func() error { return WriteKey(opts, scope, key, value) })
}

// RemoveAndReload removes key from scope's layer ('reset to default') and
// reloads, mirroring WriteAndReload.
func RemoveAndReload(opts Options, scope Scope, key string) tea.Cmd {
	return applyAndReload(opts, key, func() error { return RemoveKey(opts, scope, key) })
}

// applyAndReload runs one write-back mutation and always follows with Load.
func applyAndReload(opts Options, key string, fn func() error) tea.Cmd {
	return func() tea.Msg {
		werr := fn()
		c, diags := Load(opts)
		if werr != nil {
			diags = append(diags, Diagnostic{Field: key, Message: werr.Error()})
		}
		return ConfigReloadedMsg{Config: c, Diags: diags}
	}
}
