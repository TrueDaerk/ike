package config

// watch.go defines the message the root model routes when configuration
// changes and the staged write-back mutation. The tea.Cmd constructors that
// produce the message (Reload, WriteAndReload, …) live in internal/configcmd
// (#2939), so config itself stays free of bubbletea and the complexity-only
// build can link it without the UI runtime.

// ConfigReloadedMsg carries a freshly loaded configuration into the Update loop.
// The root model installs Config via Set and re-themes / rebuilds keymaps from
// it. Diags lets the UI surface non-fatal problems from the reload.
type ConfigReloadedMsg struct {
	Config *Config
	Diags  []Diagnostic
}

// Mutation is one staged write-back edit for configcmd.ApplyAndReload: a
// value to write, or a key to remove when Remove is set.
type Mutation struct {
	Scope  Scope
	Key    string
	Value  any
	Remove bool
}
