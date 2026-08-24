package config

import (
	"os/exec"
	"sync"

	"ike/internal/changefeed"
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
		Name:    "lazygit",
		Command: "lazygit",
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
			TextWidth:              80,
			ClipboardSync:          true,
			ClipboardHistorySize:   20, // register.DefaultHistoryCap, JetBrains' ring size
			AutoIndent:             true,
			AutoClosePairs:         true,
			TrimTrailingWhitespace: true,
			InsertFinalNewline:     true,
			FormatOnSave:           false,
			OrganizeImportsOnSave:  false,
			Editorconfig:           true,
			ShowWhitespace:         "none",
			IndentGuides:           false,
			RainbowIndentGuides:    true,
			Rulers:                 []int{},
			StickyScroll:           true,
			StickyScrollDepth:      4,
			SmartPaste:             true,
			MarkdownRendering:      true,
			CSVRendering:           true,
			LogRendering:           true,
			FollowPollMs:           500,
			TimestampDecoding:      true,
			UnicodeEscapeDecoding:  true,
			EntityDecoding:         true,
			Base64Decoding:         true,
			CronHints:              true,
			PemSummary:             true,
			ByteSizeHints:          true,
			DurationHints:          true,
			DigitGrouping:          true,
			RadixHints:             true,
			NumberHintUnits:        []string{}, // no mapping: the key heuristics decide (#1685)
			PermissionHints:        true,
			CIDRHints:              true,
			IDNHints:               true,
			TogglePairs:            []string{}, // additions only; the built-in pairs live in the editor (#1658)
			ConcealInclude:         []string{}, // no file filter: the per-family toggles alone decide (#1704)
			ConcealExclude:         []string{},
			ConcealFileRules:       []string{},
			SecretMasking:          true,
			SecretMaskingKeys:      []string{}, // additions only; the built-in key patterns live in internal/secret (#1712)
			Hyperlinks:             true,
			ColorPreview:           true,
			IDColors:               true,
			IDColorMinLength:       7,
			DiffWordHighlight:      true,
			RainbowBrackets:        true,
			SearchIgnoreCase:       false,
			Breadcrumbs:            true,
			PostfixCompletion:      true,
			Tabs:                   Tabs{AlwaysShow: false, Limit: 5},
			Typing:                 Typing{SpaceAfterPunctuation: true},
			Marks: Marks{
				LSPErrors:   true,
				LSPWarnings: true,
				LSPInfo:     true,
				LSPHints:    true,
				GitAdded:    true,
				GitChanged:  true,
				GitDeleted:  true,
				Inheritance: true,
			},
		},
		Explorer: Explorer{
			ShowHidden: false,
			GitStatus:  true,
			TreeIndent: 2,
			Sort:       "name",
			AutoReveal: false,
			Icons:      false,
			Exclude:    []string{".git", ".idea", ".DS_Store"},
			Colors:     map[string]string{},
		},
		Keymap: Keymap{
			Preset:          "jetbrains",
			Bindings:        map[string]string{},
			WhichKey:        true,
			WhichKeyDelayMs: 300,
		},
		LSP: LSP{
			Enabled:        true,
			InlayHints:     false,
			SignatureAuto:  true,
			CompletionAuto: true,
			CodeLens:       true,
			Folding:        true,
			SemanticTokens: true,
			SelectionRange: true,
			WillRename:     true,
			LogLevel:       "warn",
			Servers:        map[string]map[string]any{},
			// Default ignore rules (#1260): intelephense's P1006 TypeError
			// cannot infer types written through by-reference parameters
			// (&$param) and floods by-ref-heavy PHP with bogus
			// "Expected type '...'. Found 'null'." / "... Found 'unset'."
			// errors (bmewburn/vscode-intelephense#3504, open as of 1.18.5).
			// Only the null/unset variants are suppressed — other P1006
			// findings (real type mismatches) still surface. A user-set
			// lsp.diagnostics_ignore list replaces these defaults wholesale,
			// like any other list setting.
			DiagnosticsIgnore: []string{
				"source=intelephense code=P1006 msg=*Found 'null'*",
				"source=intelephense code=P1006 msg=*Found 'unset'*",
			},
		},
		Theme: Theme{
			Name: "default",
			Auto: false,
			// The auto pair (#1480): matches the default dark scheme with the
			// JetBrains-flavoured light one.
			Light: "intellij-light",
			Dark:  "default",
			// A slot map like explorer.colors / keymap.bindings: seeded
			// non-nil so layers merge into it key by key (#1318).
			Captures: map[string]string{},
		},
		Plugins: map[string]map[string]any{},
		Project: Project{
			History:     []ProjectHistoryEntry{},
			MaxHistory:  20,
			RestoreLast: false,
			// The visible default mirrors JetBrains' ~/IdeaProjects; it is
			// only materialised when a feature needs it (project.clone).
			Directory: "~/IkeProjects",
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
			// Enough to review a long agent run without unbounded growth
			// (#2000); past it the oldest rows drop out.
			ChangeFeedLimit: changefeed.DefaultLimit,
			Associations:    map[string]string{},
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
		History: History{
			// The Timeline shows both histories by default (#1916) — the
			// whole point is having one list for committed and uncommitted
			// changes.
			TimelineSource: "both",
		},
		Terminal: Terminal{
			Autosuggest:     true,
			ScrollbackLines: 10000,
		},
		Lang:  map[string]map[string]string{},
		Tools: Tools{Custom: defaultTools()},
		Todo: Todo{
			Patterns: []string{"TODO", "FIXME", "HACK", "XXX"},
		},
		Run: Run{
			Placement:    "bottom", // the Run tool docks at the bottom edge (#1905)
			VSCodeLaunch: true,     // .vscode/launch.json entries join the picker (#1914)
		},
		Tests: Tests{
			ResultsWindow: true, // parsed test runs open the Test Results tool (#1911)
			AutoOpen:      true,
		},
		Scratch: Scratch{
			// The explorer's Scratches section (#1963) shows by default: it
			// costs no editor rows, only the bottom of the explorer column.
			// 5 rows list five scratches; the divider drag resizes at runtime.
			// The legacy panel/panel_height stay zero — a non-zero
			// panel_height can only come from an old config file, which
			// Validate migrates onto section_height.
			Section:       true,
			SectionHeight: 5,
			Sort:          "name",
		},
		Debug: Debug{
			InlineValues: true, // paused locals annotate their lines (#1914)
			PHP: DebugPHP{
				Port: 9003, // Xdebug's default DBGp port
			},
		},
		Perf: Perf{
			// One HUD refresh per second: fast enough to watch a spike
			// build, slow enough that the HUD's own wake is a rounding
			// error in the rate it reports (#1999).
			HUDIntervalMs:     1000,
			HUDHistorySeconds: 60,
		},
		Remote: Remote{
			// 64 MiB covers logs, configs and most artifacts without letting
			// an accidental open of a dump stall a slow link (#1997).
			MaxFetchMB: 64,
		},
		Screenshot: Screenshot{
			// Empty resolves to ~/.ike/screenshots (internal/app owns the
			// fallback): shots sit next to the user config rather than in the
			// project, so nothing has to be gitignored (#2001).
			Directory: "",
		},
	}
}
