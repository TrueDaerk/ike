package config

import tea "github.com/charmbracelet/bubbletea"

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
