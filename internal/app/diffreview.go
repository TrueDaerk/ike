package app

// diffreview.go — review all changes against HEAD in one diff pane (#2848).
// `diff.reviewChanges` (the palette, the VCS panel's review action, or
// shift+enter on a panel row) opens the diff viewer in review mode over the
// ordered list of changed files — the rows the VCS panel shows, untracked
// files last — each compared working tree vs HEAD. F7 / shift+F7 step hunks
// across the file boundaries, `diff.nextFile` / `diff.prevFile` jump whole
// files, and the pane's `f` picker jumps directly. The pane model
// (internal/diff/review.go) owns the list and the position; this file is
// the owner side: it builds the list from the status snapshot, serves the
// load requests (the HEAD blob via git, the working tree from the open
// buffer or disk), skips what cannot be diffed, and keeps the list in step
// with the snapshot and the file watcher.

import (
	"os"
	"path/filepath"
	"sort"

	tea "charm.land/bubbletea/v2"

	"ike/internal/diff"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/vcs"
)

// ReviewChangesMsg runs diff.reviewChanges: open (or refocus) the review
// pane. Path, when set, is the repo-relative file the review starts on; ""
// starts on the first changed file.
type ReviewChangesMsg struct{ Path string }

// DiffFileStepMsg runs diff.nextFile / diff.prevFile on the focused review
// pane: Delta whole files.
type DiffFileStepMsg struct{ Delta int }

// reviewHeadMsg carries a loaded HEAD blob for review file Index of pane
// Key. Path names the file the request was for, so an answer that arrives
// after a list refresh moved the file is matched by path, not by index.
type reviewHeadMsg struct {
	Key   string
	Index int
	Path  string
	Head  string
	Err   error
}

// reviewFileList derives the review order from the status snapshot: every
// changed entry sorted by path (the VCS panel's rows), untracked files
// behind the tracked ones. Working-tree files that cannot be diffed —
// binary, or over the engine's input budget — keep their row as a
// placeholder, marked to be stepped over; a deleted file has no
// working-tree side to inspect and is judged when its HEAD blob loads.
func reviewFileList(snap *vcs.Snapshot) []diff.ReviewFile {
	if snap == nil {
		return nil
	}
	var tracked, untracked []diff.ReviewFile
	for _, e := range snap.Entries {
		f := diff.ReviewFile{
			Path: e.Path,
			Abs:  filepath.Join(snap.Root, filepath.FromSlash(e.Path)),
			Code: e.Code(),
		}
		f.Skip = reviewSkipReason(f.Abs)
		if e.Status == vcs.StatusUntracked {
			untracked = append(untracked, f)
		} else {
			tracked = append(tracked, f)
		}
	}
	byPath := func(files []diff.ReviewFile) {
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	}
	byPath(tracked)
	byPath(untracked)
	return append(tracked, untracked...)
}

// reviewSkipReason inspects a working-tree file and names why it cannot be
// diffed: "too large" over diff.MaxDiffBytes (a stat, no read), "binary"
// for a NUL in the leading window (the archive pane's test). "" for a
// diffable — or absent — file.
func reviewSkipReason(abs string) string {
	st, err := os.Stat(abs)
	if err != nil || st.IsDir() {
		return ""
	}
	if st.Size() > diff.MaxDiffBytes {
		return "too large"
	}
	f, err := os.Open(abs)
	if err != nil {
		return ""
	}
	defer f.Close()
	head := make([]byte, 8192)
	n, _ := f.Read(head)
	if isBinary(head[:n]) {
		return "binary"
	}
	return ""
}

// openReviewPane opens the diff viewer in review mode, positioned at
// startPath (or the first file). An existing review pane is refocused and
// re-listed rather than duplicated; otherwise the single diff slot is
// retargeted (#513) or a new viewer placed (#2507), like a HEAD diff.
func (m *Model) openReviewPane(startPath string) tea.Cmd {
	snap := m.vcs.snap
	if snap == nil {
		m.host.Notify(host.Info, "not a git repository")
		return nil
	}
	files := reviewFileList(snap)
	if len(files) == 0 {
		m.host.Notify(host.Info, "working tree clean — nothing to review")
		return nil
	}
	start := 0
	if startPath != "" {
		for i, f := range files {
			if f.Path == startPath {
				start = i
				break
			}
		}
	}
	if hostKey, tabIdx, inst, ok := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindDiff && c.Diff().Reviewing()
	}); ok {
		inst.StopDiffEdit()
		m.focusContentAt(hostKey, tabIdx)
		if startPath == "" {
			if cur, ok := inst.Diff().ReviewCurrent(); ok {
				// Re-running the command keeps the reader's place.
				start = reviewIndexOf(files, cur.Path, start)
			}
		}
		return inst.Diff().StartReview(files, start)
	}
	if inst, hostKey, tabIdx, ok := m.diffSlot(); ok {
		inst.StopDiffEdit()
		m.focusContentAt(hostKey, tabIdx)
		cmd := inst.Diff().StartReview(files, start)
		saveLayout(m.activeWS().Tree, m.activeWS().Panes)
		return cmd
	}
	inst, ok := m.openDiffLeaf(func() string { return m.activeWS().Panes.AddDiffHead(files[start].Abs) })
	if !ok {
		return nil
	}
	cmd := inst.Diff().StartReview(files, start)
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	return cmd
}

// reviewIndexOf returns the index of path in files, or fallback.
func reviewIndexOf(files []diff.ReviewFile, path string, fallback int) int {
	for i, f := range files {
		if f.Path == path {
			return i
		}
	}
	return fallback
}

// reviewPaneByKey locates the diff instance owning review pane key, wherever
// it lives (dedicated pane or content tab, #1778); nil when gone or no
// longer reviewing.
func (m Model) reviewPaneByKey(key string) *pane.Instance {
	_, _, inst, ok := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindDiff && c.Diff().Key() == key && c.Diff().Reviewing()
	})
	if !ok {
		return nil
	}
	return inst
}

// serveReviewLoad answers the pane's load request for one file: a
// placeholder is reported skipped straight away, an untracked file needs no
// git (its HEAD side is empty), everything else fetches the HEAD blob
// asynchronously and lands through reviewHeadMsg.
func (m *Model) serveReviewLoad(msg diff.ReviewLoadMsg) tea.Cmd {
	inst := m.reviewPaneByKey(msg.Key)
	if inst == nil {
		return nil
	}
	files := inst.Diff().ReviewFiles()
	if msg.Index < 0 || msg.Index >= len(files) {
		return nil
	}
	f := files[msg.Index]
	if f.Skip != "" {
		return inst.Diff().ReviewSkip(msg.Index, f.Skip)
	}
	if f.Code == "??" || m.vcs.snap == nil {
		m.landReviewFile(inst, msg.Index, "")
		return nil
	}
	root, key, idx, path := m.vcs.snap.Root, msg.Key, msg.Index, f.Path
	return func() tea.Msg {
		head, err := vcs.HeadContent(root, path)
		return reviewHeadMsg{Key: key, Index: idx, Path: path, Head: head, Err: err}
	}
}

// applyReviewHead lands a fetched HEAD blob. A blob git cannot show — an
// added file, the new name of a rename — is an empty HEAD side, not an
// error: the diff then reads as all-added, which is what it is.
func (m *Model) applyReviewHead(msg reviewHeadMsg) tea.Cmd {
	inst := m.reviewPaneByKey(msg.Key)
	if inst == nil {
		return nil
	}
	files := inst.Diff().ReviewFiles()
	idx := msg.Index
	if idx < 0 || idx >= len(files) || files[idx].Path != msg.Path {
		// A refresh moved the file while git ran: find it again, or drop
		// the answer when it became clean meanwhile.
		idx = reviewIndexOf(files, msg.Path, -1)
		if idx < 0 {
			return nil
		}
	}
	head := msg.Head
	if msg.Err != nil {
		head = ""
	}
	return m.landReviewFile(inst, idx, head)
}

// landReviewFile reads the working-tree side — the open buffer when the
// file is in an editor (unsaved edits included, like vcs.diff), else the
// file on disk; empty for a deleted file — and shows the pair, or reports
// the file skipped when the HEAD side turns out binary or oversized.
func (m *Model) landReviewFile(inst *pane.Instance, idx int, head string) tea.Cmd {
	d := inst.Diff()
	f := d.ReviewFiles()[idx]
	right := readFileOrEmpty(f.Abs)
	if ed := m.editorForPath(f.Abs); ed != nil {
		right = ed.Text()
	}
	switch {
	case diff.TooLarge(head, right):
		return d.ReviewSkip(idx, "too large")
	case isBinary([]byte(head)) || isBinary([]byte(right)):
		return d.ReviewSkip(idx, "binary")
	}
	inst.StopDiffEdit()
	d.ShowReviewFile(idx, head, right)
	return nil
}

// refreshReviewPanes re-derives every review pane's file list from a new
// status snapshot: files that became clean drop out, the position stays,
// and the file on screen re-diffs in place (HEAD may have moved).
func (m *Model) refreshReviewPanes(snap *vcs.Snapshot) []tea.Cmd {
	var cmds []tea.Cmd
	var files []diff.ReviewFile
	built := false
	m.contentInstances(func(_ string, _ int, c *pane.Instance) bool {
		if c.Kind() != pane.KindDiff || !c.Diff().Reviewing() {
			return true
		}
		if !built {
			files, built = reviewFileList(snap), true
		}
		if cmd := c.Diff().UpdateReviewFiles(append([]diff.ReviewFile(nil), files...)); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return true
	})
	return cmds
}

// reloadReviewForPath re-reads the working-tree side of every review pane
// showing path after a watcher event, re-diffing in place — the status
// refresh behind the same event may find the snapshot unchanged (the file
// was already "M") and refresh nothing. Edit mode is left alone, as for
// file diffs (#2506).
func (m *Model) reloadReviewForPath(path string) {
	abs := absDiffPath(path)
	m.contentInstances(func(_ string, _ int, c *pane.Instance) bool {
		if c.Kind() != pane.KindDiff || !c.Diff().Reviewing() || c.DiffEditor() != nil {
			return true
		}
		cur, ok := c.Diff().ReviewCurrent()
		if !ok || absDiffPath(cur.Abs) != abs {
			return true
		}
		c.Diff().ReloadRight(readFileOrEmpty(cur.Abs))
		return true
	})
}
