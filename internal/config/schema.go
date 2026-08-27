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
	History       History       `toml:"history"`
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
	// Run holds run-configuration behaviour (0350, #576).
	Run Run `toml:"run"`
	// Tasks holds task-discovery and problem-matcher settings (#1915):
	// [[tasks.matcher]] entries define custom problem matchers a run
	// configuration can reference by name, next to the built-in ones.
	Tasks Tasks `toml:"tasks"`
	// Tests holds Test Results tool-window behaviour (#1911).
	Tests Tests `toml:"tests"`
	// Scratch holds Scratch Files tool-window behaviour (#1932).
	Scratch Scratch `toml:"scratch"`
	// Issues holds the forge Issues tool-window's defaults (#2090).
	Issues Issues `toml:"issues"`
	// Debug holds debugger behaviour (0360, #823).
	Debug Debug `toml:"debug"`
	// Tools holds user-defined TUI tool panes (#741).
	Tools Tools `toml:"tools"`
	// Elasticsearch holds the ES console's cluster endpoints (#1927).
	Elasticsearch Elasticsearch `toml:"elasticsearch"`
	// Snippets holds user live templates (#1152): [[snippets]] entries the
	// editor expands on Tab after the trigger word and offers in the
	// completion popup. Like every TOML list it replaces across layers — a
	// project [[snippets]] table hides the user-scope one — but the built-in
	// examples live in code (internal/snippets) and are only shadowed per
	// trigger+language, never wiped.
	Snippets []SnippetEntry `toml:"snippets"`
	// Format holds per-language formatter overrides as a free-form slot
	// (Roadmap 0470, #1402, mirrors LSP.Servers): [format.<languageID>] with
	// command, args, range_args, temp_file, enabled. The formatter registry's
	// config-override provider reads it; enabled = false switches a
	// language's external formatting off entirely.
	Format map[string]map[string]any `toml:"format"`
	// Perf holds the built-in performance HUD's behaviour (#1999).
	Perf Perf `toml:"perf"`
	// Remote holds the SFTP remote browser's behaviour (#1997).
	Remote Remote `toml:"remote"`
	// Screenshot holds the in-IDE PNG export's behaviour (#2001).
	Screenshot Screenshot `toml:"screenshot"`
	// Forge holds code-forge behaviour: how often IKE re-reads the forge in
	// the background (#2085) and how prominently each kind of forge event
	// announces itself (#2086).
	Forge Forge `toml:"forge"`
}

// Forge holds the code-forge settings (#2085, #2086). PollIntervalSeconds is
// how often IKE re-fetches the repository's issues and pull requests in the
// background so new issues, closed issues and PR state changes surface without
// a manual refresh; 0 turns polling off entirely, and anything between 1 and
// the floor is raised to it, so a mistyped interval cannot hammer the forge
// (every poll is a CLI/API round trip). Notify is the per-event-type
// notification style of the forge event surface those polls feed. Cache
// persists the last successful listing per project (#2108) so a freshly
// started IKE shows it instantly and refreshes incrementally; off drops both
// the snapshot file and the incremental path.
type Forge struct {
	PollIntervalSeconds int         `toml:"poll_interval_seconds"`
	Cache               bool        `toml:"cache"`
	Notify              ForgeNotify `toml:"notify"`
}

// ForgeNotify is the notification style per forge event kind (#2086). Each
// field takes "dialog" (centered, dismissable dialog over the workspace),
// "badge" (status-line unread badge only), "toast" (the ordinary bottom-right
// toast) or "off" (history only). The field names mirror
// forge.EventKind.Name(), so a new kind adds one field and one entry.
type ForgeNotify struct {
	IssueOpened     string `toml:"issue_opened"`
	IssueClosed     string `toml:"issue_closed"`
	PROpened        string `toml:"pr_opened"`
	PRMerged        string `toml:"pr_merged"`
	PRClosed        string `toml:"pr_closed"`
	PRChecksFailing string `toml:"pr_checks_failing"`
}

// Screenshot holds the in-IDE screenshot export settings (#2001). Directory
// is where view.exportScreenshot writes its PNGs: "~" expands, a relative path
// resolves against the project directory, and an empty value selects the
// built-in default (~/.ike/screenshots). The directory is created on the first
// capture, so it need not exist yet.
type Screenshot struct {
	Directory string `toml:"directory"`
}

// Remote holds the SFTP remote browser settings (#1997). MaxFetchMB caps the
// size of a remote file the browser will download into the local cache to
// preview it — the guard against pulling gigabytes over a slow link.
type Remote struct {
	MaxFetchMB int `toml:"max_fetch_mb"`
}

// Perf holds the performance HUD settings (#1999). HUDIntervalMs is how often
// the HUD closes a measurement window and refreshes — it is also the wake rate
// the HUD costs while open, so it is a setting rather than a constant.
// HUDHistorySeconds is the span the sparklines and the snapshot's min/avg/max
// cover; the ring length follows from the two.
type Perf struct {
	HUDIntervalMs     int `toml:"hud_interval_ms"`
	HUDHistorySeconds int `toml:"hud_history_seconds"`
	// WatchdogSeconds is the update-loop stall watchdog threshold (#2163): a
	// single Update/View pass in flight longer than this dumps every
	// goroutine's stack to the state dir, so a frozen session leaves
	// evidence. 0 disables the watchdog (the opt-out).
	WatchdogSeconds int `toml:"watchdog_seconds"`
}

// SnippetEntry is one user live template ([[snippets]], #1152). Trigger is the
// word that expands (identifier runes only — Tab expansion matches the word
// before the cursor); Body is the LSP-snippet-syntax text ($1, ${2:default};
// literal "\t" runs re-indent to the buffer's tab settings and "\n" lines to
// the current line's indent); Language scopes the entry to one language ID
// (internal/lang), empty means every buffer.
type SnippetEntry struct {
	Trigger  string `toml:"trigger"`
	Language string `toml:"language"`
	Body     string `toml:"body"`
}

// Tools holds the custom TUI tool panes (#741): [[tools.custom]] entries, each
// exposed as a palette command "tool.<name>" that opens a pane running the
// configured program directly (lazygit, htop, k9s, …), plus the optional slot
// template pinning tools to exact layout positions (#1897).
type Tools struct {
	Custom []ToolEntry `toml:"custom"`
	Layout ToolLayout  `toml:"layout"`
}

// ToolLayout is the named-slot template for tool placement (#1897). Template
// is an ASCII grid, one string per row (like CSS grid-template-areas): every
// cell names a slot by a single rune, each slot's cells must form a solid
// rectangle, "E" is the reserved editor region, and the arrangement must
// decompose into straight full-width/full-height cuts. Row/column counts set
// the proportions ({"XEEH","XEEH","TTZZ"} gives X a quarter of the width over
// two thirds of the height). Assign maps tools onto slots as "SLOT=tool"
// entries — the tool is a [[tools.custom]] name or a built-in id (see
// BuiltinAssignTools). An assigned tool always opens at its slot's exact
// position; slots of closed tools collapse, their space absorbed by the
// surviving neighbors. Several tools may share one slot: the first to open
// materializes the pane, later ones join it as tabs. Tools without an
// assignment keep the #1889 placement / adaptive behavior. An empty Template
// disables slot placement entirely.
type ToolLayout struct {
	Template []string `toml:"template"`
	Assign   []string `toml:"assign"`
}

// BuiltinAssignTools lists the built-in ids a [tools.layout] assign entry may
// name besides [[tools.custom]] names (#1946): the singleton tool windows,
// the Run tool (#1905), and the integrated terminal — an assigned "terminal"
// slot is where fresh terminal panes open, later ones joining as tabs; the
// popup overlay terminal is unaffected. This is the single authoritative
// list the settings form's hints/validation, the config validator and the
// app-side slot resolver share.
func BuiltinAssignTools() []string {
	return []string{
		"explorer", "vcs", "debug", "problems", "structure", "usages",
		"http", "breakpoints", "tests", "issues", "dom", "xdoctor", "run",
		"terminal",
	}
}

// ToolEntry is one configured TUI tool. Name is the display/command suffix
// ("lazygit" → command id "tool.lazygit"); Command is the program to run with
// Args; Cwd is the working directory (empty: the project root). Placement is
// the tool's configured home position (#1889, JetBrains-style docking):
// "left", "right", "top" or "bottom" docks a fresh open full-span against
// that workspace edge (an occupied slot adds the tool as a focused tab
// there); empty keeps the adaptive auxZone heuristic (#1588). Placement is
// intent, not state — moving the tool never rewrites it, so close + reopen
// returns to the configured edge. Multiple opts the tool into concurrent
// instances (#835): a "tool.<name>.new" command spawns additional panes;
// false (default) keeps the strict single-instance toggle. Global marks the
// tool as one process-wide instance shared across every workspace (#1890):
// opening it in another project re-attaches the same running session instead
// of spawning a second one, and switching or closing projects never ends it —
// only closing the pane or quitting IKE does. Global and Multiple are
// mutually exclusive; a config declaring both gets a diagnostic and Multiple
// is ignored. A global tool's Cwd resolves once, against the project where it
// first spawns.
type ToolEntry struct {
	Name      string   `toml:"name"`
	Command   string   `toml:"command"`
	Args      []string `toml:"args"`
	Cwd       string   `toml:"cwd"`
	Placement string   `toml:"placement"`
	Multiple  bool     `toml:"multiple"`
	Global    bool     `toml:"global"`
}

// Elasticsearch holds the Elasticsearch console configuration (#1927):
// [[elasticsearch.endpoints]] entries, each exposed as a palette command
// "es.console.<name>" that opens a read-only console pane against that
// cluster. Like every TOML list the endpoint list replaces across layers — a
// project-scope [[elasticsearch.endpoints]] table hides the user-scope one.
type Elasticsearch struct {
	Endpoints []ESEndpoint `toml:"endpoints"`
}

// ESEndpoint is one configured Elasticsearch cluster. Name is the display and
// command suffix ("prod" → command id "es.console.prod") and must be unique;
// URL is the cluster's base URL (http or https, host required). Auth is
// optional and one of two schemes: Username/Password become basic auth, or
// APIKey becomes an "Authorization: ApiKey …" header — configuring both is
// downgraded to basic auth with a diagnostic. The console only ever reads
// (search, mapping, cat APIs), so a read-only API key is enough.
type ESEndpoint struct {
	Name     string `toml:"name"`
	URL      string `toml:"url"`
	Username string `toml:"username"`
	Password string `toml:"password"`
	APIKey   string `toml:"api_key"`
}

// Debug holds debugger behaviour (0360). PHP carries the web/request listen
// mode's settings (#823). InlineValues renders the paused frame's local
// variable values as line-end annotations while stopped (#1914). SessionEnd
// picks what happens to the combined debug area when a session ends (#2190):
// "keep" leaves it open reviewable, "close" tidies it away.
type Debug struct {
	InlineValues bool     `toml:"inline_values"`
	SessionEnd   string   `toml:"session_end"`
	PHP          DebugPHP `toml:"php"`
}

// DebugPHP configures "listen for PHP debug connections" (#823). Port is the
// DBGp listener port (Xdebug's default is 9003). Hostname, when set, only
// accepts debug sessions whose request's $_SERVER['HTTP_HOST'] matches (port
// suffix ignored, case-insensitive) — other requests are detached so an
// unrelated vhost on the same php-fpm pool cannot hijack the debugger.
// PathMappings translate between the server's docroot and the project layout
// when they differ.
type DebugPHP struct {
	Port         int            `toml:"port"`
	Hostname     string         `toml:"hostname"`
	PathMappings []DebugPathMap `toml:"path_mappings"`
}

// DebugPathMap is one server→local path-prefix mapping ([[debug.php.path_mappings]]).
// Server is the path prefix as the engine reports it (e.g. /var/www/html);
// Local is the matching project path, absolute or project-relative.
type DebugPathMap struct {
	Server string `toml:"server"`
	Local  string `toml:"local"`
}

// Run holds run-configuration behaviour (0350, #576). Placement is the Run
// tool's home position (#1905): "bottom" (the default), "left", "right" or
// "top" dock the Run tool pane at that workspace edge, "in_pane" keeps the
// run output as a terminal tab in the focused editor pane. The pre-#1905
// "new_terminal" migrates to "bottom". A [tools.layout] slot assigned to
// "run" overrides the placement, like for any other tool. VSCodeLaunch
// merges compatible .vscode/launch.json entries into the run-configuration
// picker (#1914).
type Run struct {
	Placement    string `toml:"placement"`
	VSCodeLaunch bool   `toml:"vscode_launch"`
}

// Tasks holds task-discovery settings (#1915): the [[tasks.matcher]] custom
// problem matchers. Conventionally project-scoped — a matcher describes a
// project's build output.
type Tasks struct {
	Matchers []MatcherEntry `toml:"matcher"`
}

// MatcherEntry is one custom problem matcher ([[tasks.matcher]], #1915): a
// single-line regex whose capture groups, indexed 1-based by File/Line/Col/
// Severity/Message, pull a problem's fields out of a matching output line.
// File, Line and Message are required; Col and Severity are optional (0 =
// absent). A Severity group captures a word (error/warning/info/hint); lines
// without one fall back to DefaultSeverity ("" = error). Validation compiles
// the pattern and checks every group index, dropping a broken entry with a
// clear diagnostic instead of failing the load.
type MatcherEntry struct {
	Name            string `toml:"name"`
	Pattern         string `toml:"pattern"`
	File            int    `toml:"file"`
	Line            int    `toml:"line"`
	Col             int    `toml:"col"`
	Severity        int    `toml:"severity"`
	Message         int    `toml:"message"`
	DefaultSeverity string `toml:"default_severity"`
}

// Tests holds Test Results tool-window behaviour (#1911). ResultsWindow
// routes test runs whose language declares an output parser (`go test
// -json`, pytest) into the structured Test Results pane; off keeps every
// test run in the raw Run tool terminal (#1905). AutoOpen opens the pane
// when a captured test run starts; off, it only fills an already open pane.
type Tests struct {
	ResultsWindow bool `toml:"results_window"`
	AutoOpen      bool `toml:"auto_open"`
}

// Scratch holds the explorer's Scratches section behaviour (#1963, replacing
// the #1932 tool pane). Section shows the scratch store as a divider-separated
// section below the explorer's file tree; SectionHeight is the row count the
// section opens at (dragging the divider resizes it for the session, and that
// height persists with the explorer's session state); Sort orders the rows by
// "name" (the default, like the tree) or "modified" (newest first).
//
// Panel and PanelHeight are the legacy #1932 tool-pane keys: still decoded so
// old configs load without an unknown-setting warning, then migrated by
// Validate (panel_height seeds section_height; panel is dropped — the section
// replaces the pane and is visible by default).
type Scratch struct {
	Section       bool   `toml:"section"`
	SectionHeight int    `toml:"section_height"`
	Sort          string `toml:"sort"`
	Panel         bool   `toml:"panel"`
	PanelHeight   int    `toml:"panel_height"`
}

// Issues holds the forge Issues tool window's opening defaults (#2090, #2115).
// DefaultTab selects which of the pane's two views it opens on ("issues" or
// "prs"); DefaultSort is the list order both views start in ("relevance" —
// best fuzzy match while filtering, newest otherwise — "newest", "oldest",
// "updated" or "number"); DefaultFilter is the narrowing a freshly opened
// pane starts with, written in the same qualifier syntax the filter overlay's
// match input accepts ("is:open label:bug crash", internal/issuefilter).
// SavedFilters are named filters in that syntax ("triage=is:open label:bug"),
// offered on the filter overlay's saved row. All only seed a freshly opened
// pane: switching the
// tab, the sort order or a filter by hand wins for the rest of the session,
// so a live config reload never yanks the view away.
type Issues struct {
	DefaultTab    string   `toml:"default_tab"`
	DefaultSort   string   `toml:"default_sort"`
	DefaultFilter string   `toml:"default_filter"`
	SavedFilters  []string `toml:"saved_filters"`
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

// Terminal holds integrated-terminal behaviour (Roadmap 0170). Autosuggest
// is the completion popup's while-typing trigger (#740); ctrl+space stays
// available when it is off. ScrollbackLines bounds each terminal session's
// scrollback buffer (#1545) — the dominant memory cost per terminal pane,
// multiplied by parked background workspaces that keep ingesting output.
type Terminal struct {
	Shell           string `toml:"shell"`
	Autosuggest     bool   `toml:"autosuggest"`
	ScrollbackLines int    `toml:"scrollback_lines"`
	// SSHHosts are extra aliases the SSH host picker (#1938) offers beyond
	// the ones ~/.ssh/config declares — machines reachable by name that no
	// ssh config entry mentions.
	SSHHosts []string `toml:"ssh_hosts"`
}

// UI holds chrome toggles (Roadmap 0160). MenuBar shows the top menu row.
// Onboarded records that the welcome tour (#658) has been shown once; it is
// written when the tour opens (not closes), so quitting mid-tour never
// re-triggers it — the tour stays reachable via the palette.
type UI struct {
	MenuBar   bool `toml:"menu_bar"`
	Onboarded bool `toml:"onboarded"`
	// PopupMaxWidth caps centered popup windows (palette modes, modal shell,
	// settings) at this outer width in columns on large terminals (#932);
	// extra terminal width just adds margin. 0 disables the cap.
	PopupMaxWidth int `toml:"popup_max_width"`
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

// History holds per-file Timeline behaviour (#1916). TimelineSource is the
// default source filter the Timeline view opens with: "both" (local-history
// snapshots and git commits), "local" or "git"; the view's filter key cycles
// it for the open list without writing the setting.
type History struct {
	TimelineSource string `toml:"timeline_source"`
}

// Files holds external-file-change behaviour (Roadmap 0140). Watch enables the
// fsnotify project watcher; AutoReload ("clean" or "never") controls whether a
// clean editor buffer reloads in place when its file changes on disk.
// LargeFileKB and LargeFileLines are the large-file thresholds (#149): a file
// crossing either at load/reload is flagged and code insight (highlighting,
// LSP, watcher content hashing) degrades; 0 disables that guard.
// The LargeFile*KB per-feature thresholds (#2159) switch one service off
// earlier than the base cliff — highlighting, LSP didChange sync, the VCS
// gutter diff, the search match tally, and the on-save format chain each
// degrade once the file exceeds its own KB value; 0 (the default) means the
// feature simply follows the base thresholds.
// PersistentUndo (#148) keeps undo history across restarts (vim's undofile):
// stacks are written to the state store on save/close and adopted on open
// while the file content is unchanged.
// ChangeFeedLimit (#2000) bounds the session's external-change feed: how many
// externally changed files watch.changeFeed lists, oldest dropped first; 0
// turns the feed off entirely.
// Associations (#1365) maps file patterns to registered language ids —
// `"*.mytool" = "toml"`, `"Jenkinsfile" = "groovy"` — a slot map like
// explorer.colors: keys are single map keys (they contain dots and globs),
// matched against the base name. User associations win over the plugins'
// built-in extension/filename lists and sniffers; internal/lang resolves them
// (see lang.ByAssociation).
type Files struct {
	Watch                bool              `toml:"watch"`
	AutoReload           string            `toml:"auto_reload"`
	LargeFileKB          int               `toml:"large_file_kb"`
	LargeFileLines       int               `toml:"large_file_lines"`
	LargeFileHighlightKB int               `toml:"large_file_highlight_kb"`
	LargeFileLSPKB       int               `toml:"large_file_lsp_kb"`
	LargeFileVCSKB       int               `toml:"large_file_vcs_kb"`
	LargeFileSearchKB    int               `toml:"large_file_search_kb"`
	LargeFileFormatKB    int               `toml:"large_file_format_kb"`
	PersistentUndo       bool              `toml:"persistent_undo"`
	ChangeFeedLimit      int               `toml:"change_feed_limit"`
	Associations         map[string]string `toml:"associations"`
}

// Editor holds text-editing behaviour (Roadmap 0060 consumes most of it).
// AutoSave controls whether a dirty buffer saves itself: "off", "focus" (when
// focus leaves its pane or its document is replaced, #174), or "idle" (focus
// plus a debounced save after AutoSaveIdleMs of no edits, #731).
type Editor struct {
	AutoSave            string `toml:"auto_save"`
	AutoSaveIdleMs      int    `toml:"auto_save_idle_ms"`
	TabWidth            int    `toml:"tab_width"`
	UseSpaces           bool   `toml:"use_spaces"`
	LineNumbers         bool   `toml:"line_numbers"`
	RelativeLineNumbers bool   `toml:"relative_line_numbers"`
	Wrap                bool   `toml:"wrap"`
	ScrollOff           int    `toml:"scroll_off"`
	// TextWidth is the hard-wrap column the gq reflow operator targets
	// (#1193); 0 falls back to vim's 79.
	TextWidth int `toml:"text_width"`
	// ClipboardSync mirrors unnamed-register yanks (yy, y{motion}, visual y)
	// onto the system clipboard (#1256) — the conservative half of vim's
	// `clipboard=unnamed`: named registers and deletes/changes never sync.
	// On by default; false keeps yanks internal, leaving Cmd+C as the only
	// route to the system clipboard.
	ClipboardSync bool `toml:"clipboard_sync"`
	// ClipboardHistorySize bounds the clipboard history the paste-from-history
	// picker lists (#2061, cmd+shift+v): the newest N yanks, deletes and
	// pane-side copies, duplicates collapsed. Clamped to [1, 200]; the ring is
	// in-memory only and starts empty after a restart.
	ClipboardHistorySize int `toml:"clipboard_history_size"`
	// ClipboardHistoryMaxKB caps a single clipboard-history entry in kilobytes
	// (#2250). A yank, delete or pane copy larger than this is skipped by the
	// ring — never truncated into it — so the picker never offers a row that
	// pastes something other than what was copied; the registers still hold
	// the payload in full. Clamped to [1, 10240].
	ClipboardHistoryMaxKB  int  `toml:"clipboard_history_max_kb"`
	AutoIndent             bool `toml:"auto_indent"`
	AutoClosePairs         bool `toml:"auto_close_pairs"`
	TrimTrailingWhitespace bool `toml:"trim_trailing_whitespace"`
	InsertFinalNewline     bool `toml:"insert_final_newline"`
	// FormatOnSave runs LSP whole-document formatting before every manual
	// write (#1148); OrganizeImportsOnSave applies the server's
	// source.organizeImports code action first. Both are capability-gated
	// silent no-ops without a ready server, and only user-initiated saves
	// (:w, editor.write, Save All) run them — autosave and shutdown writes
	// stay raw.
	FormatOnSave          bool `toml:"format_on_save"`
	OrganizeImportsOnSave bool `toml:"organize_imports_on_save"`
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
	// RainbowIndentGuides colors each indent guide by its depth (#1628) with
	// the rainbow palette bracket pairs use, mixed toward the plain guide
	// colour so the indentation stays chrome. It is what makes nesting
	// readable where there are no brackets to color — YAML, Python. Only in
	// effect while IndentGuides draws the guides at all.
	RainbowIndentGuides bool `toml:"rainbow_indent_guides"`
	// StickyScroll pins the enclosing declaration lines (function/class
	// headers) at the top of the editor while scrolling inside their body
	// (#168); StickyScrollDepth caps how many nested headers are pinned.
	StickyScroll      bool `toml:"sticky_scroll"`
	StickyScrollDepth int  `toml:"sticky_scroll_depth"`
	// StickyScrollSymbols lets sticky scroll fall back to the LSP
	// documentSymbol tree the Structure view and the breadcrumbs already
	// cache (#2167), so languages with no Tree-sitter grammar still pin
	// their enclosing declarations. Tree-sitter scopes always win where
	// they exist — they need no server and follow every keystroke.
	StickyScrollSymbols bool `toml:"sticky_scroll_symbols"`
	// SmartPaste re-indents a multi-line linewise paste to the target line's
	// indentation, preserving relative structure (#1476).
	SmartPaste bool `toml:"smart_paste"`
	// MarkdownRendering enables the in-editor Markdown semi-preview (#881):
	// bold/italic/strikethrough text attributes, marker concealment on
	// non-cursor lines, and box-drawing pipe tables while the cursor is
	// outside the table.
	MarkdownRendering bool `toml:"markdown_rendering"`
	// CSVRendering enables the table-like rendering of separator-delimited
	// files (#1589): rainbow columns, concealed separators with display-only
	// alignment padding on non-caret lines, and the pinned header row.
	CSVRendering bool `toml:"csv_rendering"`
	// LogRendering enables the log-file rendering (#1621): severity colors,
	// dimmed timestamps, rainbow thread/logger names and ANSI escapes drawn
	// as their styles with the escape bytes concealed.
	LogRendering bool `toml:"log_rendering"`
	// FollowPollMs is the poll interval in milliseconds of editor follow mode
	// (#1928): while a view follows its file (view.toggleFollow), the app
	// polls the tracked open files this often as the fallback for
	// filesystems where fsnotify under-reports. Clamped to [100, 10000].
	FollowPollMs int `toml:"follow_poll_ms"`
	// TimestampDecoding renders numeric Unix epoch timestamps (seconds and
	// milliseconds) as their UTC form (#1618) in JSON values, log lines and
	// .http request bodies; the raw number reappears under the caret.
	TimestampDecoding bool `toml:"timestamp_decoding"`
	// UnicodeEscapeDecoding renders \uXXXX escapes (surrogate pairs combined,
	// Go's \UXXXXXXXX included) inside string literals as the escaped
	// character (#1620); the raw escape reappears under the caret.
	UnicodeEscapeDecoding bool `toml:"unicode_escape_decoding"`
	// EntityDecoding renders HTML/XML character references (&amp;, &#x2026;)
	// decoded (#1620) — the full named table in HTML, only the five
	// predefined entities in XML; the raw reference reappears under the
	// caret.
	EntityDecoding bool `toml:"entity_decoding"`
	// Base64Decoding renders base64 values decoded where base64 is the
	// convention — the data: block of a Kubernetes Secret in YAML — and only
	// when the payload is printable text (#1620); the raw value reappears
	// under the caret.
	Base64Decoding bool `toml:"base64_decoding"`
	// CronHints renders a cron expression's English reading after it
	// (#1624) — "*/5 * * * *  every 5 min" — in crontab files, CI YAML
	// `cron:` values and quoted expressions in config formats; the bare
	// expression reappears under the caret.
	CronHints bool `toml:"cron_hints"`
	// PemSummary collapses a PEM block onto its BEGIN marker plus a decoded
	// one-line summary (#1652) — subject CN, validity window, issuer, key
	// type, SANs for a certificate, a type label for everything else — with
	// expired certificates in the error colour and soon-expiring ones in the
	// warning colour; the raw base64 reappears with the cursor in the block.
	PemSummary bool `toml:"pem_summary"`
	// ByteSizeHints renders a byte count as a binary size (#1627) —
	// `10485760` as `10 MiB` — where the key names a byte count or the
	// value is a multiple of 1024; the raw number reappears under the caret.
	ByteSizeHints bool `toml:"byte_size_hints"`
	// DurationHints renders a millisecond/second count as a duration
	// (#1627) — `86400000` as `24h` — where the key names a timeout, a TTL
	// or an interval; the raw number reappears under the caret.
	DurationHints bool `toml:"duration_hints"`
	// DigitGrouping renders a plain integer of five digits or more grouped
	// (#1627) — `1000000` as `1_000_000`; the raw digits reappear under the
	// caret.
	DigitGrouping bool `toml:"digit_grouping"`
	// RadixHints appends a hex literal's decimal reading (#1627) —
	// `0x1F4  = 500` — and a permission/flag value's octal or hex one
	// (`420  = 0o644`); the bare literal reappears under the caret.
	RadixHints bool `toml:"radix_hints"`
	// NumberHintUnits maps field names to the unit their values are read in
	// (#1685), overriding the key heuristics of the four hint families above.
	// Each entry is `pattern=unit`, the pattern matched case-insensitively
	// over the whole field name with `*` wildcards: `*_bytes=bytes`,
	// `retention=s`, `created_at=timestamp-s`, `session_id=none`. A mapped
	// field is read in that unit and no other — a value in a `bytes` field
	// draws as a byte size even when its digits also read as a timestamp —
	// and `none` silences every stand-in over the field's values.
	NumberHintUnits []string `toml:"number_hint_units"`
	// PermissionHints renders an octal file mode's symbolic form after it
	// (#1656) — `0644  rw-r--r--`, `4755  rwsr-xr-x` — where the context
	// carries a permission: `chmod`/`install -m`/`mkdir -m` in shell,
	// `COPY --chmod=` in Dockerfiles, the mode arguments of `os.Chmod` and
	// friends in Go and Python, and `mode:`/`defaultMode:` keys in YAML; the
	// bare literal reappears under the caret.
	PermissionHints bool `toml:"permission_hints"`
	// CIDRHints renders a CIDR prefix's address range and size after it
	// (#1653) — `10.0.0.0/8  10.0.0.0–10.255.255.255, 16,777,214 hosts` —
	// in config formats, .http files and string literals in code; the bare
	// literal reappears under the caret.
	CIDRHints bool `toml:"cidr_hints"`
	// IDNHints renders a punycode hostname's decoded Unicode form after it
	// (#1653) — `xn--mnchen-3ya.de  münchen.de` — with a decoded name
	// carrying the homograph shape (mixed scripts in one label, or a whole
	// label of Latin look-alikes) drawn in the warning colour; the raw name
	// reappears under the caret.
	IDNHints bool `toml:"idn_hints"`
	// TogglePairs extends the value pairs the Toggle Value command (g!)
	// cycles (#1658). Each entry is `a=b`; matching is case-insensitive and
	// the replacement copies the original's capitalization. Entries listed
	// here are matched before the built-in pairs (true/false, on/off, yes/no,
	// enabled/disabled, ==/!=, &&/||, </>), so a member can be redefined.
	TogglePairs []string `toml:"toggle_pairs"`
	// ConcealInclude restricts every conceal family to the files matching one
	// of these globs (#1704) — `*.py`, `Makefile`, `**/config/**`. Empty (the
	// default) means no restriction. A pattern without a separator matches
	// the file's base name, one with a separator the whole path, anchored at
	// any segment boundary unless it starts with `/` or `**`; matching is
	// case-insensitive.
	ConcealInclude []string `toml:"conceal_include"`
	// ConcealExclude switches every conceal family off in the files matching
	// one of these globs (#1704). Exclude beats include, so a file matching
	// both conceals nothing.
	ConcealExclude []string `toml:"conceal_exclude"`
	// ConcealFileRules overrides the two lists above for a single conceal
	// family (#1704). Each entry is `family=pattern`, the pattern prefixed
	// `-` (or `!`) for an exclude and bare (or `+`) for an include:
	// `secret_masking=-testdata/**`, `cron_hints=*.log`. The family names are
	// the editor.* toggle keys without their prefix. A family whose own rules
	// decide a path never consults the global lists; one with nothing to say
	// follows them.
	ConcealFileRules []string `toml:"conceal_file_rules"`
	// SecretMasking renders the value of a secret-suspect key in a dotenv
	// file masked (#1623) — `*_TOKEN`, `*_SECRET`, `PASSWORD`, `CREDENTIALS`
	// and friends show as ••••; the raw value reappears under the caret.
	SecretMasking bool `toml:"secret_masking"`
	// SecretMaskingKeys extends the key patterns secret masking recognises
	// (#1712). Each entry is a pattern matched case-insensitively over the
	// whole key name with `*` wildcards — `MY_API_KEY`, `*_LICENSE`,
	// `db_pass*` — masking every key it covers. A pattern prefixed `-` (or
	// `!`) exempts instead, so a key the built-in patterns would mask stays
	// readable: `-PUBLIC_TOKEN`. Earlier entries win, and a key no entry
	// matches falls through to the built-in patterns.
	SecretMaskingKeys []string `toml:"secret_masking_keys"`
	// Hyperlinks wraps detected URLs — bare http(s) URLs and Markdown link
	// labels, which carry their link target — in OSC 8 hyperlink sequences
	// (#1655), so terminals that support them (Ghostty, iTerm2, kitty,
	// WezTerm) make the text clickable; terminals without support ignore the
	// zero-width sequences by spec.
	Hyperlinks bool `toml:"hyperlinks"`
	// DiffWordHighlight emphasizes the word-level changed range inside
	// paired removed/added lines of .diff/.patch buffers (#1630) with the
	// diff viewer's changed-range background; off keeps whole-line coloring
	// only.
	DiffWordHighlight bool `toml:"diff_word_highlight"`
	// RainbowBrackets colors bracket pairs by nesting depth (#789) with a
	// cycling palette derived from the active theme; brackets left without a
	// partner render as errors (#1628).
	RainbowBrackets bool `toml:"rainbow_brackets"`
	// SearchIgnoreCase makes the in-file "/" "?" search case-insensitive by
	// default (#1111): queries fold case without an explicit \c marker, and a
	// \C prefix forces exact matching. Off keeps smartcase (#257): an
	// all-lowercase pattern folds case, any uppercase rune matches exactly.
	SearchIgnoreCase bool `toml:"search_ignore_case"`
	// ColorPreview tints recognized color literals (#rrggbb, rgb(), hsl())
	// with their own color (#790).
	ColorPreview bool `toml:"color_preview"`
	// IDColors colors UUIDs and long hex hashes (git SHAs, request/trace
	// IDs) by hashing the identifier into the rainbow palette (#1626), so
	// every occurrence of the same identifier shares one color. Active in
	// logs, JSON/NDJSON, .http files and .http response bodies.
	IDColors bool `toml:"id_colors"`
	// IDColorMinLength is the minimum length of a bare hex run that counts
	// as an identifier (#1626); the default 7 is the abbreviated git SHA.
	IDColorMinLength int `toml:"id_color_min_length"`
	// Breadcrumbs renders a one-line symbol-path bar under an editor pane's
	// tab/title row (#1153): the documentSymbol chain enclosing the cursor,
	// clickable per segment. On by default (the JetBrains default); the row
	// only appears while symbol data exists for the file, so files without a
	// documentSymbol provider spend no editor row on it.
	Breadcrumbs bool `toml:"breadcrumbs"`
	// PostfixCompletion offers the JetBrains-style postfix templates after a
	// dot (#1913): `err.nil` completes to `if err == nil { … }`, `xs.range` to
	// a range loop. Off removes the whole source from the popup; the templates
	// themselves are the language's (lang.Language.Postfix), so a language
	// without any is unaffected either way.
	PostfixCompletion bool   `toml:"postfix_completion"`
	Tabs              Tabs   `toml:"tabs"`
	Marks             Marks  `toml:"marks"`
	Typing            Typing `toml:"typing"`
}

// Typing holds the while-you-type assistance switches (#1326) — edits the
// editor makes on its own as a character is typed, beyond auto-close pairs.
// Each aid is one switch so a user can keep the ones they like: which
// characters an aid applies to is the language's call (lang.SpaceAfter), not
// the user's.
type Typing struct {
	// SpaceAfterPunctuation inserts a space after punctuation the language
	// declares in SpaceAfter — ":" in JSON, so `"key":` becomes `"key": ` as
	// you type. Suppressed inside strings and comments, and when a space (or
	// the line end) already follows.
	SpaceAfterPunctuation bool `toml:"space_after_punctuation"`
}

// Marks holds the per-source, per-severity editor decoration toggles (#1259):
// each switch gates one mark class everywhere the editor decorates it —
// scrollbar stripe, gutter colouring and inline underlines. All on by default.
// The Problems tool window intentionally keeps showing every diagnostic
// regardless of these toggles, so nothing is silently lost.
type Marks struct {
	LSPErrors   bool `toml:"lsp_errors"`
	LSPWarnings bool `toml:"lsp_warnings"`
	LSPInfo     bool `toml:"lsp_info"`
	LSPHints    bool `toml:"lsp_hints"`
	GitAdded    bool `toml:"git_added"`
	GitChanged  bool `toml:"git_changed"`
	GitDeleted  bool `toml:"git_deleted"`
	// Inheritance gates the gutter ↑/↓ inheritance arrows (#1453) and the LSP
	// probe traffic computing them.
	Inheritance bool `toml:"inheritance"`
	// Coverage gates the per-line test-coverage bars a coverage run paints in
	// the gutter (#2081); the coverage.toggle command hides them per session
	// on top of this.
	Coverage bool `toml:"coverage"`
}

// Tabs holds editor-tab behaviour (Roadmap 0190). AlwaysShow renders the
// pane's tab bar even when it holds a single tab; by default the bar only
// appears with two or more tabs. Limit caps the open editor tabs per pane
// (#742, the JetBrains tab limit): opening a file beyond it closes the least
// recently used non-dirty file tab; 0 (or negative) disables the limit.
type Tabs struct {
	AlwaysShow bool `toml:"always_show"`
	Limit      int  `toml:"limit"`
}

// Explorer holds file-tree behaviour. Colors is a per-filetype color-name slot
// filled by Roadmap 0050. AutoReveal (#1042) is the JetBrains "autoscroll from
// source": when on, the tree reveals (expands ancestors, selects, scrolls to)
// the focused editor's file on every focus/tab switch; off by default.
// Icons (#1046) gates one-cell file-type marker glyphs before each name
// (plain unicode, no nerd font); off by default.
// Exclude (#1139) is a TOML array of base-name glob patterns
// (filepath.Match semantics: ".git", "*.pyc", "node_modules") hidden from the
// explorer tree at every depth, regardless of the show-hidden toggle —
// JetBrains' "Excluded files". Explorer-only: go-to-file, find-in-path and
// the LSP scan are unaffected. Default: .git, .idea, .DS_Store.
type Explorer struct {
	ShowHidden bool              `toml:"show_hidden"`
	GitStatus  bool              `toml:"git_status"`
	TreeIndent int               `toml:"tree_indent"`
	Sort       string            `toml:"sort"`
	AutoReveal bool              `toml:"auto_reveal"`
	Icons      bool              `toml:"icons"`
	Exclude    []string          `toml:"exclude"`
	Colors     map[string]string `toml:"colors"`
}

// Keymap selects a binding preset and carries per-action overrides. Bindings is
// a slot filled by Roadmap 0080.
// A binding key is a chord ("ctrl+s") or, since #1312, a chord qualified by
// the context it applies in ("editor.ctrl+s") — the config spelling that lets
// one chord run different commands in different panes. The keymap package
// parses the keys; this map only carries them, and the nested TOML spelling
// ([keymap.bindings.editor]) is flattened into it before decoding (see
// flattenSlotMaps).
// WhichKey gates the which-key hint popup for a held chord prefix (#1909) and
// WhichKeyDelayMs is how long the prefix must stay pending before the popup
// appears — a sequence completed faster than that never flashes one.
type Keymap struct {
	Preset          string            `toml:"preset"`
	Bindings        map[string]string `toml:"bindings"`
	WhichKey        bool              `toml:"which_key"`
	WhichKeyDelayMs int               `toml:"which_key_delay_ms"`
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
	CompletionAuto bool `toml:"completion_auto"`
	// CodeLens toggles server code lenses ("run test", reference counts)
	// rendered as virtual annotations on the anchored line and executable via
	// the lsp.codeLens command (#1912).
	CodeLens bool `toml:"code_lens"`
	// Folding toggles server-provided folding ranges (#1912). When off (or
	// the server lacks the capability) the editor's Tree-sitter folds are
	// used alone; when on, server ranges are merged in and win on conflicts.
	Folding bool `toml:"folding"`
	// SemanticTokens toggles the server semantic-token overlay refining the
	// Tree-sitter highlight (#9, #1912): parameters vs locals, readonly vs
	// mutable. Rendering and traffic are both gated; cached data is kept.
	SemanticTokens bool `toml:"semantic_tokens"`
	// SelectionRange makes extend/shrink selection prefer the server's
	// textDocument/selectionRange ladder (#1912); when off (or unsupported)
	// the commands fall back to Tree-sitter syntax ranges.
	SelectionRange bool `toml:"selection_range"`
	// WillRename sends workspace/willRenameFiles before explorer renames and
	// moves and applies the returned refactoring edits (import paths etc.,
	// #1912). The FS operation itself never depends on the server answering.
	WillRename bool                      `toml:"will_rename"`
	LogLevel   string                    `toml:"log_level"`
	Servers    map[string]map[string]any `toml:"servers"`
	// DiagnosticsIgnore suppresses matching diagnostics everywhere they surface
	// (#1259): editor decorations AND the Problems window — an ignored
	// diagnostic is dropped before any consumer sees it. Each rule is a string
	// of space-separated `source=<glob>` / `code=<glob>` conditions plus an
	// optional trailing `msg=<glob>` that consumes the rest of the rule; a bare
	// token is shorthand for code=. All present conditions must match
	// (case-insensitive, * wildcards). Conventionally project-scoped.
	DiagnosticsIgnore []string `toml:"diagnostics_ignore"`
	// DiagnosticsSeverity remaps published diagnostic severities per rule
	// (#1503): each entry is the diagnostics_ignore condition grammar plus a
	// trailing severity keyword — `[source=<glob>] [code=<glob>] [msg=<glob>]
	// <error|warning|info|hint|off>` — first match wins, `off` drops the
	// diagnostic. Codeless diagnostics (syntax errors) are never remapped.
	// Exact-code rules additionally pass through natively to servers that
	// support per-rule overrides (pyright's diagnosticSeverityOverrides).
	DiagnosticsSeverity []string `toml:"diagnostics_severity"`
	// Onboarded records that the first-start server-install dialog (#301) has
	// had its say (answered or skipped); it is never shown again once set.
	Onboarded bool `toml:"onboarded"`
}

// Theme selects the active palette; its contents are owned by Roadmap 0110.
type Theme struct {
	Name string `toml:"name"`
	// Auto syncs the theme with the terminal background (#1480, OSC 11): when
	// on, the background's light/dark classification picks Light or Dark
	// below. An explicit selection (themes.select.*, the settings picker)
	// turns it off again. Off by default.
	Auto bool `toml:"auto"`
	// Light and Dark name the pair Auto chooses between. Dark repurposes the
	// key of a never-read bool (pre-#1480); a leftover `dark = true` in an
	// old settings file surfaces as a decode diagnostic, not a crash.
	Light string `toml:"light"`
	Dark  string `toml:"dark"`
	// Captures overrides individual syntax-highlighting colours on top of the
	// active theme (#1318): capture name ("keyword", "constant.builtin",
	// "rainbow.0") -> colour token (name, #rrggbb, ANSI index). A slot map,
	// so it merges key by key across layers.
	Captures map[string]string `toml:"captures"`
	// Terminal overrides the integrated terminal's 16-colour ANSI palette and
	// its default foreground/background (#1363): "black", "bright_blue", …,
	// "foreground", "background" -> colour token. A slot map, so it merges key
	// by key across layers.
	Terminal map[string]string `toml:"terminal"`
}

// Project holds recent-project history. History is a replace-by-default list
// (see merge.go) of [[project.history]] entries, most-recent-first; its content
// rules (upsert, dedupe, cap) and UX live in internal/project (Roadmap 0090).
type Project struct {
	History     []ProjectHistoryEntry `toml:"history"`
	MaxHistory  int                   `toml:"max_history"`
	RestoreLast bool                  `toml:"restore_last"`
	// MaxWorkspaces caps the live background workspaces kept across seamless
	// project switches (0370, #780); exceeding it evicts the
	// least-recently-used one (with a confirm when unsaved buffers or
	// running processes would die). <=0 selects the default (3).
	MaxWorkspaces int `toml:"max_workspaces"`
	// BackgroundLSPTimeout is how long a parked background workspace keeps
	// its language servers alive (#1521): past it they stop via the LSP
	// manager's CloseRoot and respawn lazily on resume. A Go duration string
	// ("5m", "90s"); empty selects the default (5m), "off"/"0" disables the
	// shutdown (servers run for as long as the workspace stays parked).
	BackgroundLSPTimeout string `toml:"background_lsp_timeout"`
	// AutoSaveOnSwitch writes every dirty file-backed buffer of the departing
	// project before an orderly project switch (#2186), the way JetBrains
	// does — the switch itself never asks. Buffers that have no writable home
	// (untitled, read-only, changed on disk since the edits) are collected
	// into one decision dialog instead. False restores the pre-#2186
	// behaviour: dirty buffers simply park with their workspace.
	AutoSaveOnSwitch bool `toml:"auto_save_on_switch"`
	// Directory is the default parent for projects IKE creates itself —
	// today the clone target of project.clone (#1349), mirroring JetBrains'
	// ~/IdeaProjects. A leading `~` is expanded; empty selects the built-in
	// default (~/IkeProjects). The directory is created on first use, never
	// at startup. internal/project owns the resolution (ProjectsDir).
	Directory string `toml:"directory"`
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
