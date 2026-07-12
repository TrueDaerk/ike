package config

import "ike/internal/largefile"

// defaults.go constructs the lowest-precedence layer in code, so IKE works with
// zero config files present. Slot maps are initialised non-nil and empty so
// higher layers (and extensions) merge into them by key rather than replacing a
// nil map wholesale.

// defaults returns a freshly allocated default Config. It is a function, not a
// package var, so callers can never mutate a shared baseline.
func defaults() *Config {
	return &Config{
		Editor: Editor{
			AutoSave:               "focus",
			TabWidth:               4,
			UseSpaces:              true,
			LineNumbers:            true,
			RelativeLineNumbers:    false,
			Wrap:                   false,
			ScrollOff:              3,
			AutoIndent:             true,
			AutoClosePairs:         true,
			TrimTrailingWhitespace: true,
			InsertFinalNewline:     true,
			Editorconfig:           true,
			ShowWhitespace:         "none",
			IndentGuides:           false,
			Rulers:                 []int{},
			StickyScroll:           true,
			StickyScrollDepth:      4,
			Tabs:                   Tabs{AlwaysShow: false},
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
			Enabled:    true,
			InlayHints: true,
			LogLevel:   "warn",
			Servers:    map[string]map[string]any{},
		},
		Theme: Theme{
			Name: "default",
			Dark: true,
		},
		Plugins: map[string]map[string]any{},
		Project: Project{
			History:     []ProjectHistoryEntry{},
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
		Files: Files{
			Watch:          true,
			AutoReload:     "clean",
			LargeFileKB:    largefile.DefaultMaxKB,
			LargeFileLines: largefile.DefaultMaxLines,
			PersistentUndo: true,
		},
		UI: UI{
			MenuBar: true,
		},
		Backup: Backup{
			Enable:     true,
			DebounceMs: 2000,
			MaxAgeDays: 7,
		},
		Lang: map[string]map[string]string{},
		Todo: Todo{
			Patterns: []string{"TODO", "FIXME", "HACK", "XXX"},
		},
	}
}
