package ui

// FindChord reports whether key is the shared find chord (#2409) — the
// muscle-memory alias for the bare "/" every searchable surface binds.
//
// Panes reached through the pane registry get the chord from the Global
// search.open command instead; this helper is for the surfaces that own the
// keyboard before the keymap layer runs (the settings panel, the TODO index
// overlay, a terminal in copy mode), which have to answer it themselves.
//
// Both spellings of the Command key are listed: bubbletea reports it as
// "super+" under some terminal protocols and the keymap layer's "cmd+" form
// reaches here through Model.String() under others.
func FindChord(key string) bool {
	switch key {
	case "cmd+f", "super+f", "ctrl+f":
		return true
	}
	return false
}
