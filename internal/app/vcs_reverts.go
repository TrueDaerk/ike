package app

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"ike/internal/fuzzy"
	"ike/internal/host"
	"ike/internal/palette"
	"ike/internal/vcs"
)

// vcs_reverts.go is the revert history behind vcs.undoRevert (#556): every
// vcs.revertFile snapshots the pre-revert content (vcs/revertlog.go); this
// picker lists the focused file's snapshots and re-applies the chosen one to
// the buffer as a single undo-tree change — dirty, undoable, saved only when
// the user says so.

// revertsPrefix selects the revert-history mode inside the palette; the root
// model only opens it locked, so the rune has no user-facing prefix story.
const revertsPrefix = '='

// UndoRevertMsg starts the vcs.undoRevert flow for the focused editor.
type UndoRevertMsg struct{}

// RestoreRevertMsg is emitted when a picker item is activated: re-apply
// snapshot Index of Path's revert log.
type RestoreRevertMsg struct {
	Path  string
	Index int
}

// revertsMode is the palette Mode listing the focused file's pre-revert
// snapshots; state is injected so the mode reads the shared vcs state
// without holding the model.
type revertsMode struct {
	list func() (string, []vcs.RevertSnapshot)
}

func newRevertsMode(list func() (string, []vcs.RevertSnapshot)) *revertsMode {
	return &revertsMode{list: list}
}

// Prefix implements palette.Mode.
func (r *revertsMode) Prefix() rune { return revertsPrefix }

// Placeholder implements palette.Mode.
func (r *revertsMode) Placeholder() string { return "Undo revert…" }

// Results implements palette.Mode: snapshots newest-first, fuzzy-matched on
// their timestamp label.
func (r *revertsMode) Results(query string, _ palette.Context) []palette.Item {
	path, snaps := r.list()
	var items []palette.Item
	for i, s := range snaps {
		title := s.At.Format("2006-01-02 15:04:05")
		res, ok := fuzzy.Match(query, title)
		if !ok {
			continue
		}
		detail := strconv.Itoa(s.Changed) + " changed lines when reverted"
		if s.Changed == 1 {
			detail = "1 changed line when reverted"
		}
		items = append(items, palette.Item{
			Title:  title,
			Detail: detail,
			Spans:  res.Positions,
			// Newest first regardless of fuzzy score: recency is the
			// signal here, not match quality over near-identical stamps.
			Score: len(snaps) - i,
			Msg:   RestoreRevertMsg{Path: path, Index: i},
		})
	}
	return items
}

// openRevertHistory validates the focused file, loads its snapshots onto the
// shared vcs state, and opens the picker.
func (m Model) openRevertHistory() (tea.Model, tea.Cmd) {
	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() {
		m.host.Notify(host.Info, "no file to restore")
		return m, nil
	}
	snaps := vcs.RevertSnapshots(ed.Path())
	if len(snaps) == 0 {
		m.host.Notify(host.Info, "no reverts recorded for this file")
		return m, nil
	}
	m.vcs.revertsPath = ed.Path()
	m.vcs.reverts = snaps
	m.palette.SetSize(m.width, m.height)
	m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, revertsPrefix)
	return m, nil
}

// restoreRevert lands the chosen snapshot in the file's buffer.
func (m Model) restoreRevert(msg RestoreRevertMsg) (tea.Model, tea.Cmd) {
	if msg.Path != m.vcs.revertsPath || msg.Index < 0 || msg.Index >= len(m.vcs.reverts) {
		return m, nil
	}
	snap := m.vcs.reverts[msg.Index]
	ed := m.editorForPath(msg.Path)
	if ed == nil {
		m.host.Notify(host.Info, "file is no longer open")
		return m, nil
	}
	if !ed.RestoreContent(snap.Content) {
		m.host.Notify(host.Info, "buffer already matches that version")
		return m, nil
	}
	m.host.Notify(host.Info, "restored pre-revert content of "+displayPath(msg.Path)+" — save to keep it")
	return m, m.vcsMarksCmd(ed)
}
