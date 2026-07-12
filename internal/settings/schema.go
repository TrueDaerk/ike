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
}

// BasePages returns the built-in core pages (#92). themes is the registry's
// theme-name list for the Appearance enum (live preview: writing theme.name
// hot-reloads through the normal pipeline). Pages grow as features land;
// schema entries live next to their feature's config keys, and every key must
// exist in the typed schema (guarded by the no-dead-keys test).
func BasePages(themes []string) []Page {
	return []Page{
		{Title: "Editor", Entries: []Entry{
			{Key: "editor.tab_width", Type: Int, Title: "Tab width", Description: "Columns per indentation step", Scope: config.UserScope, Min: 1, Max: 16},
			{Key: "editor.use_spaces", Type: Bool, Title: "Use spaces", Description: "Indent with spaces instead of tab characters", Scope: config.UserScope},
			{Key: "editor.auto_indent", Type: Bool, Title: "Auto indent", Description: "Carry the current line's indentation into new lines", Scope: config.UserScope},
			{Key: "editor.auto_save", Type: Enum, Title: "Auto save", Description: "Save a dirty buffer when focus leaves its pane", Scope: config.UserScope, Options: []string{"focus", "off"}},
			{Key: "editor.trim_trailing_whitespace", Type: Bool, Title: "Trim trailing whitespace", Description: "Strip line-end whitespace on save", Scope: config.UserScope},
			{Key: "editor.insert_final_newline", Type: Bool, Title: "Insert final newline", Description: "End every saved file with a newline", Scope: config.UserScope},
			{Key: "editor.editorconfig", Type: Bool, Title: "EditorConfig", Description: "Honour .editorconfig files per buffer", Scope: config.UserScope},
			{Key: "editor.line_numbers", Type: Bool, Title: "Line numbers", Description: "Show the line-number gutter", Scope: config.UserScope},
			{Key: "editor.relative_line_numbers", Type: Bool, Title: "Relative line numbers", Description: "Count gutter lines away from the cursor (vim-style)", Scope: config.UserScope},
			{Key: "editor.scroll_off", Type: Int, Title: "Scroll offset", Description: "Minimum lines kept visible above and below the cursor", Scope: config.UserScope, Min: 0, Max: 50},
			{Key: "editor.sticky_scroll", Type: Bool, Title: "Sticky scroll", Description: "Pin enclosing function/class header lines at the top while scrolling", Scope: config.UserScope},
			{Key: "editor.sticky_scroll_depth", Type: Int, Title: "Sticky scroll depth", Description: "Maximum number of nested header lines pinned at once", Scope: config.UserScope, Min: 1, Max: 10},
			{Key: "editor.wrap", Type: Bool, Title: "Soft wrap", Description: "Wrap long lines at the pane edge", Scope: config.UserScope},
			{Key: "editor.show_whitespace", Type: Enum, Title: "Show whitespace", Description: "Render spaces and tabs visibly", Scope: config.UserScope, Options: []string{"none", "trailing", "all"}},
			{Key: "editor.indent_guides", Type: Bool, Title: "Indent guides", Description: "Draw vertical lines at each indentation level", Scope: config.UserScope},
			{Key: "editor.tabs.always_show", Type: Bool, Title: "Always show tab bar", Description: "Render the pane's tab bar even with a single tab", Scope: config.UserScope},
		}},
		{Title: "Appearance", Entries: []Entry{
			{Key: "theme.name", Type: Enum, Title: "Theme", Description: "Color scheme; applies immediately on selection", Scope: config.UserScope, Options: themes},
			{Key: "ui.menu_bar", Type: Bool, Title: "Menu bar", Description: "Show the File/Edit/… menu row above the panes", Scope: config.UserScope},
			{Key: "palette.toggle_key", Type: Chord, Title: "Command palette key", Description: "Chord that opens the command palette", Scope: config.UserScope},
		}},
		{Title: "Files & Session", Entries: []Entry{
			{Key: "project.restore_last", Type: Bool, Title: "Restore last project", Description: "Reopen the previous project's workspace on start", Scope: config.UserScope},
			{Key: "files.watch", Type: Bool, Title: "Watch files", Description: "Report external file changes (fsnotify on the project root)", Scope: config.UserScope},
			{Key: "files.auto_reload", Type: Enum, Title: "Auto reload", Description: "Reload clean buffers when their file changes on disk", Scope: config.UserScope, Options: []string{"clean", "never"}},
			{Key: "files.persistent_undo", Type: Bool, Title: "Persistent undo", Description: "Keep undo history across restarts while the file is unchanged", Scope: config.UserScope},
		}},
		{Title: "Backup", Entries: []Entry{
			{Key: "backup.enable", Type: Bool, Title: "Crash recovery", Description: "Snapshot dirty buffers for recovery; off also purges existing snapshots", Scope: config.UserScope},
			{Key: "backup.debounce_ms", Type: Int, Title: "Snapshot debounce", Description: "Milliseconds a dirty buffer must stay quiet before it is snapshotted", Scope: config.UserScope, Min: 100, Max: 60000},
			{Key: "backup.max_age_days", Type: Int, Title: "Snapshot max age", Description: "Days before leftover snapshots are pruned at startup (after the restore prompt)", Scope: config.UserScope, Min: 1, Max: 365},
		}},
		{Title: "Notifications", Entries: []Entry{
			{Key: "notifications.timeout_seconds", Type: Int, Title: "Notification timeout", Description: "Seconds before info/warn toasts expire", Scope: config.UserScope, Min: 1, Max: 300},
			{Key: "notifications.min_severity", Type: Enum, Title: "Notification severity floor", Description: "Below this severity notifications go to the history only", Scope: config.UserScope, Options: []string{"info", "warn", "error"}},
		}},
	}
}
