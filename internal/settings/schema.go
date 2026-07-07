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

// Page is one category: a titled list of entries.
type Page struct {
	Title   string
	Entries []Entry
}

// BasePages returns the built-in framework pages. The core pages (Editor,
// Appearance, Files & Session) land with #92; the framework ships with the
// interface/chrome page so the panel is functional on its own.
func BasePages() []Page {
	return []Page{
		{Title: "Interface", Entries: []Entry{
			{Key: "ui.menu_bar", Type: Bool, Title: "Menu bar", Description: "Show the File/Edit/… menu row above the panes", Scope: config.UserScope},
			{Key: "notifications.timeout_seconds", Type: Int, Title: "Notification timeout", Description: "Seconds before info/warn toasts expire", Scope: config.UserScope, Min: 1, Max: 300},
			{Key: "notifications.min_severity", Type: Enum, Title: "Notification severity floor", Description: "Below this severity notifications go to the history only", Scope: config.UserScope, Options: []string{"info", "warn", "error"}},
		}},
	}
}
