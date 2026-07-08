package config

// defaults.go constructs the lowest-precedence layer in code, so IKE works with
// zero config files present. Slot maps are initialised non-nil and empty so
// higher layers (and extensions) merge into them by key rather than replacing a
// nil map wholesale.

// defaults returns a freshly allocated default Config. It is a function, not a
// package var, so callers can never mutate a shared baseline.
func defaults() *Config {
	return &Config{
		Editor: Editor{
			TabWidth:               4,
			UseSpaces:              true,
			LineNumbers:            true,
			RelativeLineNumbers:    false,
			Wrap:                   false,
			ScrollOff:              3,
			AutoIndent:             true,
			TrimTrailingWhitespace: true,
			InsertFinalNewline:     true,
			ShowWhitespace:         false,
		},
		Explorer: Explorer{
			ShowHidden: false,
			GitStatus:  true,
			TreeIndent: 2,
			Sort:       "name",
			Colors:     map[string]string{},
		},
		Keymap: Keymap{
			Preset:   "jetbrains",
			Bindings: map[string]string{},
		},
		LSP: LSP{
			Enabled:  true,
			LogLevel: "warn",
			Servers:  map[string]map[string]any{},
		},
		Theme: Theme{
			Name: "default",
			Dark: true,
		},
		Project: Project{
			History:     []string{},
			MaxHistory:  20,
			RestoreLast: false,
		},
		Palette: Palette{
			MaxResults:  12,
			DefaultMode: ":",
			OffContext:  "rank",
			ToggleKey:   "ctrl+p",
		},
		Notifications: Notifications{
			TimeoutSeconds: 4,
			MinSeverity:    "info",
		},
	}
}
