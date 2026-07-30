package project

// msgs.go names the messages of the switch transaction (Roadmap 0090, #3).
// The switch is msg-driven end to end: this package validates and emits, the
// root model in internal/app routes and re-roots — no subsystem is mutated
// from here.

// SwitchProjectMsg carries a validated, absolute project root the IDE should
// re-root to. The root model turns it into the re-root sequence, gating on the
// unsaved-changes guard first when any editor buffer is dirty.
type SwitchProjectMsg struct{ Root string }

// SwitchedMsg reports a completed switch: the IDE is re-rooted at Root and the
// history has recorded the open.
type SwitchedMsg struct{ Root string }

// SwitchFailedMsg reports a switch that never started: Path failed validation
// (or the re-root itself failed). Err carries the actionable reason. The
// current project is untouched.
type SwitchFailedMsg struct {
	Path string
	Err  error
}

// CloseProjectMsg asks the root model to close the current project (#1355):
// tear the active workspace down and resume the most recently used background
// workspace; with no background workspace the request becomes an app quit.
// Dispatched by project.close.
type CloseProjectMsg struct{}

// CloseWorkspaceMsg asks the root model to unload the background workspace at
// Path (#820): terminate its terminals/runs/debug sessions and free the
// memory, without switching to it. Emitted as the aux action of marked
// recent-projects entries; the history entry itself stays.
type CloseWorkspaceMsg struct{ Path string }

// CloseAuxGlyph is the aux-zone glyph for close-workspace rows (#1418): "⏏"
// (unload, keep the entry) distinguishes them from the default removal "✕"
// on unloaded rows, wherever recent-projects entries render (the picker and
// the Recent Files dialog's projects column).
const CloseAuxGlyph = "⏏"

// RemoveFromHistoryMsg asks the root model to delete the history entry at
// Path (#842): the aux action of unloaded recent-projects entries. Only the
// persisted list changes — nothing is closed or switched.
type RemoveFromHistoryMsg struct{ Path string }

// RemovedFromHistoryMsg reports a RemoveFromHistoryCmd outcome (#842); on
// success the root model reloads the config so the open picker re-lists.
type RemovedFromHistoryMsg struct {
	Path string
	Err  error
}

// UnsavedChangesMsg gates the switch on dirty editor buffers: the root model
// answers it with a save-all / discard / cancel prompt and only a confirming
// answer lets the switch to Root proceed.
type UnsavedChangesMsg struct{ Root string }
