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

// RenameMsg prompts for a new name for the selected entry
// (explorer.rename).
type RenameMsg struct{}

// RenamePathMsg renames the entry at Path to Name within its directory,
// without requiring it to be the explorer's selection — the app's file.rename
// command (shift+f6 with an editor focused, #175) targets the focused file.
type RenamePathMsg struct {
	Path string
	Name string
}

// MoveToMsg moves the entry at Path into TargetDir (file.move, f6, #175). The
// target comes from the palette's directory picker; the operation lands on the
// explorer's undo/redo stack like a rename.
type MoveToMsg struct {
	Path      string
	TargetDir string
}

// UndoMsg reverses the last file operation instantly: a create is moved to the
// trash, a delete is restored, a rename is renamed back (explorer.undo).
type UndoMsg struct{}

// RedoMsg re-applies the most recently undone file operation (explorer.redo).
type RedoMsg struct{}

// FileDeletedMsg announces that the explorer removed a path (a delete, or the
// undo of a create) so the root model can close any editor still showing it. It
// is handled by the app, not the explorer, so — unlike the messages above — it
// deliberately does not implement Msg.
type FileDeletedMsg struct {
	Path  string
	IsDir bool
}

// FileMovedMsg announces that a path changed location or name (a rename, a
// move, or their undo/redo) so the root model can re-point any editor still
// showing it — open buffers follow the path instead of closing (#175). Like
// FileDeletedMsg it is handled by the app and does not implement Msg.
type FileMovedMsg struct {
	Old   string
	New   string
	IsDir bool
}

func (ToggleHiddenMsg) explorerMsg() {}
func (CollapseAllMsg) explorerMsg()  {}
func (RefreshMsg) explorerMsg()      {}
func (RevealMsg) explorerMsg()       {}
func (NewFileMsg) explorerMsg()      {}
func (NewDirMsg) explorerMsg()       {}
func (DeleteMsg) explorerMsg()       {}
func (RenameMsg) explorerMsg()       {}
func (RenamePathMsg) explorerMsg()   {}
func (MoveToMsg) explorerMsg()       {}
func (UndoMsg) explorerMsg()         {}
func (RedoMsg) explorerMsg()         {}
