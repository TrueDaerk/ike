package config

import (
	"os/exec"
	"sync"

	"ike/internal/largefile"
)

// defaults.go constructs the lowest-precedence layer in code, so IKE works with
// zero config files present. Slot maps are initialised non-nil and empty so
// higher layers (and extensions) merge into them by key rather than replacing a
// nil map wholesale.

// lookPath resolves a binary on PATH; a seam for tests.
var lookPath = exec.LookPath

// lazygitProbe caches one PATH probe per process — defaults() runs on every
// config Get before the first Set, and PATH does not change under IKE.
var lazygitProbe struct {
	once sync.Once
	ok   bool
}

func lazygitOnPath() bool {
	lazygitProbe.once.Do(func() {
		_, err := lookPath("lazygit")
		lazygitProbe.ok = err == nil
	})
	return lazygitProbe.ok
}

// resetLazygitProbe re-arms the cached PATH probe; intended for tests only.
func resetLazygitProbe() {
	lazygitProbe.once = sync.Once{}
	lazygitProbe.ok = false
}

// defaultTools returns the default [[tools.custom]] entries (#750): lazygit
// ships preconfigured as the example git-workflow tool pane whenever it is on
// PATH — the native VCS tool window is file-context only, workflow is
// delegated to tool panes (#741). When lazygit is missing there is no hard
// dependency: the entry is omitted and the tools.setup onboarding offers it
// as an install suggestion instead (internal/toolcatalog). A user-defined
// tools.custom list overrides this default wholesale, like any other setting.
func defaultTools() []ToolEntry {
	if !lazygitOnPath() {
		return nil
	}
	return []ToolEntry{{
		Name:      "lazygit",
		Command:   "lazygit",
		Placement: "bottom",
	}}
}

// defaults returns a freshly allocated default Config. It is a function, not a
// package var, so callers can never mutate a shared baseline.
func defaults() *Config {
	return &Config{
		Editor: Editor{
			AutoSave:               "focus",
			AutoSaveIdleMs:         2000,
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
			MarkdownRendering:      true,
			ColorPreview:           true,
			RainbowBrackets:        true,
			SearchIgnoreCase:       false,
			Tabs:                   Tabs{AlwaysShow: false, Limit: 5},
		},
		Explorer: Explorer{
			ShowHidden: false,
			GitStatus:  true,
			TreeIndent: 2,
			Sort:       "name",
			AutoReveal: false,
			Icons:      false,
			Colors:     map[string]string{},
		},
		Keymap: Keymap{
			Preset:   "jetbrains",
			Bindings: map[string]string{},
		},
		LSP: LSP{
			Enabled:        true,
			InlayHints:     false,
			SignatureAuto:  true,
			CompletionAuto: true,
			LogLevel:       "warn",
			Servers:        map[string]map[string]any{},
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
			// No default toggle chord: the palette opens via esc-esc, "@" and
			// searchEverywhere; ctrl+p belongs to lsp.parameterInfo (#523).
			ToggleKey: "",
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
			MenuBar:       true,
			PopupMaxWidth: 110,
		},
		Backup: Backup{
			Enable:     true,
			DebounceMs: 2000,
			MaxAgeDays: 7,
		},
		Terminal: Terminal{
			Autosuggest: true,
		},
		Lang:  map[string]map[string]string{},
		Tools: Tools{Custom: defaultTools()},
		Todo: Todo{
			Patterns: []string{"TODO", "FIXME", "HACK", "XXX"},
		},
		Run: Run{
			Placement: "in_pane",
		},
		Debug: Debug{
			PHP: DebugPHP{
				Port: 9003, // Xdebug's default DBGp port
			},
		},
	}
}
