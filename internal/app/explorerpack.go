package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"ike/internal/archive"
	"ike/internal/archview"
	"ike/internal/explorer"
	"ike/internal/gzfile"
	"ike/internal/host"
	"ike/internal/menu"
	"ike/internal/pane"
	"ike/internal/ui"
)

// explorerpack.go is the explorer's keyboard context menu and its archive
// actions (#2805):
//
//   - explorer.contextMenu (alt+enter) opens the right-click menu at the
//     cursor row, the selection kept as it is;
//   - explorer.extractHere / explorer.extractTo unpack an archive (through the
//     archive viewer's own pipeline — plan, cap, traversal and link checks,
//     overwrite guard) or a plain .gz (gzfile.Extract, the same cap);
//   - explorer.compressGzip / explorer.compressZip pack a file into
//     <name>.gz, or a directory or multi-selection into <name>.zip.
//
// The menu only offers what applies to the selection, and every action ends
// with the tree rescanned and the produced entry selected.

// ExplorerContextMenuMsg runs explorer.contextMenu.
type ExplorerContextMenuMsg struct{}

// ExplorerExtractHereMsg runs explorer.extractHere.
type ExplorerExtractHereMsg struct{}

// ExplorerExtractToMsg runs explorer.extractTo.
type ExplorerExtractToMsg struct{}

// ExplorerCompressGzipMsg runs explorer.compressGzip.
type ExplorerCompressGzipMsg struct{}

// ExplorerCompressZipMsg runs explorer.compressZip.
type ExplorerCompressZipMsg struct{}

// packKind classifies the explorer selection for the archive actions.
type packKind int

const (
	// packNone: nothing the archive actions apply to (a scratch, the root).
	packNone packKind = iota
	// packArchive: one file archive.Detect claims (zip, tar, tar.gz, …).
	packArchive
	// packGzip: one plain gzip file (gzfile.IsPlain).
	packGzip
	// packFile: one other regular file — gzip it.
	packFile
	// packTree: one directory or a multi-selection — zip it.
	packTree
)

// explorerPackTarget resolves the selection the archive actions act on and
// what kind of thing it is.
func (m Model) explorerPackTarget() ([]string, packKind) {
	paths, bulk := m.explorer().OpTargetPaths()
	if len(paths) == 0 {
		return nil, packNone
	}
	if bulk {
		return paths, packTree
	}
	return paths, classifyPack(paths[0])
}

// classifyPack decides which archive actions a single entry offers. The
// routing is the viewers' own: archive.Detect claims archives, gzfile.IsPlain
// the lone .gz files the archive viewer leaves alone.
func classifyPack(path string) packKind {
	st, err := os.Stat(path)
	switch {
	case err != nil:
		return packNone
	case st.IsDir():
		return packTree
	case !st.Mode().IsRegular():
		return packNone
	}
	head := readHead(path)
	switch {
	case archive.IsArchive(path, head):
		return packArchive
	case gzfile.IsPlain(path, head):
		return packGzip
	}
	return packFile
}

// explorerMenuItems is the node menu for the current selection: the fixed
// entries plus the archive actions that apply to it.
func (m Model) explorerMenuItems() []menu.Item {
	items := explorerContextItems()
	_, kind := m.explorerPackTarget()
	switch kind {
	case packArchive, packGzip:
		items = append(items,
			menu.Item{Title: "Extract Here", Command: "explorer.extractHere"},
			menu.Item{Title: "Extract To…", Command: "explorer.extractTo"})
	case packFile:
		items = append(items, menu.Item{Title: "Compress (gzip)", Command: "explorer.compressGzip"})
	case packTree:
		items = append(items, menu.Item{Title: "Compress (zip)", Command: "explorer.compressZip"})
	}
	return items
}

// openExplorerContextMenu is alt+enter on the tree: the right-click menu,
// anchored just below the cursor row so the row stays readable.
func (m *Model) openExplorerContextMenu() {
	r, ok := m.lay.Panes[pane.ExplorerKey]
	if !ok {
		return
	}
	exp := m.explorer()
	if exp.Prompting() || exp.Searching() {
		return
	}
	x, y, ok := exp.ContextRow()
	if !ok {
		return
	}
	m.ctxMenu.Open(m.explorerMenuItems(), r.X+paneContentX+x, r.Y+paneContentY+y+1, m.width, m.height)
}

// selectInExplorer is the refresh-and-select that closes every action.
func selectInExplorer(path string) tea.Cmd {
	return func() tea.Msg { return explorer.SelectPathMsg{Path: path} }
}

// explorerExtract runs Extract Here (to == false) or Extract To… on the
// selected archive or .gz.
func (m *Model) explorerExtract(to bool) tea.Cmd {
	paths, kind := m.explorerPackTarget()
	if kind != packArchive && kind != packGzip {
		m.host.Notify(host.Info, "extract: select an archive or a .gz file")
		return nil
	}
	src := paths[0]
	if to {
		dir := defaultExtractDir(src)
		if kind == packGzip {
			dir = filepath.Dir(src)
		}
		m.openArchiveExtractPrompt(src, nil, dir)
		m.archExtractReveal, m.archExtractGzip = true, kind == packGzip
		m.renderArchiveExtractPrompt()
		m.shell.SetSize(m.width, m.height)
		m.shell.Open()
		return nil
	}
	if kind == packGzip {
		return m.startGunzip(src, filepath.Join(filepath.Dir(src), gzfile.ExtractName(src)))
	}
	m.archExtractReveal = true
	return m.planArchiveExtract(src, nil, defaultExtractDir(src))
}

// packJob is one extraction or compression waiting to run: where it writes
// and the work itself, which runs off the event loop.
type packJob struct {
	verb string // "extract" or "compress", the notification prefix
	dest string
	// announce is the progress notice raised when the job starts ("" for
	// inputs small enough to finish before it could be read).
	announce string
	run      func() packDoneMsg
}

// packDoneMsg reports a finished job back to the event loop.
type packDoneMsg struct {
	verb, dest, summary string
	err                 error
}

// packAnnounceBytes is the input size from which a job announces itself: a
// smaller one is done before a notice could be read.
const packAnnounceBytes = 8 << 20

// startGunzip decompresses a plain .gz to dest under the extraction cap.
func (m *Model) startGunzip(src, dest string) tea.Cmd {
	limit := m.extractLimit()
	job := packJob{verb: "extract", dest: dest, run: func() packDoneMsg {
		// Extract To… accepts a directory that does not exist yet, as the
		// archive prompt does.
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return packDoneMsg{verb: "extract", dest: dest, err: err}
		}
		n, err := gzfile.Extract(src, dest, limit)
		if errors.Is(err, gzfile.ErrExtractTooLarge) {
			err = fmt.Errorf("refused — %s decompresses past the %s extraction cap",
				filepath.Base(src), archview.HumanSize(limit))
		}
		return packDoneMsg{verb: "extract", dest: dest, err: err,
			summary: fmt.Sprintf("extracted %s to %s", archview.HumanSize(n), displayPath(dest))}
	}}
	if st, err := os.Stat(src); err == nil && st.Size() >= packAnnounceBytes/8 {
		job.announce = "extract: decompressing " + filepath.Base(src) + "…"
	}
	return m.queuePack(job)
}

// explorerCompress runs Compress (gzip) (zip == false) or Compress (zip).
func (m *Model) explorerCompress(zip bool) tea.Cmd {
	paths, kind := m.explorerPackTarget()
	if !zip {
		if kind != packFile {
			m.host.Notify(host.Info, "compress: select a file that is not already compressed")
			return nil
		}
		src, dest := paths[0], paths[0]+".gz"
		job := packJob{verb: "compress", dest: dest, run: func() packDoneMsg {
			res, err := archive.WriteGzip(src, dest)
			return packDoneMsg{verb: "compress", dest: dest, err: err,
				summary: fmt.Sprintf("gzipped %s into %s", archview.HumanSize(res.Bytes), displayPath(dest))}
		}}
		if st, err := os.Stat(src); err == nil && st.Size() >= packAnnounceBytes {
			job.announce = "compress: gzipping " + filepath.Base(src) + "…"
		}
		return m.queuePack(job)
	}
	if kind != packTree {
		m.host.Notify(host.Info, "compress: select a directory or several entries to zip")
		return nil
	}
	dest := zipDest(paths)
	job := packJob{verb: "compress", dest: dest, run: func() packDoneMsg {
		res, err := archive.WriteZip(paths, dest)
		summary := fmt.Sprintf("zipped %d file(s), %s into %s",
			res.Files, archview.HumanSize(res.Bytes), displayPath(dest))
		if res.Skipped > 0 {
			summary += fmt.Sprintf(" — %d skipped (links, special files)", res.Skipped)
		}
		return packDoneMsg{verb: "compress", dest: dest, err: err, summary: summary}
	}}
	// A tree's size is unknown without walking it — the walk *is* the job —
	// so a directory always announces itself.
	job.announce = "compress: zipping into " + filepath.Base(dest) + "…"
	return m.queuePack(job)
}

// zipDest names the zip beside the selection: <dir>.zip for one directory,
// archive.zip for a multi-selection (there is no one name to borrow).
func zipDest(paths []string) string {
	if len(paths) == 1 {
		return paths[0] + ".zip"
	}
	return filepath.Join(filepath.Dir(paths[0]), "archive.zip")
}

// queuePack runs job, or asks first when its target exists. A directory in
// the way is refused outright: neither job replaces a directory.
func (m *Model) queuePack(job packJob) tea.Cmd {
	st, err := os.Lstat(job.dest)
	if err == nil && st.IsDir() {
		m.host.Notify(host.Error, job.verb+": "+displayPath(job.dest)+" is a directory")
		return nil
	}
	if err == nil {
		m.openPackGuard(job)
		return nil
	}
	return m.runPack(job)
}

// runPack starts job off the event loop.
func (m *Model) runPack(job packJob) tea.Cmd {
	if job.announce != "" {
		m.host.Notify(host.Info, job.announce)
	}
	return func() tea.Msg { return job.run() }
}

// finishPack reports a finished job and selects what it produced.
func (m *Model) finishPack(msg packDoneMsg) tea.Cmd {
	if msg.err != nil {
		m.host.Notify(host.Error, msg.verb+": failed — "+msg.err.Error())
		return nil
	}
	m.host.Notify(host.Info, msg.verb+": "+msg.summary)
	return tea.Batch(selectInExplorer(msg.dest), m.scheduleVCSRefresh())
}

// packGuardOpen reports whether the shell shows the pack overwrite guard.
func (m Model) packGuardOpen() bool { return m.packPending != nil && m.shell.IsOpen() }

// openPackGuard asks before an existing target is replaced. Keeping it is the
// primary answer, as in every other overwrite guard.
func (m *Model) openPackGuard(job packJob) {
	m.packPending = &job
	m.shell.SetContent(ui.ModelContent{
		Heading: "Target already exists",
		Body: func() string {
			return fmt.Sprintf("%s already exists.\n\n", displayPath(job.dest)) +
				guardLine("s", "keep it — write nothing", true) +
				guardLine("o", "overwrite it", false) +
				guardCancel("cancel — write nothing")
		},
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// updatePackGuard consumes every key while the guard is open; anything but
// the three answers is swallowed.
func (m Model) updatePackGuard(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	job := *m.packPending
	switch guardAnswer(msg, "s") {
	case "o":
		m.closePackGuard()
		return m, m.runPack(job)
	case "s", "esc":
		m.closePackGuard()
		m.host.Notify(host.Info, job.verb+": cancelled — "+displayPath(job.dest)+" kept")
	}
	return m, nil
}

// closePackGuard drops the guard state and the shell.
func (m *Model) closePackGuard() {
	m.packPending = nil
	m.shell.Close()
}
