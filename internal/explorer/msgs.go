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

func (ToggleHiddenMsg) explorerMsg() {}
func (CollapseAllMsg) explorerMsg()  {}
func (RefreshMsg) explorerMsg()      {}
func (RevealMsg) explorerMsg()       {}
