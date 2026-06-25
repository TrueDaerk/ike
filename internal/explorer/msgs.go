package explorer

// Msg is the marker interface implemented by every explorer-local message. The
// root model routes any value satisfying it to the explorer's Update, so the
// explorer's command set can grow without the root enumerating each type.
type Msg interface{ explorerMsg() }

// ToggleHiddenMsg flips hidden-file visibility (explorer.toggleHidden).
type ToggleHiddenMsg struct{}

// CollapseAllMsg folds the tree back to the root (explorer.collapseAll).
type CollapseAllMsg struct{}

// RefreshMsg invalidates and re-scans the selected subtree (explorer.refresh).
type RefreshMsg struct{}

// RevealMsg moves the cursor to the currently open file (explorer.reveal).
type RevealMsg struct{}

// NewFileMsg prompts for a name and creates an empty file next to the selection
// (explorer.newFile).
type NewFileMsg struct{}

// NewDirMsg prompts for a name and creates a directory next to the selection
// (explorer.newFolder).
type NewDirMsg struct{}

// DeleteMsg asks to delete the selected entry, after confirmation
// (explorer.delete).
type DeleteMsg struct{}

// UndoMsg reverses the last file operation, after confirmation: it deletes the
// last created entry or restores the last deleted one (explorer.undo).
type UndoMsg struct{}

// FileDeletedMsg announces that the explorer removed a path (a delete, or the
// undo of a create) so the root model can close any editor still showing it. It
// is handled by the app, not the explorer, so — unlike the messages above — it
// deliberately does not implement Msg.
type FileDeletedMsg struct {
	Path  string
	IsDir bool
}

func (ToggleHiddenMsg) explorerMsg() {}
func (CollapseAllMsg) explorerMsg()  {}
func (RefreshMsg) explorerMsg()      {}
func (RevealMsg) explorerMsg()       {}
func (NewFileMsg) explorerMsg()      {}
func (NewDirMsg) explorerMsg()       {}
func (DeleteMsg) explorerMsg()       {}
func (UndoMsg) explorerMsg()         {}
