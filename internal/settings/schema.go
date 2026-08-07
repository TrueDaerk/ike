// Package settings implements the settings panel framework (Roadmap 0160,
// #91): a full-window overlay with a category list on the left and a
// schema-driven form on the right. Pages are data — an Entry names a config
// key, its control type, scope and docs; the form renders from the descriptor,
// never from hand-built page UIs. Every edit writes through the config
// write-back layer (#89) and hot-reloads; the panel re-reads the live config,
// so the config stays the single source of truth.
package settings

import "ike/internal/config"

// EntryType selects the control rendered for an entry.
type EntryType int

const (
	// Bool renders a toggle (enter flips and writes).
	Bool EntryType = iota
	// Int renders a number input with optional Min/Max clamping.
	Int
	// String renders a free-text input.
	String
	// Enum renders a fixed-choice control (enter cycles Options).
	Enum
	// Path renders a text input validated for existence on commit.
	Path
	// Chord renders a key-capture control (the next key press is the value).
	Chord
	// List renders a free-text input edited as a comma-separated list but
	// persisted as a TOML string array (#1139): the config schema field is a
	// []string, so committing the raw comma string would fail the typed
	// decode — commit splits it instead.
	List
	// IntList is List over a []int config field (#1663, editor.rulers): the
	// same indexed multi-value editor, but every element must parse as a
	// number and the commit writes a TOML integer array.
	IntList
)

// Entry describes one setting.
type Entry struct {
	Key         string // dotted config key, e.g. "editor.tab_width"
	Type        EntryType
	Title       string       // human-facing label
	Description string       // one-line help shown for the selected entry
	Scope       config.Scope // write target layer (user / project)
	Options     []string     // Enum: the allowed values, in cycle order
	Min, Max    int          // Int: inclusive bounds; both zero = unbounded
}

// Page is one category: a titled list of entries, or — when Custom is set — a
// self-rendered page model (the keymap editor #93) the panel hosts.
type Page struct {
	Title   string
	Entries []Entry
	Custom  PageModel
	// Section groups the category rail (#890): pages sharing a Section render
	// under one header. Empty joins the previous section.
	Section string
	// Description explains what the category is for. The detail column shows
	// it whenever no entry is selected (0460, #1295) — the third column is
	// never blank, and browsing the rail already teaches what a page does.
	Description string
}

// typeName names an entry's value type for the detail column's meta row
// (#1295): the reader sees what shape a value has before editing it.
func typeName(t EntryType) string {
	switch t {
	case Bool:
		return "bool"
	case Int:
		return "int"
	case Enum:
		return "enum"
	case Path:
		return "path"
	case Chord:
		return "chord"
	case List:
		return "list"
	case IntList:
		return "int list"
	default:
		return "string"
	}
}

// marker is the value marker rendered after a value in the settings column
// (#1295): it announces which editor the detail column opens, so the row says
// how it is edited before enter is pressed.
//
//	▸ list · ‹› stepper · ◉ toggle · ⌨ capture · ≡ multi-list · ✎ free text
func marker(t EntryType) string {
	switch t {
	case Bool:
		return "◉"
	case Int:
		return "‹›"
	case Enum:
		return "▸"
	case Chord:
		return "⌨"
	case List, IntList:
		return "≡"
	default: // String, Path
		return "✎"
	}
}

// InsertAfter puts page directly behind the page titled after, so a custom
// page can sit next to the schema page it belongs with (#1238: Syntax Colors
// behind Appearance) without BasePages having to know how to build it.
// Appends when the anchor is not found.
func InsertAfter(pages []Page, after string, page Page) []Page {
	for i, p := range pages {
		if p.Title != after {
			continue
		}
		// The following page must keep its own section header; a page
		// inserted mid-section carries none.
		out := make([]Page, 0, len(pages)+1)
		out = append(out, pages[:i+1]...)
		out = append(out, page)
		return append(out, pages[i+1:]...)
	}
	return append(pages, page)
}

// BasePages returns the built-in core pages (#92). themes is the registry's
// theme-name list for the Appearance enum (live preview: writing theme.name
// hot-reloads through the normal pipeline); lightThemes/darkThemes are its
// light/dark partitions for the auto-sync pair (#1480). Pages grow as
// features land; schema entries live next to their feature's config keys, and
// every key must exist in the typed schema (guarded by the no-dead-keys test).
func BasePages(themes, lightThemes, darkThemes []string) []Page {
	return []Page{
		{Section: "CORE", Title: "Editor", Description: "How text is edited: indentation, saving, wrapping and the gutter. Language-specific overrides come from .editorconfig when it is enabled.", Entries: []Entry{
			{Key: "editor.tab_width", Type: Int, Title: "Tab width", Description: "Columns per indentation step", Scope: config.UserScope, Min: 1, Max: 16},
			{Key: "editor.use_spaces", Type: Bool, Title: "Use spaces", Description: "Indent with spaces instead of tab characters", Scope: config.UserScope},
			{Key: "editor.auto_indent", Type: Bool, Title: "Auto indent", Description: "Carry the current line's indentation into new lines", Scope: config.UserScope},
			{Key: "editor.auto_close_pairs", Type: Bool, Title: "Auto-close brackets", Description: "Insert the matching ), ] or } when typing an opening bracket", Scope: config.UserScope},
			{Key: "editor.clipboard_sync", Type: Bool, Title: "Yank to system clipboard", Description: "Mirror yanks (yy, y{motion}, visual y) onto the system clipboard; named registers and deletes stay internal", Scope: config.UserScope},
			{Key: "editor.auto_save", Type: Enum, Title: "Auto save", Description: "Save a dirty buffer when focus leaves its pane (focus), additionally after an idle delay while editing (idle), or never (off)", Scope: config.UserScope, Options: []string{"focus", "idle", "off"}},
			{Key: "editor.auto_save_idle_ms", Type: Int, Title: "Auto save idle delay", Description: "Milliseconds a dirty buffer must stay quiet before idle auto save writes it (auto_save = idle)", Scope: config.UserScope, Min: 100, Max: 60000},
			{Key: "editor.trim_trailing_whitespace", Type: Bool, Title: "Trim trailing whitespace", Description: "Strip line-end whitespace on save", Scope: config.UserScope},
			{Key: "editor.format_on_save", Type: Bool, Title: "Format on save", Description: "Run LSP document formatting before every manual save (autosave stays raw)", Scope: config.UserScope},
			{Key: "editor.organize_imports_on_save", Type: Bool, Title: "Organize imports on save", Description: "Apply the server's organize-imports action before every manual save (autosave stays raw)", Scope: config.UserScope},
			{Key: "editor.insert_final_newline", Type: Bool, Title: "Insert final newline", Description: "End every saved file with a newline", Scope: config.UserScope},
			{Key: "editor.editorconfig", Type: Bool, Title: "EditorConfig", Description: "Honour .editorconfig files per buffer", Scope: config.UserScope},
			{Key: "editor.line_numbers", Type: Bool, Title: "Line numbers", Description: "Show the line-number gutter", Scope: config.UserScope},
			{Key: "editor.relative_line_numbers", Type: Bool, Title: "Relative line numbers", Description: "Count gutter lines away from the cursor (vim-style)", Scope: config.UserScope},
			{Key: "editor.scroll_off", Type: Int, Title: "Scroll offset", Description: "Minimum lines kept visible above and below the cursor", Scope: config.UserScope, Min: 0, Max: 50},
			{Key: "editor.text_width", Type: Int, Title: "Text width", Description: "Hard-wrap column the gq reflow operator targets (0 = vim's default 79)", Scope: config.UserScope, Min: 0, Max: 500},
			{Key: "editor.sticky_scroll", Type: Bool, Title: "Sticky scroll", Description: "Pin enclosing function/class header lines at the top while scrolling", Scope: config.UserScope},
			{Key: "editor.markdown_rendering", Type: Bool, Title: "Markdown rendering", Description: "Render Markdown inline styles, conceal markers (revealed while the caret is inside the span) and draw pipe tables with box characters, only the caret's cell showing raw source; toggle per view via Toggle Markdown Rendering", Scope: config.UserScope},
			{Key: "editor.csv_rendering", Type: Bool, Title: "CSV table rendering", Description: "Render csv/tsv files table-like: rainbow columns, aligned fields, a pinned header row, and separators concealed except under the caret", Scope: config.UserScope},
			{Key: "editor.timestamp_decoding", Type: Bool, Title: "Timestamp decoding", Description: "Show numeric Unix epoch timestamps (seconds and milliseconds, 2001-2100) as their UTC date and time in JSON values, log lines and .http request bodies; the raw number reappears under the caret; toggle per view via Toggle Timestamp Decoding", Scope: config.UserScope},
			{Key: "editor.unicode_escape_decoding", Type: Bool, Title: "Unicode escape decoding", Description: "Show \\uXXXX escapes (surrogate pairs combined, Go's \\UXXXXXXXX included) inside string literals as the escaped character; the raw escape reappears under the caret; toggle per view via Toggle Unicode Escape Decoding", Scope: config.UserScope},
			{Key: "editor.entity_decoding", Type: Bool, Title: "Entity decoding", Description: "Show HTML/XML character references (&amp;, &#x2026;) decoded — the full named table in HTML, only the five predefined entities in XML; the raw reference reappears under the caret; toggle per view via Toggle Entity Decoding", Scope: config.UserScope},
			{Key: "editor.base64_decoding", Type: Bool, Title: "Base64 decoding", Description: "Show base64 values decoded where base64 is the convention (Kubernetes Secret data: blocks in YAML) and the payload is printable text; the raw value reappears under the caret; toggle per view via Toggle Base64 Decoding", Scope: config.UserScope},
			{Key: "editor.secret_masking", Type: Bool, Title: "Secret masking", Description: "Mask dotenv values whose key names a secret (*_TOKEN, *_SECRET, PASSWORD, …) as ••••; the raw value reappears under the caret; toggle per view via Toggle Secret Masking", Scope: config.UserScope},
			{Key: "editor.cron_hints", Type: Bool, Title: "Cron hints", Description: "Show a cron expression's English reading after it (\"*/5 * * * *  every 5 min\") in crontab files, CI YAML cron:/schedule: values and quoted expressions in JSON/TOML; the bare expression reappears under the caret; toggle per view via Toggle Cron Hints", Scope: config.UserScope},
			{Key: "editor.pem_summary", Type: Bool, Title: "PEM summary", Description: "Collapse a PEM block onto its BEGIN marker plus a decoded one-line summary (certificates: subject CN, validity window, issuer, key type, SANs; everything else a type label), expired or not-yet-valid certificates in the error color and ones expiring within 30 days in the warning color; private keys are never parsed; the raw base64 reappears with the cursor in the block; toggle per view via Toggle PEM Summary", Scope: config.UserScope},
			{Key: "editor.byte_size_hints", Type: Bool, Title: "Byte size hints", Description: "Show a byte count as a binary size (10485760 as \"10 MiB\") where the key names a byte count (*size*, *bytes*, *memory*, …) or the value is a multiple of 1024; the raw number reappears under the caret; toggle per view via Toggle Byte Size Hints", Scope: config.UserScope},
			{Key: "editor.duration_hints", Type: Bool, Title: "Duration hints", Description: "Show a millisecond/second count as a duration (86400000 as \"24h\") where the key names a timeout, TTL or interval, with *_ms/*_seconds-style words pinning the unit; the raw number reappears under the caret; toggle per view via Toggle Duration Hints", Scope: config.UserScope},
			{Key: "editor.digit_grouping", Type: Bool, Title: "Digit grouping", Description: "Show plain integers of five digits or more grouped (1000000 as \"1_000_000\"); the raw digits reappear under the caret; toggle per view via Toggle Digit Grouping", Scope: config.UserScope},
			{Key: "editor.radix_hints", Type: Bool, Title: "Radix hints", Description: "Append a hex literal's decimal reading (\"0x1F4  = 500\") and a permission/flag value's octal or hex one (\"420  = 0o644\"); the bare literal reappears under the caret; toggle per view via Toggle Radix Hints", Scope: config.UserScope},
			{Key: "editor.permission_hints", Type: Bool, Title: "Permission hints", Description: "Show an octal file mode's symbolic form after it (\"0644  rw-r--r--\", \"4755  rwsr-xr-x\") where the context carries a permission: chmod/install -m/mkdir -m in shell, COPY --chmod= in Dockerfiles, the mode arguments of os.Chmod and friends in Go and Python, and mode:/defaultMode: keys in YAML; the bare literal reappears under the caret; toggle per view via Toggle Permission Hints", Scope: config.UserScope},
			{Key: "editor.cidr_hints", Type: Bool, Title: "CIDR hints", Description: "Show a CIDR prefix's address range and size after it (\"10.0.0.0/8  10.0.0.0–10.255.255.255, 16,777,214 hosts\") in config formats, .http files and string literals in code; the bare literal reappears under the caret; toggle per view via Toggle CIDR Hints", Scope: config.UserScope},
			{Key: "editor.idn_hints", Type: Bool, Title: "IDN hints", Description: "Show a punycode hostname's decoded Unicode form after it (\"xn--mnchen-3ya.de  münchen.de\"), with names carrying the homograph shape — mixed scripts in one label, or a whole label of Latin look-alikes — in the warning color; the raw name reappears under the caret; toggle per view via Toggle IDN Hints", Scope: config.UserScope},
			{Key: "editor.toggle_pairs", Type: List, Title: "Toggle pairs", Description: "Extra value pairs the Toggle Value Under Caret command (g!) cycles, each written as \"a=b\"; matching is case-insensitive and the replacement copies the original's capitalization (\"True\" becomes \"False\"). Entries here are matched before the built-in pairs true/false, on/off, yes/no, enabled/disabled, ==/!=, &&/|| and </>, so a member can be redefined", Scope: config.UserScope},
			{Key: "editor.log_rendering", Type: Bool, Title: "Log rendering", Description: "Render .log files readable: severity levels colored, timestamps dimmed, thread/logger names rainbow-colored, and ANSI escapes drawn as their styles with the escape bytes concealed (revealed under the caret); toggle per view via Toggle Log Rendering", Scope: config.UserScope},
			{Key: "editor.hyperlinks", Type: Bool, Title: "Clickable URLs", Description: "Wrap URLs in OSC 8 hyperlink sequences so supporting terminals (Ghostty, iTerm2, kitty, WezTerm) make them clickable — cmd/ctrl+click opens the browser; Markdown link labels carry their link target; terminals without support ignore the sequences", Scope: config.UserScope},
			{Key: "editor.diff_word_highlight", Type: Bool, Title: "Diff word highlighting", Description: "Emphasize the word-level changed range inside paired removed/added lines of .diff/.patch files with the diff viewer's changed-range background; off keeps whole-line coloring only", Scope: config.UserScope},
			{Key: "editor.rainbow_brackets", Type: Bool, Title: "Rainbow brackets", Description: "Color bracket pairs by nesting depth (theme-derived cycle); brackets left without a partner render underlined in the error color", Scope: config.UserScope},
			{Key: "editor.color_preview", Type: Bool, Title: "Color preview", Description: "Tint color literals (#rgb/#rrggbb/#rrggbbaa, rgb(), hsl()) with their own color", Scope: config.UserScope},
			{Key: "editor.id_colors", Type: Bool, Title: "Identifier colors", Description: "Color UUIDs and long hex hashes (git SHAs, request/trace IDs) by hashing the identifier into the rainbow palette, so every occurrence of the same identifier shares one color; active in logs, JSON and .http files and response bodies, toggle per view via Toggle Identifier Colors", Scope: config.UserScope},
			{Key: "editor.id_color_min_length", Type: Int, Title: "Identifier color minimum length", Description: "Shortest bare hex run that counts as an identifier (7 = abbreviated git SHA)", Scope: config.UserScope, Min: 6, Max: 64},
			{Key: "editor.sticky_scroll_depth", Type: Int, Title: "Sticky scroll depth", Description: "Maximum number of nested header lines pinned at once", Scope: config.UserScope, Min: 1, Max: 10},
			{Key: "editor.smart_paste", Type: Bool, Title: "Smart paste", Description: "Re-indent multi-line pastes to the target line's indentation, preserving relative structure", Scope: config.UserScope},
			{Key: "editor.search_ignore_case", Type: Bool, Title: "Case-insensitive search", Description: "Match in-file / and ? searches ignoring case by default; a \\C query prefix forces exact case. Off = smartcase: all-lowercase folds, any uppercase matches exactly", Scope: config.UserScope},
			{Key: "editor.wrap", Type: Bool, Title: "Soft wrap", Description: "Wrap long lines at the pane edge", Scope: config.UserScope},
			{Key: "editor.show_whitespace", Type: Enum, Title: "Show whitespace", Description: "Render spaces and tabs visibly", Scope: config.UserScope, Options: []string{"none", "trailing", "all"}},
			{Key: "editor.indent_guides", Type: Bool, Title: "Indent guides", Description: "Draw vertical lines at each indentation level", Scope: config.UserScope},
			{Key: "editor.rulers", Type: IntList, Title: "Rulers", Description: "Display columns tinted as vertical margin guides (80, 120); empty draws none. Columns below 1 are clamped to 1", Scope: config.UserScope},
			{Key: "editor.rainbow_indent_guides", Type: Bool, Title: "Rainbow indent guides", Description: "Color each indent guide by its depth with the rainbow palette, so nesting reads in files that have no brackets (YAML, Python); only in effect while indent guides are drawn", Scope: config.UserScope},
			{Key: "editor.breadcrumbs", Type: Bool, Title: "Breadcrumbs", Description: "Show the enclosing symbol path in a row under the editor's tab bar; segments are clickable", Scope: config.UserScope},
			{Key: "editor.tabs.always_show", Type: Bool, Title: "Always show tab bar", Description: "Render the pane's tab bar even with a single tab", Scope: config.UserScope},
			{Key: "editor.tabs.limit", Type: Int, Title: "Tab limit", Description: "Max open editor tabs per pane; opening beyond it closes the least recently used non-dirty tab (0 disables)", Scope: config.UserScope},
		}},
		{Title: "Typing Assistance", Description: "What the editor writes for you while you type. Each aid is one switch; which characters it applies to is the language's convention (#1326), not a setting.", Entries: []Entry{
			{Key: "editor.typing.space_after_punctuation", Type: Bool, Title: "Space after punctuation", Description: "Insert the conventional space after punctuation the language declares — \":\" in JSON, so \"key\": is completed as you type. Suppressed inside strings and comments", Scope: config.UserScope},
		}},
		{Title: "Diagnostics", Description: "Which problem markers decorate the editor — per source and severity. The Problems window always lists everything, regardless of these switches.", Entries: []Entry{
			// Per-source, per-severity decoration toggles (#1259): each switch
			// gates one mark class across the scrollbar stripe, gutter and
			// inline underlines. The Problems window keeps showing everything.
			{Key: "editor.marks.lsp_errors", Type: Bool, Title: "Error marks", Description: "Decorate LSP errors (scrollbar stripe, gutter, underline); the Problems window is unaffected", Scope: config.UserScope},
			{Key: "editor.marks.lsp_warnings", Type: Bool, Title: "Warning marks", Description: "Decorate LSP warnings (scrollbar stripe, gutter, underline); the Problems window is unaffected", Scope: config.UserScope},
			{Key: "editor.marks.lsp_info", Type: Bool, Title: "Info marks", Description: "Decorate LSP information diagnostics (scrollbar stripe, gutter, underline); the Problems window is unaffected", Scope: config.UserScope},
			{Key: "editor.marks.lsp_hints", Type: Bool, Title: "Hint marks", Description: "Decorate LSP hints (scrollbar stripe, gutter, underline); the Problems window is unaffected", Scope: config.UserScope},
			{Key: "editor.marks.git_added", Type: Bool, Title: "Git added marks", Description: "Mark added lines in the gutter and scrollbar", Scope: config.UserScope},
			{Key: "editor.marks.git_changed", Type: Bool, Title: "Git changed marks", Description: "Mark changed lines in the gutter and scrollbar", Scope: config.UserScope},
			{Key: "editor.marks.git_deleted", Type: Bool, Title: "Git deleted marks", Description: "Mark deletions in the gutter and scrollbar", Scope: config.UserScope},
			{Key: "editor.marks.inheritance", Type: Bool, Title: "Inheritance marks", Description: "Show ↑/↓ gutter arrows on symbols that implement/override a super declaration or have implementations; also gates the LSP probes computing them", Scope: config.UserScope},
			{Key: "lsp.diagnostics_ignore", Type: List, Title: "Ignored diagnostics", Description: "Suppression rules dropped everywhere (editor and Problems window): each rule combines source=<glob> code=<glob> and a trailing msg=<glob>; a bare token means code=. The editor's Ignore Diagnostic Under Caret command appends here", Scope: config.ProjectScope},
			{Key: "lsp.diagnostics_severity", Type: List, Title: "Diagnostic severity overrides", Description: "Remap rules applied everywhere (editor and Problems window): the ignore-rule conditions plus a trailing error/warning/info/hint/off — e.g. 'reportArgumentType warning'. First match wins, off drops the diagnostic; syntax errors (codeless diagnostics) always stay errors. The 'partial' keyword restricts a rule to union-partial type mismatches ('str | None' passed where 'str' is expected) — combine with source=, the parsed phrasing is server-specific. Exact-code rules also pass through to servers with native overrides (pyright)", Scope: config.ProjectScope},
		}},
		{Title: "Explorer", Description: "The project file tree: what it shows, how it is sorted and what it hides. Excluding an entry here hides it from the tree only; go-to-file and find-in-path still reach it.", Entries: []Entry{
			{Key: "explorer.show_hidden", Type: Bool, Title: "Show hidden files", Description: "List dot files and dot directories in the tree; entries matched by the exclude patterns below stay hidden either way", Scope: config.UserScope},
			{Key: "explorer.git_status", Type: Bool, Title: "Git status colors", Description: "Tint tree entries by their git status (added, modified, ignored) and roll a directory's status up from its children", Scope: config.UserScope},
			{Key: "explorer.icons", Type: Bool, Title: "File type icons", Description: "Draw a one-cell file-type marker glyph before each name (plain unicode, no nerd font needed)", Scope: config.UserScope},
			{Key: "explorer.auto_reveal", Type: Bool, Title: "Autoscroll from source", Description: "Reveal the focused editor's file in the tree on every focus/tab switch — expand its ancestors, select it and scroll it into view (JetBrains' \"autoscroll from source\")", Scope: config.UserScope},
			{Key: "explorer.sort", Type: Enum, Title: "Sort order", Description: "How entries are ordered inside a directory; directories always come before files", Scope: config.UserScope, Options: []string{"name", "type", "size", "modified"}},
			{Key: "explorer.tree_indent", Type: Int, Title: "Tree indent", Description: "Columns of indentation per nesting level in the tree", Scope: config.UserScope, Min: 0, Max: 8},
			{Key: "explorer.exclude", Type: List, Title: "Excluded entries", Description: "Comma-separated base-name glob patterns (.git, *.pyc, node_modules) hidden from the file tree at every depth, even with hidden files shown; explorer-only — go-to-file and find-in-path still see them", Scope: config.UserScope},
		}},
		{Title: "Language Support", Description: "The LSP subsystem's global switches: whether language servers run at all, what they may install, and which requests fire on their own. Per-language server commands live on the Language Servers page.", Entries: []Entry{
			{Key: "lsp.enabled", Type: Bool, Title: "Language servers", Description: "Run language servers at all; off disables completion, diagnostics, navigation and every other LSP-backed feature", Scope: config.UserScope},
			{Key: "lsp.auto_install", Type: Bool, Title: "Auto-install servers", Description: "Install a missing language server automatically when a file of its language opens, instead of only offering it", Scope: config.UserScope},
			{Key: "lsp.completion_auto", Type: Bool, Title: "Completion while typing", Description: "Open the completion popup on identifier characters; server trigger characters (\".\") and the manual ctrl+space request work either way", Scope: config.UserScope},
			{Key: "lsp.signature_auto", Type: Bool, Title: "Signature help while typing", Description: "Open the signature-help popup on the server's trigger characters (\"(\", \",\"); the manual Parameter Info command works either way", Scope: config.UserScope},
			{Key: "lsp.inlay_hints", Type: Bool, Title: "Inlay hints", Description: "Show inline parameter-name and type hints from the server; off by default — parameter info is available on demand instead", Scope: config.UserScope},
			{Key: "lsp.log_level", Type: Enum, Title: "Server log level", Description: "Verbosity of the language-server log the LSP: Show Server Log command opens", Scope: config.UserScope, Options: []string{"error", "warn", "info", "debug"}},
		}},
		{Title: "Appearance", Description: "How IKE looks: the color scheme, the menu bar and the size of centered popups.", Entries: []Entry{
			{Key: "theme.name", Type: Enum, Title: "Theme", Description: "Color scheme; applies immediately on selection (and turns off auto sync)", Scope: config.UserScope, Options: themes},
			{Key: "theme.auto", Type: Bool, Title: "Sync with terminal", Description: "Pick the light or dark theme below from the terminal's background colour (OSC 11); picking a theme explicitly turns this off", Scope: config.UserScope},
			{Key: "theme.light", Type: Enum, Title: "Light theme", Description: "Theme used when auto sync sees a light terminal background", Scope: config.UserScope, Options: lightThemes},
			{Key: "theme.dark", Type: Enum, Title: "Dark theme", Description: "Theme used when auto sync sees a dark terminal background", Scope: config.UserScope, Options: darkThemes},
			{Key: "ui.menu_bar", Type: Bool, Title: "Menu bar", Description: "Show the File/Edit/… menu row above the panes", Scope: config.UserScope},
			{Key: "ui.popup_max_width", Type: Int, Title: "Popup max width", Description: "Cap centered popups (palette, dialogs, settings) at this width in columns; 0 disables", Scope: config.UserScope},
			{Key: "palette.toggle_key", Type: Chord, Title: "Command palette key", Description: "Chord that opens the command palette", Scope: config.UserScope},
		}},
		{Title: "Command Palette", Description: "The command palette overlay: how many rows it shows, which mode a query without a prefix rune lands in, and what happens to commands belonging to another pane.", Entries: []Entry{
			{Key: "palette.max_results", Type: Int, Title: "Result rows", Description: "Result rows shown at once; the list still scrolls past it", Scope: config.UserScope, Min: 1, Max: 100},
			{Key: "palette.default_mode", Type: Enum, Title: "Default mode", Description: "Prefix assumed when the query starts with no mode rune: \":\" ranks it as a command, \"@\" as a file name", Scope: config.UserScope, Options: []string{":", "@"}},
			{Key: "palette.off_context", Type: Enum, Title: "Off-context commands", Description: "How command mode treats commands scoped to a pane other than the focused one: rank them last, or hide them", Scope: config.UserScope, Options: []string{"rank", "hide"}},
		}},
		{Title: "Files & Session", Description: "Opening, watching and restoring files: which project comes back on start, how external changes are handled and when a file counts as too large for language features.", Entries: []Entry{
			{Key: "project.restore_last", Type: Bool, Title: "Restore last project", Description: "Reopen the previous project's workspace on start", Scope: config.UserScope},
			{Key: "project.directory", Type: String, Title: "Project directory", Description: "Default parent for projects IKE creates itself (clone repository); a leading ~ is expanded and the directory is created on first use", Scope: config.UserScope},
			{Key: "project.max_history", Type: Int, Title: "Recent projects kept", Description: "How many entries the recent-projects list keeps; the oldest fall off", Scope: config.UserScope, Min: 0, Max: 200},
			{Key: "project.max_workspaces", Type: Int, Title: "Background workspaces", Description: "Live background workspaces kept across seamless project switches; exceeding it evicts the least recently used one (confirming first when unsaved buffers or running processes would die). 0 selects the built-in default of 3", Scope: config.UserScope, Min: 0, Max: 20},
			{Key: "project.background_lsp_timeout", Type: String, Title: "Background LSP timeout", Description: "How long a parked background workspace keeps its language servers alive, as a Go duration (\"5m\", \"90s\"); past it they stop and respawn lazily on resume. Empty selects 5m, \"off\" keeps them running", Scope: config.UserScope},
			{Key: "files.watch", Type: Bool, Title: "Watch files", Description: "Report external file changes (fsnotify on the project root)", Scope: config.UserScope},
			{Key: "files.auto_reload", Type: Enum, Title: "Auto reload", Description: "Reload clean buffers when their file changes on disk", Scope: config.UserScope, Options: []string{"clean", "never"}},
			{Key: "files.persistent_undo", Type: Bool, Title: "Persistent undo", Description: "Keep undo history across restarts while the file is unchanged", Scope: config.UserScope},
			{Key: "files.large_file_kb", Type: Int, Title: "Large file threshold (KB)", Description: "Above this size, highlighting and language features are disabled for the file (#149); 0 disables the size guard. Applies to subsequently opened or reloaded files", Scope: config.UserScope, Min: 0},
			{Key: "files.large_file_lines", Type: Int, Title: "Large file threshold (lines)", Description: "Above this line count, highlighting and language features are disabled for the file (#149); 0 disables the line guard. Applies to subsequently opened or reloaded files", Scope: config.UserScope, Min: 0},
		}},
		{Title: "Backup", Description: "Crash recovery: dirty buffers are snapshotted while you type and offered back after an unclean exit.", Entries: []Entry{
			{Key: "backup.enable", Type: Bool, Title: "Crash recovery", Description: "Snapshot dirty buffers for recovery; off also purges existing snapshots", Scope: config.UserScope},
			{Key: "backup.debounce_ms", Type: Int, Title: "Snapshot debounce", Description: "Milliseconds a dirty buffer must stay quiet before it is snapshotted", Scope: config.UserScope, Min: 100, Max: 60000},
			{Key: "backup.max_age_days", Type: Int, Title: "Snapshot max age", Description: "Days before leftover snapshots are pruned at startup (after the restore prompt)", Scope: config.UserScope, Min: 1, Max: 365},
		}},
		{Title: "Terminal", Description: "The integrated terminal and what it offers while you type at the shell prompt.", Entries: []Entry{
			{Key: "terminal.shell", Type: Path, Title: "Shell", Description: "Program new terminal sessions spawn; empty follows $SHELL. Applies to sessions started after the change", Scope: config.UserScope},
			{Key: "terminal.autosuggest", Type: Bool, Title: "Command auto-suggest", Description: "Popup with command/path/make-target completions while typing at the shell prompt; ctrl+space opens it on demand either way", Scope: config.UserScope},
			{Key: "terminal.scrollback_lines", Type: Int, Title: "Scrollback lines", Description: "Lines of scrollback each terminal session keeps (#1545); the main memory cost per terminal pane. Applies to new sessions and, on lowering, trims live ones forward — already-trimmed history is not restored by raising it", Scope: config.UserScope, Min: 100, Max: 1000000},
		}},
		{Title: "Run", Description: "Where a run configuration opens its output.", Entries: []Entry{
			{Key: "run.placement", Type: Enum, Title: "Run placement", Description: "Where a run opens when no unused terminal exists: a terminal tab in the editor pane, or a new terminal pane split off the editor", Scope: config.UserScope, Options: []string{"in_pane", "new_terminal"}},
		}},
		{Title: "Debug", Description: "Debugger transport settings. PHP debugging listens for incoming Xdebug connections; the hostname filter keeps foreign sessions out.", Entries: []Entry{
			{Key: "debug.php.port", Type: Int, Title: "PHP listen port", Description: "DBGp port debug.listen binds for incoming Xdebug connections (Xdebug's default is 9003)", Scope: config.UserScope, Min: 1, Max: 65535},
			{Key: "debug.php.hostname", Type: String, Title: "PHP hostname filter", Description: "Only accept listen-mode debug sessions whose request HTTP_HOST matches (port suffix ignored); empty accepts all — per project", Scope: config.ProjectScope},
		}},
		{Title: "Notifications", Description: "Toasts and the notification history: how long they stay and which severities are worth interrupting for.", Entries: []Entry{
			{Key: "notifications.timeout_seconds", Type: Int, Title: "Notification timeout", Description: "Seconds before info/warn toasts expire", Scope: config.UserScope, Min: 1, Max: 300},
			{Key: "notifications.min_severity", Type: Enum, Title: "Notification severity floor", Description: "Below this severity notifications go to the history only", Scope: config.UserScope, Options: []string{"info", "warn", "error"}},
		}},
		{Title: "TODO Index", Description: "The project-wide comment-tag index behind the TODO tool window.", Entries: []Entry{
			{Key: "todo.patterns", Type: List, Title: "Tag words", Description: "Comment tag words the project scan matches as whole words, case-insensitively (TODO, FIXME, HACK, XXX); entries are literals, not regexes", Scope: config.UserScope},
		}},
		{Title: "Marketplace Catalog", Description: "Where the plugin marketplace fetches its catalog from. The Marketplace page installs from whatever this URL serves.", Entries: []Entry{
			{Key: "marketplace.catalog_url", Type: String, Title: "Catalog URL", Description: "HTTPS location of the marketplace index.json; empty falls back to the built-in default, which may itself be empty — then the marketplace stays disabled", Scope: config.UserScope},
		}},
	}
}
