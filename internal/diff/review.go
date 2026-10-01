package diff

// review.go is the diff pane's review-changes mode (#2848): one pane walks
// every changed file of the working tree against HEAD, in order. The model
// holds the ordered file list and the position in it; what it does not know
// is how to read a file or a HEAD blob — that is the owner's business (the
// root model runs git), so every step across a file boundary is a request:
// the model returns a ReviewLoadMsg naming the file it wants, the owner
// loads both sides and hands them back through ShowReviewFile (or reports
// the file unreadable through ReviewSkip). Hunk stepping (F7 / shift+F7,
// n / N) continues into the next or previous file once the current file's
// last or first hunk is passed; diff.nextFile / diff.prevFile jump whole
// files, and the in-pane file picker ("f", a ui.LineSearch over the paths)
// jumps directly. A single-file diff has no list and keeps stopping at its
// ends.

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/ui"
)

// ReviewFile is one entry of the review-changes file list: a changed file
// of the working tree, in the order the list walks it.
type ReviewFile struct {
	// Path is the repo-relative, slash-separated path — the label the side
	// headers, the footer and the picker show.
	Path string
	// Abs is the absolute working-tree path: the right side's file, enter's
	// jump target, and what a watcher event is matched against.
	Abs string
	// Code is the porcelain badge ("M", "AM", "??", "D"), shown in the
	// picker beside the path.
	Code string
	// Skip is non-empty for a file stepping passes over, naming why:
	// "binary" or "too large". A skipped file keeps its row in the list as a
	// placeholder — the position counter stays honest — and shows an empty
	// diff with the reason in the footer when jumped to directly.
	Skip string
}

// ReviewLoadMsg asks the owner of diff pane Key to load review file Index:
// read the working-tree side (or the open buffer) and the HEAD blob, then
// call ShowReviewFile — or ReviewSkip when the file cannot be diffed.
type ReviewLoadMsg struct {
	Key   string
	Index int
}

// review is the mode's state, held behind a pointer so the value-receiver
// View copies share it like the search does; nil means a single-file diff.
type review struct {
	files []ReviewFile
	// idx is the file on screen, -1 before the first one landed.
	idx int
	// pending is the index of a requested but not yet shown file (-1 none),
	// and dir the direction of the step that asked for it: a forward step
	// lands on the file's first hunk, a backward one on its last.
	pending int
	dir     int
	// jump marks a pending request made by a direct jump (the start file,
	// the picker) rather than a step: a skipped file reached by a jump
	// shows its placeholder, one reached by a step is passed over.
	jump bool
	// picker is the in-pane file picker, nil while closed.
	picker *ui.LineSearch
}

// StartReview puts the pane into review mode over files, positioned at
// start (clamped), and returns the load request for that file. An empty
// list is accepted and leaves the pane as it was with the footer saying so.
func (m *Model) StartReview(files []ReviewFile, start int) tea.Cmd {
	m.rv = &review{files: files, idx: -1, pending: -1, dir: 1}
	m.closePicker()
	if len(files) == 0 {
		m.SetNotice("no changes to review")
		return nil
	}
	return m.jumpReviewFile(clamp(start, 0, len(files)-1))
}

// EndReview leaves review mode: the pane becomes the single-file diff of
// whatever it shows, stepping clamps at its ends again. Retargeting the
// pane to another comparison ends it implicitly.
func (m *Model) EndReview() { m.rv = nil }

// Reviewing reports whether the pane is in review-changes mode.
func (m Model) Reviewing() bool { return m.rv != nil }

// ReviewFiles returns the review file list (nil for a single-file diff).
func (m Model) ReviewFiles() []ReviewFile {
	if m.rv == nil {
		return nil
	}
	return m.rv.files
}

// ReviewIndex returns the index of the file on screen, -1 before the first
// one landed or outside review mode.
func (m Model) ReviewIndex() int {
	if m.rv == nil {
		return -1
	}
	return m.rv.idx
}

// ReviewCurrent returns the file on screen, ok=false when none is.
func (m Model) ReviewCurrent() (ReviewFile, bool) {
	if m.rv == nil || m.rv.idx < 0 || m.rv.idx >= len(m.rv.files) {
		return ReviewFile{}, false
	}
	return m.rv.files[m.rv.idx], true
}

// ReviewPending returns the index of the requested-but-unloaded file, -1
// when nothing is in flight.
func (m Model) ReviewPending() int {
	if m.rv == nil {
		return -1
	}
	return m.rv.pending
}

// UpdateReviewFiles replaces the file list after a VCS status change
// without losing the position: the file on screen stays current when it is
// still in the list (it is re-requested, so a moved HEAD re-diffs it in
// place); a file that became clean drops out and the one now at its index
// — the next in order — loads instead. An emptied list keeps the current
// diff on screen with a footer notice. Outside review mode it is a no-op.
func (m *Model) UpdateReviewFiles(files []ReviewFile) tea.Cmd {
	if m.rv == nil {
		return nil
	}
	cur, ok := m.ReviewCurrent()
	wasIdx := m.rv.idx
	m.rv.files = files
	if m.rv.picker != nil {
		m.recomputePicker()
	}
	if len(files) == 0 {
		m.rv.idx, m.rv.pending = -1, -1
		m.SetNotice("no changes left to review")
		return nil
	}
	m.SetNotice("")
	if ok {
		if i := reviewIndexOf(files, cur.Path); i >= 0 {
			m.rv.idx = i
			return m.requestReviewFile(i, 0)
		}
	}
	// The current file is gone (or nothing landed yet): take its successor,
	// the file now sitting where it was — the end of the list when it was
	// the last one. idx is reset so the landing is a fresh SetContents, not
	// an in-place reload of a diff that compared a different file.
	m.rv.idx = -1
	return m.requestReviewFile(clamp(wasIdx, 0, len(files)-1), 1)
}

// reviewIndexOf returns the index of path in files, -1 when absent.
func reviewIndexOf(files []ReviewFile, path string) int {
	for i, f := range files {
		if f.Path == path {
			return i
		}
	}
	return -1
}

// requestReviewFile records the pending file and returns its load request.
// dir is the landing rule for the hunk position (+1 first hunk, -1 last
// hunk, 0 keep the view where it is — an in-place refresh).
func (m *Model) requestReviewFile(idx, dir int) tea.Cmd {
	m.rv.pending, m.rv.dir, m.rv.jump = idx, dir, false
	key := m.key
	return func() tea.Msg { return ReviewLoadMsg{Key: key, Index: idx} }
}

// jumpReviewFile is requestReviewFile for a direct jump: it lands on the
// first hunk, and a skipped target shows its placeholder.
func (m *Model) jumpReviewFile(idx int) tea.Cmd {
	cmd := m.requestReviewFile(idx, 1)
	m.rv.jump = true
	return cmd
}

// ShowReviewFile lands a loaded file: the sides are re-labelled with the
// path and revision (`path @ HEAD` / `path (working tree)`), the pane
// retargets to the file so enter jumps into it, and the view lands on the
// first or last hunk according to the step that asked for it. A reload of
// the file already on screen keeps the scroll position and current hunk the
// way ReloadContents does. Out-of-range indices and stale answers (a list
// refresh moved the file) are ignored.
func (m *Model) ShowReviewFile(idx int, left, right string) {
	if m.rv == nil || idx < 0 || idx >= len(m.rv.files) {
		return
	}
	f := m.rv.files[idx]
	dir := 1
	if idx == m.rv.pending {
		dir = m.rv.dir
	}
	m.rv.pending = -1
	if idx == m.rv.idx && f.Abs == m.rightPath {
		// Same file, fresh contents: in place.
		m.ReloadContents(left, right)
		m.SetNotice(reviewSkipNotice(f))
		return
	}
	m.rv.idx = idx
	m.retargetReview(f)
	m.SetContents(left, right)
	m.SetNotice(reviewSkipNotice(f))
	switch n := len(m.res.Hunks); {
	case n == 0:
	case dir < 0:
		m.landOnHunk(n - 1)
	case dir > 0:
		m.landOnHunk(0)
	}
}

// landOnHunk scrolls hunk i into the anchor position and makes it the
// current hunk as if a step had placed it, so the next F7 / shift+F7 walks
// on from there (and past the file end, into the neighbour).
func (m *Model) landOnHunk(i int) {
	m.scrollToHunk(i)
	m.cur = i
	m.curStepped = true
}

// retargetReview points the pane at review file f: titles, labels, paths,
// revisions. The left side is the HEAD blob (no file behind it), the right
// the working tree, so the diff stays editable (#496) and persists as a
// plain HEAD diff of this file (#508).
func (m *Model) retargetReview(f ReviewFile) {
	name := filepath.Base(f.Path)
	rv := m.rv
	m.Retarget(f.Path+" @ HEAD", name, "", f.Abs, "HEAD", "", true)
	m.rv = rv // Retarget ends review mode; this retarget *is* the review
	m.SetSideLabels(f.Path+" @ HEAD", f.Path+" (working tree)")
}

// reviewSkipNotice is the footer notice a placeholder file shows ("" for a
// diffable file).
func reviewSkipNotice(f ReviewFile) string {
	if f.Skip == "" {
		return ""
	}
	return f.Path + ": " + f.Skip + " — skipped"
}

// ReviewSkip records that the owner could not diff file idx (binary or over
// the size budget, found out while loading) and continues the step that
// asked for it past the file. A direct jump (the start file, the picker)
// and a step with no further file in its direction show the placeholder
// for idx instead, so the request always lands somewhere visible.
func (m *Model) ReviewSkip(idx int, reason string) tea.Cmd {
	if m.rv == nil || idx < 0 || idx >= len(m.rv.files) {
		return nil
	}
	m.rv.files[idx].Skip = reason
	dir, jump := m.rv.dir, m.rv.jump
	if idx == m.rv.pending {
		m.rv.pending = -1
	}
	if dir != 0 && !jump {
		if next := m.nextReviewFile(idx, dir); next >= 0 {
			return m.requestReviewFile(next, dir)
		}
	}
	m.ShowReviewFile(idx, "", "")
	return nil
}

// nextReviewFile returns the index of the next diffable file from idx in
// direction dir (±1), -1 at the end of the list.
func (m Model) nextReviewFile(idx, dir int) int {
	for i := idx + dir; i >= 0 && i < len(m.rv.files); i += dir {
		if m.rv.files[i].Skip == "" {
			return i
		}
	}
	return -1
}

// StepReviewFile moves by delta whole files — the diff.nextFile /
// diff.prevFile commands — landing on the first hunk going forward and the
// last going back. Skipped files are passed over; the ends of the list
// stop. Outside review mode it is a no-op.
func (m *Model) StepReviewFile(delta int) tea.Cmd {
	if m.rv == nil || delta == 0 {
		return nil
	}
	from := m.rv.idx
	if m.rv.pending >= 0 {
		from = m.rv.pending
	}
	dir := 1
	if delta < 0 {
		dir = -1
	}
	next := m.nextReviewFile(from, dir)
	if next < 0 {
		return nil
	}
	return m.requestReviewFile(next, dir)
}

// crossReviewBoundary is stepHunk's hook: a step past the last hunk going
// forward (or the first going back) continues into the neighbouring file.
// It reports whether a request was made; a single-file diff never crosses.
func (m *Model) crossReviewBoundary(delta int) (tea.Cmd, bool) {
	if m.rv == nil || len(m.rv.files) == 0 {
		return nil, false
	}
	cmd := m.StepReviewFile(delta)
	return cmd, cmd != nil
}

// reviewFooter is the position part of the footer in review mode:
// `file i/n` ahead of the hunk readout, or the skip reason for a
// placeholder file.
func (m Model) reviewFooter() string {
	if m.rv == nil {
		return ""
	}
	n := len(m.rv.files)
	if n == 0 {
		return "no files"
	}
	cur := "–"
	if m.rv.idx >= 0 {
		cur = fmt.Sprintf("%d", m.rv.idx+1)
	}
	return fmt.Sprintf("file %s/%d", cur, n)
}

// openPicker opens the in-pane file picker: a ui.LineSearch over the file
// list, typed into on the footer row; enter jumps to the match under the
// cursor, ctrl+n / ctrl+p (and the arrows) move between matches, esc
// closes it. It starts with every file matching, cursor on the current one,
// so enter alone re-lands and the arrows browse the list.
func (m *Model) openPicker() {
	if m.rv == nil {
		return
	}
	if m.rv.picker == nil {
		m.rv.picker = &ui.LineSearch{Cur: -1}
	}
	m.rv.picker.Start()
	m.recomputePicker()
	if m.rv.idx >= 0 {
		m.rv.picker.Locate(m.rv.idx)
	}
}

// closePicker drops the picker.
func (m *Model) closePicker() {
	if m.rv != nil {
		m.rv.picker = nil
	}
}

// PickingFile reports whether the file picker holds the keyboard (tests).
func (m *Model) PickingFile() bool { return m.rv != nil && m.rv.picker != nil && m.rv.picker.Open }

// recomputePicker rebuilds the picker's match list: every file while the
// query is empty, else the smartcase substring matches over the paths. The
// cursor stays on the nearest surviving file (SetMatches), so typing never
// throws the selection around.
func (m *Model) recomputePicker() {
	p := m.rv.picker
	files := m.rv.files
	if p.Text == "" {
		all := make([]int, len(files))
		for i := range all {
			all[i] = i
		}
		p.SetMatches(all)
	} else {
		p.Recompute(len(files), func(i int) bool {
			return ui.SmartCaseContains(p.Text, files[i].Path)
		})
	}
}

// pickerKey feeds one key to the open picker. The match-stepping chords are
// the pane's own and run first; enter jumps to the current match and closes
// the picker, esc closes it, everything else edits the query.
func (m *Model) pickerKey(msg tea.KeyPressMsg) tea.Cmd {
	p := m.rv.picker
	switch msg.String() {
	case "ctrl+n", "down":
		p.Step(1)
		return nil
	case "ctrl+p", "up":
		p.Step(-1)
		return nil
	}
	_, changed, action := p.Key(msg)
	switch action {
	case ui.SearchCancel:
		m.closePicker()
	case ui.SearchAccept:
		idx, ok := p.Current()
		m.closePicker()
		if ok && idx >= 0 && idx < len(m.rv.files) {
			return m.jumpReviewFile(idx)
		}
	default:
		if changed {
			m.recomputePicker()
		}
	}
	return nil
}

// pickerLine renders the picker's footer row: the prompt with the query,
// and the match under the cursor with its badge and position, so the row
// says which file enter would open.
func (m Model) pickerLine(st styles) string {
	p := m.rv.picker
	line := " file: " + p.Field.View()
	if p.Text != "" && len(p.Matches) == 0 {
		return line + st.gutter.Render("  no matches")
	}
	if idx, ok := p.Current(); ok && idx >= 0 && idx < len(m.rv.files) {
		f := m.rv.files[idx]
		tail := fmt.Sprintf("  %d/%d  %s %s", p.Cur+1, len(p.Matches), f.Code, f.Path)
		if f.Skip != "" {
			tail += " (" + f.Skip + ")"
		}
		line += st.gutter.Render(strings.TrimRight(tail, " "))
	}
	return line
}
