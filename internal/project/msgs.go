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

// CloseWorkspaceMsg asks the root model to unload the background workspace at
// Path (#820): terminate its terminals/runs/debug sessions and free the
// memory, without switching to it. Emitted as the aux action of marked
// recent-projects entries; the history entry itself stays.
type CloseWorkspaceMsg struct{ Path string }

// UnsavedChangesMsg gates the switch on dirty editor buffers: the root model
// answers it with a save-all / discard / cancel prompt and only a confirming
// answer lets the switch to Root proceed.
type UnsavedChangesMsg struct{ Root string }
