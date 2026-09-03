// Package settings implements the settings panel framework (Roadmap 0160,
// #91): a full-window overlay with a category list on the left and a
// schema-driven form on the right. Pages are data — an Entry names a config
// key, its control type, scope and docs; the form renders from the descriptor,
// never from hand-built page UIs. Every edit writes through the config
// write-back layer (#89) and hot-reloads; the panel re-reads the live config,
// so the config stays the single source of truth.
package settings

import (
	"image/color"
	"path/filepath"
	"strconv"
	"strings"

	"ike/internal/config"
	"ike/internal/issuefilter"
	"ike/internal/theme"
)

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
	// Path renders a text input with live filesystem completion, validated for
	// existence on commit.
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
	// Dirs narrows a Path entry to directories (#1720: project.directory names
	// a folder, never a file). The completion offers directories only, and the
	// commit check accepts a directory that does not exist yet as long as its
	// parent does — such an entry is a target the consumer creates on first use.
	Dirs bool
	// Preview renders an inline, already-styled preview for one enum option
	// (#1664: the theme list's palette swatches); nil or an empty return
	// means the option row has no preview.
	Preview func(option string) string
	// RowColors paints one enum option's row in that option's own colors
	// (#1688: a theme row is a live mini-preview of the theme, up to the
	// swatch strip). A false return leaves the row in the panel's colors.
	RowColors func(option string) (fg, bg color.Color, ok bool)
	// EntryHints supplies candidate values for the List element being typed
	// (#1946: slot letters / tool ids for tools.layout.assign), rendered under
	// the input and re-narrowed on every keystroke. lookup reads another
	// entry's effective — staged-first — value, so hints follow an uncommitted
	// template edit in the same session.
	EntryHints func(lookup func(key string) string, text string) []string
	// ValidateEntry rejects a committed List element with a message naming the
	// valid values; "" accepts (#1946). Same lookup as EntryHints.
	ValidateEntry func(lookup func(key string) string, text string) string
	// ValidateInt rejects a committed Int value with a message naming the
	// valid range; "" accepts (#2085). It exists for the entries whose valid
	// set is not one interval — forge.poll_interval_seconds takes 0 (polling
	// off) or ten seconds and up, never the gap between them. The steppers
	// jump such a gap instead of stopping inside it, so the hook only has to
	// answer for typed values.
	ValidateInt func(v int) string
	// ValidateString rejects a committed String value with a message naming
	// what is valid; "" accepts (#2115). It exists for the free-text entries
	// whose value has a grammar of its own — issues.default_filter is a
	// filter expression, not arbitrary text, so a typo has to be caught in
	// the form instead of silently ignored when the pane opens.
	ValidateString func(v string) string
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

// AttachCustom gives the page titled title a custom PageModel while keeping
// its schema entries (#2133: Conceal & Hints). The panel renders the model —
// Custom wins over Entries — and the entries stay the page's documented key
// list, so docgen keeps rendering the table and the no-dead-keys test keeps
// covering the keys. Pages the anchor does not name are returned unchanged.
func AttachCustom(pages []Page, title string, model PageModel) []Page {
	for i := range pages {
		if pages[i].Title == title {
			pages[i].Custom = model
			break
		}
	}
	return pages
}

// forgeNotifyStyles are the options every forge.notify.* entry offers (#2086),
// in escalating prominence order so cycling with enter reads naturally.
var forgeNotifyStyles = []string{"off", "toast", "badge", "dialog"}

// BasePages returns the built-in core pages (#92). themes is the registry's
// theme-name list for the Appearance enum (live preview: writing theme.name
// hot-reloads through the normal pipeline); lightThemes/darkThemes are its
// light/dark partitions for the auto-sync pair (#1480). extraThemes is the
// registry's plugin-registered theme list, so the theme enums' palette
// swatches (#1664) can resolve non-built-in names too. Pages grow as
// features land; schema entries live next to their feature's config keys, and
// every key must exist in the typed schema (guarded by the no-dead-keys test).
func BasePages(themes, lightThemes, darkThemes []string, extraThemes ...theme.Theme) []Page {
	swatch := themePreviewFn(extraThemes)
	rowColors := themeRowColorsFn(extraThemes)
	return []Page{
		{Section: "CORE", Title: "Editor", Description: "How text is edited: indentation, saving, wrapping and the gutter. Language-specific overrides come from .editorconfig when it is enabled.", Entries: []Entry{
			{Key: "editor.tab_width", Type: Int, Title: "Tab width", Description: "Columns per indentation step", Scope: config.UserScope, Min: 1, Max: 16},
			{Key: "editor.use_spaces", Type: Bool, Title: "Use spaces", Description: "Indent with spaces instead of tab characters", Scope: config.UserScope},
			{Key: "editor.auto_indent", Type: Bool, Title: "Auto indent", Description: "Carry the current line's indentation into new lines", Scope: config.UserScope},
			{Key: "editor.auto_close_pairs", Type: Bool, Title: "Auto-close brackets", Description: "Insert the matching ), ] or } when typing an opening bracket", Scope: config.UserScope},
			{Key: "editor.clipboard_sync", Type: Bool, Title: "Yank to system clipboard", Description: "Mirror yanks (yy, y{motion}, visual y) onto the system clipboard; named registers and deletes stay internal", Scope: config.UserScope},
			{Key: "editor.clipboard_history_size", Type: Int, Title: "Clipboard history size", Description: "How many recent copies Paste from History (cmd+shift+v) keeps: yanks, deletes and pane copy actions, newest first with duplicates collapsed. The ring lives in memory only and starts empty after a restart", Scope: config.UserScope, Min: 1, Max: 200},
			{Key: "editor.clipboard_history_max_kb", Type: Int, Title: "Clipboard entry limit", Description: "Largest single copy Paste from History keeps, in kilobytes. Bigger yanks, deletes and pane copies are skipped by the ring — never truncated — and still paste in full from the registers", Scope: config.UserScope, Min: 1, Max: 10240},
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
			{Key: "editor.toggle_pairs", Type: List, Title: "Toggle pairs", Description: "Extra value pairs the Toggle Value Under Caret command (g!) cycles, each written as \"a=b\"; matching is case-insensitive and the replacement copies the original's capitalization (\"True\" becomes \"False\"). Entries here are matched before the built-in pairs true/false, on/off, yes/no, enabled/disabled, ==/!=, &&/|| and </>, so a member can be redefined", Scope: config.UserScope},
			{Key: "editor.follow_poll_ms", Type: Int, Title: "Follow poll interval", Description: "Milliseconds between polls of the open files while a view follows its file, tail -f style (Toggle Follow). The poll is the fallback for filesystems where the native watcher under-reports; it runs only while at least one view follows, so there is no idle cost", Scope: config.UserScope, Min: 100, Max: 10000},
			{Key: "editor.hyperlinks", Type: Bool, Title: "Clickable URLs", Description: "Wrap URLs in OSC 8 hyperlink sequences so supporting terminals (Ghostty, iTerm2, kitty, WezTerm) make them clickable — cmd/ctrl+click opens the browser; Markdown link labels carry their link target; terminals without support ignore the sequences", Scope: config.UserScope},
			{Key: "editor.diff_word_highlight", Type: Bool, Title: "Diff word highlighting", Description: "Emphasize the word-level changed range inside paired removed/added lines of .diff/.patch files with the diff viewer's changed-range background; off keeps whole-line coloring only", Scope: config.UserScope},
			{Key: "editor.sticky_scroll_depth", Type: Int, Title: "Sticky scroll depth", Description: "Maximum number of nested header lines pinned at once", Scope: config.UserScope, Min: 1, Max: 10},
			{Key: "editor.sticky_scroll_symbols", Type: Bool, Title: "Sticky scroll from symbols", Description: "Let sticky scroll fall back to the language server's document symbols when the language has no Tree-sitter grammar, reusing the tree the Structure view and breadcrumbs already cache (no extra requests); Tree-sitter scopes always win where they exist", Scope: config.UserScope},
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
		{Section: "CORE", Title: "Conceal & Hints", Description: "The conceal and inline-hint families: what the editor draws in place of — or after — the raw bytes. These are the config defaults; a per-view toggle from the editor (Toggle Timestamp Decoding, …) overrides them for that buffer only. The file rules below gate every family by path.", Entries: []Entry{
			// Rendering layers: whole-file readable renderings.
			{Key: "editor.markdown_rendering", Type: Bool, Title: "Markdown rendering", Description: "Render Markdown inline styles, conceal markers (revealed while the caret is inside the span) and draw pipe tables with box characters, only the caret's cell showing raw source; toggle per view via Toggle Markdown Rendering", Scope: config.UserScope},
			{Key: "editor.csv_rendering", Type: Bool, Title: "CSV table rendering", Description: "Render csv/tsv files table-like: rainbow columns, aligned fields, a pinned header row, and separators concealed except under the caret", Scope: config.UserScope},
			{Key: "editor.log_rendering", Type: Bool, Title: "Log rendering", Description: "Render .log files readable: severity levels colored, timestamps dimmed, thread/logger names rainbow-colored, and ANSI escapes drawn as their styles with the escape bytes concealed (revealed under the caret); toggle per view via Toggle Log Rendering", Scope: config.UserScope},
			// Decoders: an encoded value drawn as what it encodes.
			{Key: "editor.timestamp_decoding", Type: Bool, Title: "Timestamp decoding", Description: "Show numeric Unix epoch timestamps (seconds and milliseconds, 2001-2100) as their UTC date and time in JSON values, log lines and .http request bodies; the raw number reappears under the caret; toggle per view via Toggle Timestamp Decoding", Scope: config.UserScope},
			{Key: "editor.unicode_escape_decoding", Type: Bool, Title: "Unicode escape decoding", Description: "Show unicode escapes inside string literals as the escaped character — \\uXXXX (surrogate pairs combined), \\UXXXXXXXX, and, in the languages that have them, \\u{X…} and \\xNN; the raw escape reappears under the caret; toggle per view via Toggle Unicode Escape Decoding", Scope: config.UserScope},
			{Key: "editor.entity_decoding", Type: Bool, Title: "Entity decoding", Description: "Show HTML/XML character references (&amp;, &#x2026;) decoded — the full named table in HTML, only the five predefined entities in XML; the raw reference reappears under the caret; toggle per view via Toggle Entity Decoding", Scope: config.UserScope},
			{Key: "editor.base64_decoding", Type: Bool, Title: "Base64 decoding", Description: "Show base64 values decoded where base64 is the convention (Kubernetes Secret data: blocks in YAML) and the payload is printable text; the raw value reappears under the caret; toggle per view via Toggle Base64 Decoding", Scope: config.UserScope},
			// Value hints: a stand-in appended after the literal.
			{Key: "editor.cron_hints", Type: Bool, Title: "Cron hints", Description: "Show a cron expression's English reading after it (\"*/5 * * * *  every 5 min\") in crontab files, CI YAML cron:/schedule: values and quoted expressions in JSON/TOML; the bare expression reappears under the caret; toggle per view via Toggle Cron Hints", Scope: config.UserScope},
			{Key: "editor.pem_summary", Type: Bool, Title: "PEM summary", Description: "Collapse a PEM block onto its BEGIN marker plus a decoded one-line summary (certificates: subject CN, validity window, issuer, key type, SANs; everything else a type label), expired or not-yet-valid certificates in the error color and ones expiring within 30 days in the warning color; private keys are never parsed; the raw base64 reappears with the cursor in the block; toggle per view via Toggle PEM Summary", Scope: config.UserScope},
			{Key: "editor.byte_size_hints", Type: Bool, Title: "Byte size hints", Description: "Show a byte count as a binary size (10485760 as \"10 MiB\") where the key names a byte count (*size*, *bytes*, *memory*, …) or the value is a multiple of 1024; the raw number reappears under the caret; toggle per view via Toggle Byte Size Hints", Scope: config.UserScope},
			{Key: "editor.duration_hints", Type: Bool, Title: "Duration hints", Description: "Show a millisecond/second count as a duration (86400000 as \"24h\") where the key names a timeout, TTL or interval, with *_ms/*_seconds-style words pinning the unit; the raw number reappears under the caret; toggle per view via Toggle Duration Hints", Scope: config.UserScope},
			{Key: "editor.digit_grouping", Type: Bool, Title: "Digit grouping", Description: "Show plain integers of five digits or more grouped (1000000 as \"1_000_000\"); the raw digits reappear under the caret; toggle per view via Toggle Digit Grouping", Scope: config.UserScope},
			{Key: "editor.radix_hints", Type: Bool, Title: "Radix hints", Description: "Append a hex literal's decimal reading (\"0x1F4  = 500\") and a permission/flag value's octal or hex one (\"420  = 0o644\"); the bare literal reappears under the caret; toggle per view via Toggle Radix Hints", Scope: config.UserScope},
			{Key: "editor.permission_hints", Type: Bool, Title: "Permission hints", Description: "Show an octal file mode's symbolic form after it (\"0644  rw-r--r--\", \"4755  rwsr-xr-x\") where the context carries a permission: chmod/install -m/mkdir -m in shell, COPY --chmod= in Dockerfiles, the mode arguments of os.Chmod and friends in Go and Python, and mode:/defaultMode: keys in YAML; the bare literal reappears under the caret; toggle per view via Toggle Permission Hints", Scope: config.UserScope},
			{Key: "editor.cidr_hints", Type: Bool, Title: "CIDR hints", Description: "Show a CIDR prefix's address range and size after it (\"10.0.0.0/8  10.0.0.0–10.255.255.255, 16,777,214 hosts\") in config formats, .http files and string literals in code; the bare literal reappears under the caret; toggle per view via Toggle CIDR Hints", Scope: config.UserScope},
			{Key: "editor.idn_hints", Type: Bool, Title: "IDN hints", Description: "Show a punycode hostname's decoded Unicode form after it (\"xn--mnchen-3ya.de  münchen.de\"), with names carrying the homograph shape — mixed scripts in one label, or a whole label of Latin look-alikes — in the warning color; the raw name reappears under the caret; toggle per view via Toggle IDN Hints", Scope: config.UserScope},
			// Secret masking and the field→unit mapping feeding the number hints.
			{Key: "editor.secret_masking", Type: Bool, Title: "Secret masking", Description: "Mask dotenv values whose key names a secret (*_TOKEN, *_SECRET, PASSWORD, …) as ••••; the raw value reappears under the caret; toggle per view via Toggle Secret Masking", Scope: config.UserScope},
			{Key: "editor.secret_masking_keys", Type: List, Title: "Secret masking key patterns", Description: "Extra key patterns whose values secret masking hides, beyond the built-in ones. Each entry is matched case-insensitively over the whole key name with * wildcards (\"MY_API_KEY\", \"*_LICENSE\", \"db_pass*\"). A pattern prefixed - (or !) exempts instead, so a key the built-in patterns would mask stays readable (\"-PUBLIC_TOKEN\"). Earlier entries win, and a key no entry matches falls through to the built-in patterns; masking still needs editor.secret_masking on and the file to pass the conceal filters", Scope: config.UserScope},
			{Key: "editor.number_hint_units", Type: List, Title: "Number hint field units", Description: "Map field names to the unit their numbers are read in, overriding the key heuristics of the byte size, duration, digit grouping and radix hints. Each entry is written \"pattern=unit\", the pattern matched case-insensitively over the whole field name with * wildcards (\"*_bytes=bytes\", \"retention=s\", \"created_at=timestamp-s\", \"session_id=none\"). Units: bytes, a duration unit (ns, us, ms, s, min, h, d), timestamp-s, timestamp-ms, octal, hex, group, none. A mapped field is read in that unit and no other — a value in a bytes field draws as a byte size even where its digits also read as a timestamp — and none silences every stand-in over the field's values. The unit is the base the raw number is read *in*, not just the family it draws as: with \"request_timeout=s\" the value 1500 is 1500 seconds and draws as \"25m\"", Scope: config.UserScope, EntryHints: numberUnitHints, ValidateEntry: numberUnitValidate},
			// File rules: where the families above may draw at all.
			{Key: "editor.conceal_include", Type: List, Title: "Conceal file include patterns", Description: "Restrict every conceal family (timestamps, number hints, secret masking, Markdown/CSV/log rendering, …) to the files matching one of these globs; empty means every file. A pattern without a separator matches the file's base name (\"*.py\", \"Makefile\"), one with a separator the whole path, anchored at any segment boundary unless it starts with / or ** (\"vendor/**\", \"**/config/**\"); matching is case-insensitive. Composes with the per-family toggles — a stand-in draws only if its family is on and the file passes the filter — and an explicit per-view toggle still wins", Scope: config.UserScope},
			{Key: "editor.conceal_exclude", Type: List, Title: "Conceal file exclude patterns", Description: "Switch every conceal family off in the files matching one of these globs, written like the include patterns (\"*.log\", \"**/testdata/**\"). Exclude beats include, so a file matching both conceals nothing", Scope: config.UserScope},
			{Key: "editor.conceal_file_rules", Type: List, Title: "Conceal file rules per family", Description: "Override the include/exclude lists for a single conceal family. Each entry is written \"family=pattern\", the pattern prefixed - (or !) for an exclude and bare (or +) for an include: \"secret_masking=-**/testdata/**\", \"cron_hints=*.log\". Families are the editor.* toggle keys without their prefix: markdown_rendering, csv_rendering, log_rendering, timestamp_decoding, unicode_escape_decoding, entity_decoding, base64_decoding, cron_hints, pem_summary, byte_size_hints, duration_hints, digit_grouping, radix_hints, permission_hints, cidr_hints, idn_hints, secret_masking. A family whose own rules decide a file never consults the global lists; one with nothing to say follows them", Scope: config.UserScope},
			// Coloring layers — no columns hidden, so the file rules do not gate them.
			{Key: "editor.rainbow_brackets", Type: Bool, Title: "Rainbow brackets", Description: "Color bracket pairs by nesting depth (theme-derived cycle); brackets left without a partner render underlined in the error color", Scope: config.UserScope},
			{Key: "editor.color_preview", Type: Bool, Title: "Color preview", Description: "Tint color literals (#rgb/#rrggbb/#rrggbbaa, rgb(), hsl()) with their own color", Scope: config.UserScope},
			{Key: "editor.id_colors", Type: Bool, Title: "Identifier colors", Description: "Color UUIDs and long hex hashes (git SHAs, request/trace IDs) by hashing the identifier into the rainbow palette, so every occurrence of the same identifier shares one color; active in logs, JSON and .http files and response bodies, toggle per view via Toggle Identifier Colors", Scope: config.UserScope},
			{Key: "editor.id_color_min_length", Type: Int, Title: "Identifier color minimum length", Description: "Shortest bare hex run that counts as an identifier (7 = abbreviated git SHA)", Scope: config.UserScope, Min: 6, Max: 64},
		}},
		{Title: "Typing Assistance", Description: "What the editor writes for you while you type. Each aid is one switch; which characters (and which templates) it applies to is the language's convention, not a setting.", Entries: []Entry{
			{Key: "editor.typing.space_after_punctuation", Type: Bool, Title: "Space after punctuation", Description: "Insert the conventional space after punctuation the language declares — \":\" in JSON, so \"key\": is completed as you type. Suppressed inside strings and comments", Scope: config.UserScope},
			{Key: "editor.postfix_completion", Type: Bool, Title: "Postfix completion", Description: "Offer the JetBrains-style postfix templates in the completion popup after a dot: \"err.nil\" completes to \"if err == nil { … }\", \"foo(bar).if\" wraps the whole call, \"xs.range\" writes a range loop. Accepting one replaces the whole expr.template span and places the caret inside. The templates are the language's (Go and Python ship one set each); languages without any are unaffected, and the items always rank below the language server's members on the same dot", Scope: config.UserScope},
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
			{Key: "editor.marks.coverage", Type: Bool, Title: "Coverage marks", Description: "Show per-line test-coverage bars in the gutter after a run-with-coverage (green covered, red uncovered, yellow partial); the coverage.toggle command hides them per session on top of this", Scope: config.UserScope},
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
			{Key: "lsp.code_lens", Type: Bool, Title: "Code lenses", Description: "Show server code lenses (\"run test\", reference counts) as annotations on the anchored line; the LSP: Run Code Lens command executes them", Scope: config.UserScope},
			{Key: "lsp.folding", Type: Bool, Title: "Server folding ranges", Description: "Fold along the server's folding ranges (import blocks, comments, regions) where available; Tree-sitter folding remains the fallback either way", Scope: config.UserScope},
			{Key: "lsp.semantic_tokens", Type: Bool, Title: "Semantic highlighting", Description: "Refine Tree-sitter syntax colors with server semantic tokens (parameters vs locals, constants vs mutable variables)", Scope: config.UserScope},
			{Key: "lsp.selection_range", Type: Bool, Title: "Server selection ranges", Description: "Base Extend/Shrink Selection on the server's syntactic ranges; Tree-sitter ranges are used when off or unsupported", Scope: config.UserScope},
			{Key: "lsp.will_rename", Type: Bool, Title: "Refactor on file rename", Description: "Ask language servers for refactoring edits (updated import paths) before a rename or move in the explorer and apply them", Scope: config.UserScope},
			{Key: "lsp.log_level", Type: Enum, Title: "Server log level", Description: "Verbosity of the language-server log the LSP: Show Server Log command opens", Scope: config.UserScope, Options: []string{"error", "warn", "info", "debug"}},
		}},
		{Title: "Appearance", Description: "How IKE looks: the color scheme, the menu bar, the horizontal scroll marks, the pane-number badges and the size of centered popups.", Entries: []Entry{
			{Key: "theme.name", Type: Enum, Title: "Theme", Description: "Color scheme; previews live while you scroll the list, esc restores the previous one (and selecting turns off auto sync)", Scope: config.UserScope, Options: themes, Preview: swatch, RowColors: rowColors},
			{Key: "theme.auto", Type: Bool, Title: "Sync with terminal", Description: "Pick the light or dark theme below from the terminal's background colour (OSC 11); picking a theme explicitly turns this off", Scope: config.UserScope},
			{Key: "theme.light", Type: Enum, Title: "Light theme", Description: "Theme used when auto sync sees a light terminal background", Scope: config.UserScope, Options: lightThemes, Preview: swatch, RowColors: rowColors},
			{Key: "theme.dark", Type: Enum, Title: "Dark theme", Description: "Theme used when auto sync sees a dark terminal background", Scope: config.UserScope, Options: darkThemes, Preview: swatch, RowColors: rowColors},
			{Key: "ui.menu_bar", Type: Bool, Title: "Menu bar", Description: "Show the File/Edit/… menu row above the panes", Scope: config.UserScope},
			{Key: "ui.h_scroll_marks", Type: Bool, Title: "Horizontal scroll marks", Description: "Mark the edges of a horizontally scrolled view \u2014 \u2039 where content continues left, \u203a where a line continues right \u2014 in the editor, diff, explorer and playground result", Scope: config.UserScope},
			{Key: "layout.pane_numbers", Type: Enum, Title: "Pane numbers", Description: "Draw each visible pane's layout-order number in its title bar (\"[1] EDITOR\"), the number the pane-focus chords (ctrl+1…ctrl+9 on macOS) and Focus Pane by Number address: \"on\" always, \"off\" never, \"focus-only\" only for a moment after a pane switch, when the numbers are actually being used", Scope: config.UserScope, Options: []string{"on", "off", "focus-only"}},
			{Key: "ui.popup_max_width", Type: Int, Title: "Popup max width", Description: "Cap centered popups (palette, dialogs, settings) at this width in columns; 0 disables", Scope: config.UserScope},
			{Key: "palette.toggle_key", Type: Chord, Title: "Command palette key", Description: "Chord that opens the command palette", Scope: config.UserScope},
		}},
		{Title: "Command Palette", Description: "The command palette overlay: how many rows it shows, which mode a query without a prefix rune lands in, and what happens to commands belonging to another pane.", Entries: []Entry{
			{Key: "palette.max_results", Type: Int, Title: "Result rows", Description: "Result rows shown at once; the list still scrolls past it", Scope: config.UserScope, Min: 1, Max: 100},
			{Key: "palette.default_mode", Type: Enum, Title: "Default mode", Description: "Prefix assumed when the query starts with no mode rune: \":\" ranks it as a command, \"@\" as a file name", Scope: config.UserScope, Options: []string{":", "@"}},
			{Key: "palette.off_context", Type: Enum, Title: "Off-context commands", Description: "How command mode treats commands scoped to a pane other than the focused one: rank them last, or hide them", Scope: config.UserScope, Options: []string{"rank", "hide"}},
			{Key: "palette.recent.ranking", Type: Enum, Title: "Recent files ranking", Description: "How the recent-files dialog (cmd+e) orders its files and projects: \"frecency\" blends how often and how recently each was opened, \"recency\" keeps the plain most-recently-used order", Scope: config.UserScope, Options: []string{"frecency", "recency"}},
		}},
		{Title: "Keymap Hints", Description: "The which-key popup: while a multi-step chord waits for its next key, a small panel above the status line lists every continuation valid in the focused pane. The bindings themselves are edited on the Keymap page.", Entries: []Entry{
			{Key: "keymap.which_key", Type: Bool, Title: "Which-key hints", Description: "Show the continuation popup while a chord prefix is pending (cmd+k …); off leaves multi-step sequences unannounced", Scope: config.UserScope},
			{Key: "keymap.which_key_delay_ms", Type: Int, Title: "Which-key delay", Description: "Milliseconds a chord prefix must stay pending before the popup opens; a sequence finished faster never flashes one. 0 shows it immediately", Scope: config.UserScope, Min: 0, Max: 5000},
		}},
		{Title: "Files & Session", Description: "Opening, watching and restoring files: which project comes back on start, how external changes are handled and when a file counts as too large for language features.", Entries: []Entry{
			{Key: "project.restore_last", Type: Bool, Title: "Restore last project", Description: "Reopen the previous project's workspace on start", Scope: config.UserScope},
			{Key: "project.directory", Type: Path, Dirs: true, Title: "Project directory", Description: "Default parent for projects IKE creates itself (clone repository); a leading ~ is expanded and the directory is created on first use", Scope: config.UserScope},
			{Key: "project.auto_save_on_switch", Type: Bool, Title: "Auto-save on project switch", Description: "Write every dirty file-backed buffer of the departing project before switching projects (format-on-save and the other save hooks run as on a normal save); buffers that cannot be written — untitled, read-only, changed on disk — are collected into one dialog. Off parks dirty buffers unsaved with their workspace", Scope: config.UserScope},
			{Key: "project.max_history", Type: Int, Title: "Recent projects kept", Description: "How many entries the recent-projects list keeps; the oldest fall off", Scope: config.UserScope, Min: 0, Max: 200},
			{Key: "project.max_workspaces", Type: Int, Title: "Background workspaces", Description: "Live background workspaces kept across seamless project switches; exceeding it evicts the least recently used one (confirming first when unsaved buffers or running processes would die). 0 selects the built-in default of 3", Scope: config.UserScope, Min: 0, Max: 20},
			{Key: "project.background_lsp_timeout", Type: String, Title: "Background LSP timeout", Description: "How long a parked background workspace keeps its language servers alive, as a Go duration (\"5m\", \"90s\"); past it they stop and respawn lazily on resume. Empty selects 5m, \"off\" keeps them running", Scope: config.UserScope},
			{Key: "project.find_all.include", Type: List, Title: "Find All: include globs", Description: "Default file-name include globs for Find in All Projects (project.findInAllProjects); empty keeps every file. The search form pre-fills and updates this list", Scope: config.UserScope, ValidateEntry: globValidate},
			{Key: "project.find_all.exclude", Type: List, Title: "Find All: exclude globs", Description: "Default file-name exclude globs for Find in All Projects; exclude wins over include. The search form pre-fills and updates this list", Scope: config.UserScope, ValidateEntry: globValidate},
			{Key: "project.find_all.excluded_roots", Type: List, Title: "Find All: excluded projects", Description: "Project roots (absolute paths) left out of Find in All Projects; the search form's project list toggles entries in and out", Scope: config.UserScope},
			{Key: "project.find_all.max_results", Type: Int, Title: "Find All: result cap", Description: "Bound on the merged match count across all searched projects; hitting it stops the scan and flags the results as truncated", Scope: config.UserScope, Min: 1, Max: 100000},
			{Key: "files.watch", Type: Bool, Title: "Watch files", Description: "Report external file changes (fsnotify on the project root)", Scope: config.UserScope},
			{Key: "files.auto_reload", Type: Enum, Title: "Auto reload", Description: "Reload clean buffers when their file changes on disk", Scope: config.UserScope, Options: []string{"clean", "never"}},
			{Key: "files.persistent_undo", Type: Bool, Title: "Persistent undo", Description: "Keep undo history across restarts while the file is unchanged", Scope: config.UserScope},
			{Key: "files.binary_open", Type: Enum, Title: "Open binary files as", Description: "Where a sniffed binary file (NUL byte in the first 8 KiB, no dedicated viewer) opens: the hex viewer pane, or a text buffer with code insight off. The Open File As… chooser overrides it per open", Scope: config.UserScope, Options: []string{"hex", "editor"}},
			{Key: "files.large_file_kb", Type: Int, Title: "Large file threshold (KB)", Description: "Above this size, highlighting and language features are disabled for the file (#149); 0 disables the size guard. Applies to subsequently opened or reloaded files", Scope: config.UserScope, Min: 0},
			{Key: "files.change_feed_limit", Type: Int, Title: "External-change feed size", Description: "How many externally changed files the change feed (watch.changeFeed) keeps for the session, oldest dropped first; 0 turns the feed off. It records writes by other processes — a coding agent, a git checkout, a formatter in a tool pane — never IKE's own saves", Scope: config.UserScope, Min: 0, Max: 5000},
			{Key: "files.large_file_lines", Type: Int, Title: "Large file threshold (lines)", Description: "Above this line count, highlighting and language features are disabled for the file (#149); 0 disables the line guard. Applies to subsequently opened or reloaded files", Scope: config.UserScope, Min: 0},
			{Key: "files.large_file_highlight_kb", Type: Int, Title: "Highlighting off above (KB)", Description: "Switch syntax highlighting (and its lint/Unicode scan) off once the file exceeds this size, before the base large-file cliff; 0 follows the base thresholds. The status line badges the degradation", Scope: config.UserScope, Min: 0},
			{Key: "files.large_file_lsp_kb", Type: Int, Title: "LSP sync off above (KB)", Description: "Stop syncing the document to language servers (didOpen/didChange) once the file exceeds this size; 0 follows the base thresholds. The status line badges the degradation", Scope: config.UserScope, Min: 0},
			{Key: "files.large_file_vcs_kb", Type: Int, Title: "VCS gutter off above (KB)", Description: "Skip the gutter diff-marker recompute (git show + whole-file diff per refresh) once the file exceeds this size; 0 follows the base thresholds. The status line badges the degradation", Scope: config.UserScope, Min: 0},
			{Key: "files.large_file_search_kb", Type: Int, Title: "Search counter off above (KB)", Description: "Hide the search match counter (a bounded buffer scan per edit while highlights are armed) once the file exceeds this size; 0 follows the base thresholds. Match highlighting in the viewport stays", Scope: config.UserScope, Min: 0},
			{Key: "files.large_file_format_kb", Type: Int, Title: "Format on save off above (KB)", Description: "Skip the on-save LSP chain (organize imports, format) once the file exceeds this size; 0 follows the base thresholds. The save itself is unaffected", Scope: config.UserScope, Min: 0},
		}},
		{Title: "Backup", Description: "Crash recovery: dirty buffers are snapshotted while you type and offered back after an unclean exit.", Entries: []Entry{
			{Key: "backup.enable", Type: Bool, Title: "Crash recovery", Description: "Snapshot dirty buffers for recovery; off also purges existing snapshots", Scope: config.UserScope},
			{Key: "backup.debounce_ms", Type: Int, Title: "Snapshot debounce", Description: "Milliseconds a dirty buffer must stay quiet before it is snapshotted", Scope: config.UserScope, Min: 100, Max: 60000},
			{Key: "backup.max_age_days", Type: Int, Title: "Snapshot max age", Description: "Days before leftover snapshots are pruned at startup (after the restore prompt)", Scope: config.UserScope, Min: 1, Max: 365},
		}},
		{Title: "Usage Telemetry", Description: "Local-only usage recording (#2235): structural events — which commands ran and how they were invoked, which chords resolved (or didn't), how panes and tabs are arranged — appended as JSONL under ~/.ike/telemetry for offline analysis with jq or scripts. Nothing ever leaves the machine and no event carries content: no typed text, no file contents, no clear-text paths.", Entries: []Entry{
			{Key: "telemetry.enabled", Type: Bool, Title: "Local usage telemetry", Description: "Record command, keybinding and layout usage events into per-session JSONL files under ~/.ike/telemetry; off writes nothing. The data stays on this machine", Scope: config.UserScope},
		}},
		{Title: "Timeline", Description: "The per-file Timeline (#1916): one chronological list of a file's local-history snapshots and the git commits that touched it, opened with Show Timeline.", Entries: []Entry{
			{Key: "history.timeline_source", Type: Enum, Title: "Timeline source filter", Description: "Which histories the Timeline shows when it opens: both local-history snapshots and git commits, snapshots only, or commits only. The view's f key cycles the filter for the open list without changing this default", Scope: config.UserScope, Options: []string{"both", "local", "git"}},
		}},
		{Title: "Terminal", Description: "The integrated terminal and what it offers while you type at the shell prompt.", Entries: []Entry{
			{Key: "terminal.shell", Type: Path, Title: "Shell", Description: "Program new terminal sessions spawn; empty follows $SHELL. Applies to sessions started after the change", Scope: config.UserScope},
			{Key: "terminal.autosuggest", Type: Bool, Title: "Command auto-suggest", Description: "Popup with command/path/make-target completions while typing at the shell prompt; ctrl+space opens it on demand either way", Scope: config.UserScope},
			{Key: "terminal.ssh_hosts", Type: List, Title: "Extra SSH hosts", Description: "Additional host aliases the SSH Host picker (terminal.ssh) offers, for machines no ~/.ssh/config entry declares. Each entry is passed to ssh verbatim (\"build01\", \"ops@10.0.0.5\"); the aliases parsed from ~/.ssh/config and its Include files are listed either way", Scope: config.UserScope, ValidateEntry: sshHostValidate},
			{Key: "terminal.scrollback_lines", Type: Int, Title: "Scrollback lines", Description: "Lines of scrollback each terminal session keeps (#1545); the main memory cost per terminal pane. Applies to new sessions and, on lowering, trims live ones forward — already-trimmed history is not restored by raising it", Scope: config.UserScope, Min: 100, Max: 1000000},
			{Key: "terminal.popup_cwd", Type: Enum, Title: "Popup terminal directory", Description: "Directory new popup-terminal shells start in: the project root, or the focused file's directory (project root when no file is open). Applies when a shell is spawned — the retained popup session keeps its own working directory across hide/show", Scope: config.UserScope, Options: []string{"project", "file"}},
			{Key: "terminal.popup_on_switch", Type: Enum, Title: "Popup terminal on project switch", Description: "What the popup terminal does after a project switch: \"restore\" brings the incoming project's popup back exactly as it was left, \"always-open\" opens it every time — resuming the parked instance if one exists, spawning a fresh shell otherwise", Scope: config.UserScope, Options: []string{"restore", "always-open"}},
		}},
		{Title: "Screenshots", Description: "The in-IDE PNG export (#2001): Export Screenshot paints the focused pane — or the whole window — as it is rendered, and copies the written path to the clipboard.", Entries: []Entry{
			{Key: "screenshot.directory", Type: Path, Dirs: true, Title: "Screenshot directory", Description: "Directory the exported PNGs are written to, created on the first capture; \"~\" expands and a relative path resolves against the project directory. Empty means the built-in default, ~/.ike/screenshots", Scope: config.UserScope},
		}},
		{Title: "Ansible Vault", Description: "Transparent Ansible Vault editing (#2293): a $ANSIBLE_VAULT; file opens decrypted into an editable buffer and is re-encrypted on save, so the plaintext never touches the disk. Needs a password source: the ANSIBLE_VAULT_PASSWORD or ANSIBLE_VAULT_PASSWORD_FILE environment variable, or the password file below.", Entries: []Entry{
			{Key: "ansible.vault_password_file", Type: Path, Title: "Vault password file", Description: "File whose first line is the Ansible Vault password; \"~\" expands. The user-scope value is the global default and a project-scope value overrides it; the ANSIBLE_VAULT_PASSWORD and ANSIBLE_VAULT_PASSWORD_FILE environment variables beat both. Empty (and no variable set) leaves vault files opening as ciphertext", Scope: config.UserScope},
		}},
		{Title: "Remote Browsing", Description: "The SFTP remote file browser (#1997): an SSH host from ~/.ssh/config browsed as a pane, remote files downloaded into a local cache and opened read-only.", Entries: []Entry{
			{Key: "remote.max_fetch_mb", Type: Int, Title: "Download size limit", Description: "Largest remote file the browser downloads into the local cache to preview, in MiB; opening a bigger file is refused with a notice instead of stalling the link", Scope: config.UserScope, Min: 1, Max: 4096},
		}},
		{Title: "Run", Description: "Where the Run tool — the dedicated pane every run's output goes to — opens.", Entries: []Entry{
			{Key: "run.placement", Type: Enum, Title: "Run placement", Description: "Home position of the Run tool pane: docked at the bottom, left, right or top workspace edge, or in_pane for a terminal tab in the focused editor pane. A [tools.layout] slot assigned to \"run\" overrides it; the legacy value new_terminal reads as bottom", Scope: config.UserScope, Options: []string{"bottom", "left", "right", "top", "in_pane"}},
			{Key: "run.vscode_launch", Type: Bool, Title: "Import launch.json", Description: "Merge compatible .vscode/launch.json launch configurations into the run-configuration picker (run.select); the .ike/runconfigs.json store wins name collisions and nothing is written back", Scope: config.UserScope},
		}},
		{Title: "Tests", Description: "The Test Results tool window (#1911): a structured tree of a test run's outcomes for languages with an output parser (Go, Python/pytest).", Entries: []Entry{
			{Key: "tests.results_window", Type: Bool, Title: "Structured test results", Description: "Parse test runs into the Test Results tool window (result tree, re-run failed, jump to failure) when the language declares an output parser; off keeps every test run in the raw Run tool terminal", Scope: config.UserScope},
			{Key: "tests.auto_open", Type: Bool, Title: "Open on test run", Description: "Open the Test Results tool window when a captured test run starts; off only updates an already open pane", Scope: config.UserScope},
			{Key: "tests.coverage_status", Type: Bool, Title: "Coverage in status line", Description: "Show the focused file's line-coverage percentage in the editor status line after a run with coverage (\"cov 82.4%\", marked \"stale\" once the buffer changed since the run); off keeps the figure to the Test Results window, whose detail column lists every covered file under c", Scope: config.UserScope},
		}},
		{Title: "Forge", Description: "The code forge behind the Issues tool window (GitHub through gh, Gitea/Forgejo through tea): how often IKE re-reads it in the background.", Entries: []Entry{
			{Key: "forge.poll_interval_seconds", Type: Int, Title: "Background poll interval", Description: "Seconds between background re-fetches of the repository's issues and pull requests, so new issues, closed issues and PR state changes surface without pressing r. The fetch runs off the UI loop and a tick arriving while the previous one is still running is skipped, so a slow forge never stalls IKE; consecutive failures back off exponentially (up to 5 minutes) and an unavailable forge — no CLI, no matching remote or login — stops polling until a manual refresh succeeds. 0 turns polling off entirely; the lowest interval is 10 seconds", Scope: config.UserScope, Min: 0, Max: config.ForgePollMaxSeconds, ValidateInt: forgePollValidate},
			{Key: "forge.cache", Type: Bool, Title: "Persistent listing cache", Description: "Keep the last successful issue/PR listing in the project's .ike/forgecache.json (#2108): a freshly started IKE shows it instantly, marked \"cached · updating…\" until the real fetch lands, and background polls only ask the forge for issues updated since the snapshot instead of re-listing everything. Manual r always performs a full resync; a repository or backend switch invalidates the snapshot; off never reads or writes the file", Scope: config.UserScope},
		}},
		{Title: "Diff Viewer", Description: "The diff pane (#60, 0340): how much unchanged context it keeps around a change, and whether it compares whitespace at all. Both are starting points — c/o expand context and w flips whitespace handling on the open diff; the toggle writes its new state back here.", Entries: []Entry{
			{Key: "diff.context", Type: Int, Title: "Context lines", Description: "Unchanged lines kept visible on each side of a change before the rest folds into a \"··· N unchanged lines ···\" separator; o expands the nearest fold and c shows the file in full. -1 never folds", Scope: config.UserScope, Min: -1, Max: 100},
			{Key: "diff.ignore_whitespace", Type: Bool, Title: "Ignore whitespace", Description: "Compare lines with every whitespace rune removed, the way \"git diff -w\" does: a re-indented or re-wrapped line counts as unchanged and drops out of the hunk list, and inside a line that really changed the emphasis marks only the non-whitespace ranges. Each column still shows its own raw text. w toggles it on the open diff and stores the choice here, so a reformat-heavy branch stays readable across panes", Scope: config.UserScope},
		}},
		{Title: "HTTP Client", Description: "The .http client's re-run and compare behaviour (#2247): what a response diff leaves out, and whether re-running a request opens the comparison with the run before it by itself — plus the response viewer's syntax-highlight cap (#2353) and when a finished dispatch reports itself as a notification (#2364).", Entries: []Entry{
			{Key: "http.diff_after_rerun", Type: Bool, Title: "Diff after re-run", Description: "Open the previous-vs-new response diff automatically once a re-run (R in the response pane, http.rerun) or a re-send (ctrl+r, http.resend) has landed in the history — the point of re-running a request is usually the comparison, so it needs no second key. A first-ever run has nothing to compare with and opens nothing; off keeps the pane on the new response, where P (\"Diff HTTP Response Against Previous Run\") still opens the same diff on demand", Scope: config.UserScope},
			{Key: "http.diff_ignore_headers", Type: List, Title: "Volatile diff headers", Description: "Response headers left out of every response diff: two runs of one request differ in Date, a freshly stamped request id and a handful of timing headers every single time, and those lines hide the one header that really changed. Each entry is a header name matched case-insensitively, optionally with a trailing * for a whole family (\"x-amz-*\"); a bare \"*\" is ignored, since an empty diff answers nothing. The notice above the diff says how many headers were filtered. Empty compares every header", Scope: config.UserScope, ValidateEntry: httpIgnoreHeaderValidate},
			{Key: "http.highlight_limit_kb", Type: Int, Title: "Highlight limit", Description: "Largest response body the viewer syntax-highlights, in KiB after pretty-printing. The pass runs off the update loop either way — the body always shows immediately and colours in when the parse lands — but a multi-megabyte search dump still costs CPU for colours nobody reads, so past this limit the body stays plain and a notice row in the pane says why", Scope: config.UserScope, Min: 1, Max: 65536},
			{Key: "http.notify_slow_ms", Type: Int, Title: "Slow response notice", Description: "Milliseconds a dispatch may take before its completion is announced as a notification while the response pane is not on screen. Dispatches are asynchronous and the answer usually lands after the focus has moved on, so a slow request would otherwise report itself only to whoever happens to look at the pane. The notice names method, status and duration and is recorded in the notification history like any other. 0 turns the slow branch off; a non-2xx status always notifies, threshold or not", Scope: config.UserScope, Min: 0, Max: 600000},
		}},
		{Title: "Scratch Files", Description: "The explorer's Scratches section (#1963): the scratch store listed below the file tree behind a divider, opened, renamed and deleted with the explorer's own keys and dialogs.", Entries: []Entry{
			{Key: "scratch.section", Type: Bool, Title: "Show Scratches section", Description: "List the scratch store as a divider-separated section at the bottom of the explorer; off removes the section entirely (scratches stay reachable through the \"Open Scratch File…\" command)", Scope: config.UserScope},
			{Key: "scratch.section_height", Type: Int, Title: "Scratches section height", Description: "Rows the Scratches section shows when expanded (it never grows past its content). Dragging the divider resizes it afterwards, and that height persists with the explorer's session state", Scope: config.UserScope, Min: 1, Max: 30},
			{Key: "scratch.sort", Type: Enum, Title: "Scratches sort order", Description: "How the Scratches section orders its rows: by name like the file tree, or by modification time newest first", Scope: config.UserScope, Options: []string{"name", "modified"}},
		}},
		{Title: "Issues Window", Description: "The forge Issues tool window (#2090): two full-area tabbed views — the issue list and the pull-request list — with a fuzzy filter, a label picker, an open/closed/all state filter, sort orders and a grouping toggle. These settings decide only what a freshly opened pane starts with; switching the tab, the order or a filter by hand wins for the rest of the session.", Entries: []Entry{
			{Key: "issues.default_tab", Type: Enum, Title: "Default view", Description: "Which of the pane's two views the issues window opens on: the issue list, or the pull-request list. tab and shift+tab (and a click on the tab bar) switch between them either way", Scope: config.UserScope, Options: []string{"issues", "prs"}},
			{Key: "issues.default_sort", Type: Enum, Title: "Default sort order", Description: "Order both lists open in: \"relevance\" ranks by fuzzy score while a filter pattern is typed and falls back to newest without one, \"newest\"/\"oldest\" order by creation time, \"updated\" by last activity, \"number\" by issue number ascending. The a key cycles the order for the open pane without changing this default", Scope: config.UserScope, Options: []string{"relevance", "newest", "oldest", "updated", "number"}},
			{Key: "issues.default_filter", Type: String, Title: "Default filter", Description: "Narrowing a freshly opened issues pane starts with, written in the same qualifier syntax the filter overlay's match input accepts, so an expression can be typed in the pane and pasted here unchanged: \"is:open\" (alias state:, also closed and all), \"label:bug\" repeated once per label (a name with spaces is double-quoted), \"sort:oldest\", and anything else as the fuzzy pattern — a bare word means \"match this\". A sort: qualifier overrides the default sort order. Every filter change in the pane wins for the rest of the session, so a live config reload never re-narrows a list you are working in. Empty opens the pane unfiltered", Scope: config.UserScope, ValidateString: issueFilterValidate},
			{Key: "issues.saved_filters", Type: List, Title: "Saved filters", Description: "Named filters the filter overlay's \"saved\" row cycles through, each written \"name=expression\" in the default filter's qualifier syntax (\"triage=is:open label:bug\", \"stale=is:all flaky\"). Picking one replaces the pane's state, labels and match text at once; picking \"(none)\" clears them again. Names must be unique, and neither name nor expression may contain a comma — the comma separates the entries of this list", Scope: config.UserScope, ValidateEntry: savedFilterValidate},
		}},
		{Title: "Tool Layout", Description: "Named slot template pinning tool windows to exact layout positions (#1897). The template is an ASCII grid: each row is one entry, every cell names a slot by a single letter, E is the editor region. Assigned tools always open at their slot; unassigned ones keep their home position or the adaptive split.", Entries: []Entry{
			{Key: "tools.layout.template", Type: List, Title: "Slot template rows", Description: "Grid rows, one entry per row (\"XEEH, XEEH, TTZZ\"): every cell names a slot by a single letter, E is the editor region, each slot's cells must form a solid rectangle, and row/column counts set the proportions. Empty disables slot placement; a template that cannot be split into straight cuts is rejected with a config diagnostic", Scope: config.UserScope, EntryHints: templateHints},
			{Key: "tools.layout.assign", Type: List, Title: "Slot assignments", Description: "SLOT=tool entries pinning tools to template slots (\"X=explorer, T=lazygit\"): the tool is a custom tool's name or a built-in id (explorer, vcs, debug, problems, structure, usages, http, breakpoints, run, tests, issues, dom, terminal). \"terminal\" pins fresh terminal panes (not the popup overlay); run and debug place independently. Several tools may share a slot — the first open materializes the pane, later ones join it as tabs; each tool takes at most one slot. While typing, the valid slot letters and tool ids are listed under the input", Scope: config.UserScope, EntryHints: assignHints, ValidateEntry: assignValidate},
		}},
		{Title: "Debug", Description: "Debugger transport settings. PHP debugging listens for incoming Xdebug connections; the hostname filter keeps foreign sessions out.", Entries: []Entry{
			{Key: "debug.inline_values", Type: Bool, Title: "Inline variable values", Description: "While the debugger is stopped, show the paused frame's local variable values at the end of the lines that mention them; they disappear on resume", Scope: config.UserScope},
			{Key: "debug.session_end", Type: Enum, Title: "When a session ends", Description: "What happens to the combined debug area (variables + console) when the session ends: keep leaves it open with the output reviewable until the next launch reuses it, close removes it from the layout", Scope: config.UserScope, Options: []string{"keep", "close"}},
			{Key: "debug.php.port", Type: Int, Title: "PHP listen port", Description: "DBGp port debug.listen binds for incoming Xdebug connections (Xdebug's default is 9003)", Scope: config.UserScope, Min: 1, Max: 65535},
			{Key: "debug.php.hostname", Type: String, Title: "PHP hostname filter", Description: "Only accept listen-mode debug sessions whose request HTTP_HOST matches (port suffix ignored); empty accepts all — per project", Scope: config.ProjectScope},
		}},
		{Title: "Performance HUD", Description: "The built-in performance HUD (#1999): a floating overlay with message rates, per-pane render cost, goroutines and memory, toggled with the Performance HUD command. It only measures while it is shown.", Entries: []Entry{
			{Key: "perf.hud_interval_ms", Type: Int, Title: "HUD refresh interval", Description: "Milliseconds between HUD samples. This is also the wake rate the open HUD costs the program, so a very short interval shows up in the numbers it reports; the HUD counts its own tick either way", Scope: config.UserScope, Min: 100, Max: 10000},
			{Key: "perf.hud_history_seconds", Type: Int, Title: "HUD history span", Description: "Seconds of rolling history behind the HUD's sparklines and the min/avg/max block the snapshot copies, so a spike stays readable after it passed", Scope: config.UserScope, Min: 5, Max: 600},
			{Key: "perf.watchdog_seconds", Type: Int, Title: "Stall watchdog threshold", Description: "Seconds a single Update/View pass may stay in flight before the stall watchdog dumps every goroutine's stack to the state directory (.ike, or IKE_CONFIG_DIR) and notes it in debug.log, so a frozen session leaves evidence of what it was stuck on. 0 disables the watchdog", Scope: config.UserScope, Min: 0, Max: 600},
			{Key: "perf.trace_log", Type: Bool, Title: "Update-loop trace log", Description: "While on, every message the update loop processes appends one line — the message type and the number of open HTTP requests, never any content — to trace.log in the state directory (.ike, or IKE_CONFIG_DIR), answering \"what is the IDE doing right now\" when chasing a freeze or a stall. Off (the default) it costs nothing; leave it off outside a diagnosis, the file grows with every keystroke", Scope: config.UserScope},
		}},
		{Title: "Notifications", Description: "Toasts and the notification history: how long they stay and which severities are worth interrupting for.", Entries: []Entry{
			{Key: "notifications.timeout_seconds", Type: Int, Title: "Notification timeout", Description: "Seconds before info/warn toasts expire", Scope: config.UserScope, Min: 1, Max: 300},
			{Key: "notifications.min_severity", Type: Enum, Title: "Notification severity floor", Description: "Below this severity notifications go to the history only", Scope: config.UserScope, Options: []string{"info", "warn", "error"}},
		}},
		{Title: "Forge Notifications", Description: "How prominently each kind of forge event (#2086) announces itself: dialog is a centered, dismissable dialog over the workspace, badge only marks the status line unread, toast is the ordinary bottom-right notice, off records the event in the notification history alone. While you are typing, a dialog is held back and shows as the badge instead.", Entries: []Entry{
			{Key: "forge.notify.issue_opened", Type: Enum, Title: "New issue", Description: "Notification style when an issue appears on the forge", Scope: config.UserScope, Options: forgeNotifyStyles},
			{Key: "forge.notify.issue_closed", Type: Enum, Title: "Issue closed", Description: "Notification style when an issue disappears from the open listing", Scope: config.UserScope, Options: forgeNotifyStyles},
			{Key: "forge.notify.pr_opened", Type: Enum, Title: "New pull request", Description: "Notification style when a pull request is opened", Scope: config.UserScope, Options: forgeNotifyStyles},
			{Key: "forge.notify.pr_merged", Type: Enum, Title: "Pull request merged", Description: "Notification style when a pull request is merged", Scope: config.UserScope, Options: forgeNotifyStyles},
			{Key: "forge.notify.pr_closed", Type: Enum, Title: "Pull request closed", Description: "Notification style when a pull request is closed without merging", Scope: config.UserScope, Options: forgeNotifyStyles},
			{Key: "forge.notify.pr_checks_failing", Type: Enum, Title: "Checks failing", Description: "Notification style when an open pull request's CI rollup turns failing", Scope: config.UserScope, Options: forgeNotifyStyles},
		}},
		{Title: "TODO Index", Description: "The project-wide comment-tag index behind the TODO tool window.", Entries: []Entry{
			{Key: "todo.patterns", Type: List, Title: "Tag words", Description: "Comment tag words the project scan matches as whole words, case-insensitively (TODO, FIXME, HACK, XXX); entries are literals, not regexes", Scope: config.UserScope},
		}},
		{Title: "Dependencies", Description: "The Dependencies tool window's project scan (#2419): declared dependencies per manifest with latest versions and vulnerabilities from the local toolchain.", Entries: []Entry{
			{Key: "deps.auto_scan", Type: Bool, Title: "Scan dependencies on project open", Description: "Run one background toolchain scan (outdated versions and vulnerability audit) when a project with dependency manifests opens (#2419); off leaves scans to the deps.refresh and deps.audit commands", Scope: config.UserScope},
		}},
		{Title: "Marketplace Catalog", Description: "Where the plugin marketplace fetches its catalog from. The Marketplace page installs from whatever this URL serves.", Entries: []Entry{
			{Key: "marketplace.catalog_url", Type: String, Title: "Catalog URL", Description: "HTTPS location of the marketplace index.json; empty falls back to the built-in default, which may itself be empty — then the marketplace stays disabled", Scope: config.UserScope},
			{Key: "marketplace.auto_check", Type: Bool, Title: "Check for plugin updates", Description: "Compare installed plugins against the catalog on IDE start and announce how many updates are available (#2257). The check runs in the background at most once a day and stays silent when the catalog cannot be reached; opening the Marketplace page always checks regardless", Scope: config.UserScope},
		}},
	}
}

// issueFilterValidate is the form check for issues.default_filter (#2115):
// the expression must parse, and the parser's message already names what is
// valid, so it is shown verbatim. An empty value is the built-in default —
// open the pane unfiltered.
func issueFilterValidate(v string) string {
	if strings.TrimSpace(v) == "" {
		return ""
	}
	if _, err := issuefilter.Parse(v); err != nil {
		return err.Error()
	}
	return ""
}

// globValidate is the form check for one glob of the all-projects search's
// include/exclude lists (#2394): a pattern filepath.Match cannot parse is
// refused here (strict, per the change workflow) where the config validator
// would only drop it with a diagnostic.
func globValidate(_ func(key string) string, text string) string {
	if _, err := filepath.Match(strings.TrimSpace(text), "x"); err != nil {
		return "invalid glob pattern"
	}
	return ""
}

// savedFilterValidate is the same check for one issues.saved_filters element,
// which carries a name in front of the expression. The lookup seam is unused —
// an entry stands on its own.
func savedFilterValidate(_ func(key string) string, text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	if _, _, err := issuefilter.ParseSaved(text); err != nil {
		return err.Error()
	}
	return ""
}

// httpIgnoreHeaderValidate is the form check for one entry of
// http.diff_ignore_headers (#2247). A header name has a grammar — RFC 9110
// tokens — and a value that cannot be one would silently never match; a lone
// "*" would hide every header instead, which is never what the reader of a
// diff wants. Both are refused here rather than dropped at read time.
func httpIgnoreHeaderValidate(_ func(key string) string, text string) string {
	name := strings.TrimSpace(text)
	switch {
	case name == "":
		return "header name must not be empty"
	case name == "*":
		return "\"*\" would hide every header; name the volatile ones"
	}
	for _, r := range strings.TrimSuffix(name, "*") {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("-_.~!#$%&'*+^`|", r):
		default:
			return "not a header name: " + name
		}
	}
	return ""
}

// forgePollValidate is the strict form check for forge.poll_interval_seconds
// (#2085): the config validator has to be lenient with a file on disk and
// snaps a too-small interval up to the floor, but a value typed here can be
// refused outright with the rule spelled out.
func forgePollValidate(v int) string {
	if v > 0 && v < config.ForgePollMinSeconds {
		return "0 disables polling; the lowest interval is " + strconv.Itoa(config.ForgePollMinSeconds) + "s"
	}
	return ""
}
