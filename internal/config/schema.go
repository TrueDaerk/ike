package config

// schema.go defines the typed configuration sections. Every section is a plain
// struct with TOML field tags; the slot maps (Explorer.Colors, Keymap.Bindings,
// LSP.Servers) are intentionally empty here — downstream roadmaps fill their
// concrete entries through extend.go, never by editing these structs.

// Config is the root configuration document. It is the only type the rest of
// IKE reads; no TOML types leak past this package.
type Config struct {
	Editor        Editor        `toml:"editor"`
	Explorer      Explorer      `toml:"explorer"`
	Keymap        Keymap        `toml:"keymap"`
	LSP           LSP           `toml:"lsp"`
	Theme         Theme         `toml:"theme"`
	Project       Project       `toml:"project"`
	Palette       Palette       `toml:"palette"`
	Notifications Notifications `toml:"notifications"`
	Files         Files         `toml:"files"`
	UI            UI            `toml:"ui"`
	Backup        Backup        `toml:"backup"`
	// Lang holds per-language settings as a free-form slot (Roadmap 0160,
	// mirrors LSP.Servers): [lang.python] interpreter = "/path/to/python".
	// The toolchain settings page writes it; lang.Interpreter resolution and
	// the LSP spec overlay read it.
	Lang map[string]map[string]string `toml:"lang"`
	// Terminal holds integrated-terminal settings (Roadmap 0170). Shell
	// overrides $SHELL for spawned sessions; empty follows the environment.
	Terminal Terminal `toml:"terminal"`
	// Plugins holds per-plugin toggles as a free-form slot (Roadmap 0180,
	// #133): [plugins.example] enabled = false. The plugin manager page
	// writes it; applyPluginConfig and the LSP spec resolution read it.
	Plugins map[string]map[string]any `toml:"plugins"`
	// Marketplace holds plugin-marketplace settings (Roadmap 0310).
	Marketplace Marketplace `toml:"marketplace"`
	// Todo holds the TODO/FIXME index settings (#61).
	Todo Todo `toml:"todo"`
}

// Todo holds the comment-tag index settings (#61). Patterns is the list of tag
// words the project scan matches as whole words, case-insensitively (e.g.
// ["TODO", "FIXME", "HACK", "XXX"]); entries are literals, not regexes.
type Todo struct {
	Patterns []string `toml:"patterns"`
}

// Marketplace holds plugin-marketplace settings (Roadmap 0310, #444).
// CatalogURL is the HTTPS location of the catalog index.json; empty falls back
// to the built-in default (which may itself be empty — marketplace disabled).
type Marketplace struct {
	CatalogURL string `toml:"catalog_url"`
}

// Terminal holds integrated-terminal behaviour (Roadmap 0170).
type Terminal struct {
	Shell string `toml:"shell"`
}

// UI holds chrome toggles (Roadmap 0160). MenuBar shows the top menu row.
type UI struct {
	MenuBar bool `toml:"menu_bar"`
}

// Backup holds crash-recovery snapshot behaviour (Roadmap 0210). Enable turns
// the subsystem on; disabling it also purges existing snapshots (they contain
// file contents). DebounceMs is the quiet interval after the last edit before a
// dirty buffer is snapshotted. MaxAgeDays bounds snapshot age: older leftovers
// are pruned at startup, after the restore prompt has run.
type Backup struct {
	Enable     bool `toml:"enable"`
	DebounceMs int  `toml:"debounce_ms"`
	MaxAgeDays int  `toml:"max_age_days"`
}

// Files holds external-file-change behaviour (Roadmap 0140). Watch enables the
// fsnotify project watcher; AutoReload ("clean" or "never") controls whether a
// clean editor buffer reloads in place when its file changes on disk.
// LargeFileKB and LargeFileLines are the large-file thresholds (#149): a file
// crossing either at load/reload is flagged and code insight (highlighting,
// LSP, watcher content hashing) degrades; 0 disables that guard.
// PersistentUndo (#148) keeps undo history across restarts (vim's undofile):
// stacks are written to the state store on save/close and adopted on open
// while the file content is unchanged.
type Files struct {
	Watch          bool   `toml:"watch"`
	AutoReload     string `toml:"auto_reload"`
	LargeFileKB    int    `toml:"large_file_kb"`
	LargeFileLines int    `toml:"large_file_lines"`
	PersistentUndo bool   `toml:"persistent_undo"`
}

// Editor holds text-editing behaviour (Roadmap 0060 consumes most of it).
// AutoSave ("off" or "focus") controls whether a dirty buffer saves itself when
// focus leaves its pane or its document is replaced (#174); an "idle" mode is
// reserved for later (#54).
type Editor struct {
	AutoSave               string `toml:"auto_save"`
	TabWidth               int    `toml:"tab_width"`
	UseSpaces              bool   `toml:"use_spaces"`
	LineNumbers            bool   `toml:"line_numbers"`
	RelativeLineNumbers    bool   `toml:"relative_line_numbers"`
	Wrap                   bool   `toml:"wrap"`
	ScrollOff              int    `toml:"scroll_off"`
	AutoIndent             bool   `toml:"auto_indent"`
	AutoClosePairs         bool   `toml:"auto_close_pairs"`
	TrimTrailingWhitespace bool   `toml:"trim_trailing_whitespace"`
	InsertFinalNewline     bool   `toml:"insert_final_newline"`
	// Editorconfig honours .editorconfig files (#63): their matching sections
	// override the [editor] indent/trim/final-newline/EOL/charset values per
	// buffer. On by default; false ignores them entirely.
	Editorconfig bool `toml:"editorconfig"`
	// ShowWhitespace renders whitespace visibly (#64): "none", "trailing"
	// (only line-end runs) or "all". IndentGuides draws vertical lines at each
	// indent stop; Rulers tints the given display columns (e.g. [80, 120]).
	ShowWhitespace string `toml:"show_whitespace"`
	IndentGuides   bool   `toml:"indent_guides"`
	Rulers         []int  `toml:"rulers"`
	// StickyScroll pins the enclosing declaration lines (function/class
	// headers) at the top of the editor while scrolling inside their body
	// (#168); StickyScrollDepth caps how many nested headers are pinned.
	StickyScroll      bool `toml:"sticky_scroll"`
	StickyScrollDepth int  `toml:"sticky_scroll_depth"`
	Tabs              Tabs `toml:"tabs"`
}

// Tabs holds editor-tab behaviour (Roadmap 0190). AlwaysShow renders the
// pane's tab bar even when it holds a single tab; by default the bar only
// appears with two or more tabs.
type Tabs struct {
	AlwaysShow bool `toml:"always_show"`
}

// Explorer holds file-tree behaviour. Colors is a per-filetype color-name slot
// filled by Roadmap 0050.
type Explorer struct {
	ShowHidden bool              `toml:"show_hidden"`
	GitStatus  bool              `toml:"git_status"`
	TreeIndent int               `toml:"tree_indent"`
	Sort       string            `toml:"sort"`
	Colors     map[string]string `toml:"colors"`
}

// Keymap selects a binding preset and carries per-action overrides. Bindings is
// a slot filled by Roadmap 0080; Leader is the leader-prefix key (0081/30,
// default "space").
type Keymap struct {
	Preset   string            `toml:"preset"`
	Leader   string            `toml:"leader"`
	Bindings map[string]string `toml:"bindings"`
}

// LSP holds language-server settings. Servers is a per-language slot filled by
// Roadmap 0100; its value type stays a free-form table so a server entry can
// carry arbitrary keys without a schema change here.
type LSP struct {
	Enabled     bool `toml:"enabled"`
	AutoInstall bool `toml:"auto_install"`
	// InlayHints toggles the inline parameter-name/type hints (#171).
	// Off by default (#523); parameter info is available on demand instead.
	InlayHints bool `toml:"inlay_hints"`
	// SignatureAuto gates the automatic signature-help popup on trigger
	// characters ("(", ","). The manual lsp.parameterInfo command works
	// regardless (#523).
	SignatureAuto bool `toml:"signature_auto"`
	// CompletionAuto gates the automatic completion popup while typing
	// identifier characters (#527). Server trigger characters ("." etc.) and
	// the manual ctrl+space request work regardless.
	CompletionAuto bool                      `toml:"completion_auto"`
	LogLevel       string                    `toml:"log_level"`
	Servers        map[string]map[string]any `toml:"servers"`
	// Onboarded records that the first-start server-install dialog (#301) has
	// had its say (answered or skipped); it is never shown again once set.
	Onboarded bool `toml:"onboarded"`
}

// Theme selects the active palette; its contents are owned by Roadmap 0110.
type Theme struct {
	Name string `toml:"name"`
	Dark bool   `toml:"dark"`
}

// Project holds recent-project history. History is a replace-by-default list
// (see merge.go) of [[project.history]] entries, most-recent-first; its content
// rules (upsert, dedupe, cap) and UX live in internal/project (Roadmap 0090).
type Project struct {
	History     []ProjectHistoryEntry `toml:"history"`
	MaxHistory  int                   `toml:"max_history"`
	RestoreLast bool                  `toml:"restore_last"`
}

// ProjectHistoryEntry is one recently opened project as persisted in
// [[project.history]]. Path is absolute and cleaned, Name is the display name
// (default: base dir name), LastOpened is RFC3339 and used for ordering.
// internal/project owns the semantics; this struct only fixes the TOML shape.
type ProjectHistoryEntry struct {
	Path       string `toml:"path"`
	Name       string `toml:"name"`
	LastOpened string `toml:"last_opened"`
}

// Notifications tunes the toast system (Roadmap 0130). TimeoutSeconds is the
// info/warn toast lifetime; MinSeverity ("info", "warn", "error") is the toast
// floor — notifications below it are recorded in the history but never toast.
type Notifications struct {
	TimeoutSeconds int    `toml:"timeout_seconds"`
	MinSeverity    string `toml:"min_severity"`
}

// Palette tunes the command palette overlay (Roadmap 0070). DefaultMode is the
// prefix used when the query has no recognised one (":" commands or "@" files).
// OffContext selects how command mode treats commands scoped to a different pane
// context: "rank" lists them last, "hide" omits them. ToggleKey is the default
// key that opens the palette (Roadmap 0080 owns the final keymap).
type Palette struct {
	MaxResults  int    `toml:"max_results"`
	DefaultMode string `toml:"default_mode"`
	OffContext  string `toml:"off_context"`
	ToggleKey   string `toml:"toggle_key"`
}
