package vcs

import tea "charm.land/bubbletea/v2"

// MergeStagesMsg carries the three index stages of a conflicted file for the
// three-way merge view (#1478). Base is empty (with no error) when the file
// has no stage 1 — both sides added it independently.
type MergeStagesMsg struct {
	Path   string // absolute path
	Base   string // stage :1 — the merge base
	Ours   string // stage :2
	Theirs string // stage :3
	Err    error
}

// MergeStagesCmd loads the merge stages of a conflicted file from the index.
func MergeStagesCmd(root, path string) tea.Cmd {
	return func() tea.Msg {
		msg := MergeStagesMsg{Path: path}
		// A missing base stage is normal (both-added); missing ours/theirs
		// stages (delete/modify conflicts) are not supported by the view.
		msg.Base, _ = RevContent(root, ":1", path)
		var err error
		if msg.Ours, err = RevContent(root, ":2", path); err != nil {
			msg.Err = err
			return msg
		}
		if msg.Theirs, err = RevContent(root, ":3", path); err != nil {
			msg.Err = err
			return msg
		}
		return msg
	}
}
