package explorer

// bulkops.go implements the explorer's selection-wide file operations
// (#2166): move and copy over the sticky multi-select (marks.go), plus the
// shared batching, partial-failure reporting and recursive copy they need.
// Delete lives in fileops.go next to the trash it uses; all three funnel
// through opTargets, so each one acts on the marks, else on an active
// shift+j/k range (#1044), else on the plain cursor entry.
//
// A bulk operation is best-effort per entry: one failing target does not
// abandon the rest. Everything that did succeed is recorded as ONE undo step
// and the failures are reported together in the error dialog, so the user
// sees exactly what happened instead of a half-done batch with a single
// opaque error.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// promptMove opens the target-directory prompt for the selection: one prompt
// for the whole batch, listing what will move. Accepting relocates every
// target into that directory, keeping its base name.
func (m *Model) promptMove() {
	m.promptRelocate("Move", func(mm *Model, ts []delTarget, dir string) tea.Cmd {
		return mm.moveTargets(ts, dir)
	})
}

// promptCopy is promptMove for a copy: the same single target-directory
// prompt, but the originals stay where they are.
func (m *Model) promptCopy() {
	m.promptRelocate("Copy", func(mm *Model, ts []delTarget, dir string) tea.Cmd {
		return mm.copyTargets(ts, dir)
	})
}

// promptRelocate is the shared shape of the move and copy prompts: resolve the
// targets once (so a rescan between prompt and accept cannot swap them out),
// title the box by verb and count, and list the entries underneath.
func (m *Model) promptRelocate(verb string, run func(*Model, []delTarget, string) tea.Cmd) {
	if m.inScratch() {
		return // the scratch store is flat and owns its own operations (#1963)
	}
	targets, bulk := m.opTargets()
	if len(targets) == 0 {
		return
	}
	title := fmt.Sprintf("%s %q to directory:", verb, filepath.Base(targets[0].path))
	var lines []string
	if bulk {
		title = fmt.Sprintf("%s %d %s to directory:", verb, len(targets), entryNoun(len(targets)))
		lines = m.targetLines(targets)
	}
	m.prompt = &prompt{
		kind:   promptInput,
		title:  title,
		lines:  lines,
		anchor: m.cursorPath(),
		accept: func(mm *Model, in string) tea.Cmd {
			return run(mm, targets, in)
		},
	}
}

// resolveDir turns a typed target-directory into an absolute path: an absolute
// input is taken as-is, anything else is read relative to the project root
// (the same convention as the palette's directory picker). An empty input has
// no target, so the operation is simply cancelled.
func (m Model) resolveDir(in string) (string, bool) {
	in = strings.TrimSpace(in)
	if in == "" {
		return "", false
	}
	p := filepath.FromSlash(in)
	if !filepath.IsAbs(p) {
		p = filepath.Join(m.root.path, p)
	}
	return filepath.Clean(p), true
}

// checkTargetDir resolves and validates the destination of a move/copy; on a
// bad target it opens the error dialog and reports false.
func (m *Model) checkTargetDir(in string) (string, bool) {
	dir, ok := m.resolveDir(in)
	if !ok {
		return "", false
	}
	info, err := os.Stat(dir)
	if err != nil {
		m.fail(err)
		return "", false
	}
	if !info.IsDir() {
		m.fail(fmt.Errorf("not a directory: %s", dir))
		return "", false
	}
	return dir, true
}

// pathTargets rebuilds targets from bare paths (the app's file.move picker
// hands back the multi-select as paths, #2166), reading each entry's kind from
// disk. Vanished paths are skipped — the batch acts on what still exists.
func (m Model) pathTargets(paths []string) []delTarget {
	var ts []delTarget
	for _, p := range paths {
		info, err := os.Lstat(p)
		if err != nil {
			continue
		}
		ts = append(ts, delTarget{path: p, isDir: info.IsDir()})
	}
	return ts
}

// moveTargets moves every target into dir. A single target keeps the existing
// one-entry path — including the LSP willRenameFiles round trip (#1912), which
// is inherently one-rename-at-a-time — so behaviour with nothing selected is
// unchanged; two or more are renamed directly and recorded as one batch.
func (m *Model) moveTargets(targets []delTarget, in string) tea.Cmd {
	dir, ok := m.checkTargetDir(in)
	if !ok || len(targets) == 0 {
		return nil
	}
	if len(targets) == 1 {
		t := targets[0]
		m.clearSel()
		m.clearMarks()
		return m.moveEntry(t.path, dir, t.isDir)
	}
	var subs []fileOp
	var cmds []tea.Cmd
	dirs := map[string]bool{}
	var errs []error
	for _, t := range targets {
		newPath := filepath.Join(dir, filepath.Base(t.path))
		if err := checkRelocate(t, newPath); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := os.Rename(t.path, newPath); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", filepath.Base(t.path), err))
			continue
		}
		subs = append(subs, fileOp{kind: opRename, path: t.path, newPath: newPath, isDir: t.isDir})
		dirs[filepath.Dir(t.path)] = true
		dirs[filepath.Dir(newPath)] = true
		cmds = append(cmds, movedCmd(t.path, newPath, t.isDir))
	}
	return m.finishBatch("moved", len(targets), subs, dirs, cmds, errs)
}

// copyTargets copies every target into dir, originals untouched. Each copy is
// recorded as an opCreate, so one undo trashes exactly the copies and leaves
// the sources alone.
func (m *Model) copyTargets(targets []delTarget, in string) tea.Cmd {
	dir, ok := m.checkTargetDir(in)
	if !ok || len(targets) == 0 {
		return nil
	}
	var subs []fileOp
	var cmds []tea.Cmd
	dirs := map[string]bool{}
	var errs []error
	for _, t := range targets {
		newPath := filepath.Join(dir, filepath.Base(t.path))
		if err := checkRelocate(t, newPath); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := copyTree(t.path, newPath); err != nil {
			// A half-written copy would be invisible garbage: remove it, so a
			// failed entry leaves the destination exactly as it was.
			_ = os.RemoveAll(newPath)
			errs = append(errs, fmt.Errorf("%s: %w", filepath.Base(t.path), err))
			continue
		}
		subs = append(subs, fileOp{kind: opCreate, path: newPath, isDir: t.isDir})
		dirs[dir] = true
		cmds = append(cmds, createdCmd(newPath, t.isDir))
	}
	return m.finishBatch("copied", len(targets), subs, dirs, cmds, errs)
}

// checkRelocate rejects the two destinations no move or copy may have: the
// entry's current location (a no-op that a plain rename would silently accept)
// and a path inside the directory being relocated (which would consume its own
// source).
func checkRelocate(t delTarget, newPath string) error {
	if newPath == t.path {
		return fmt.Errorf("%s: already there", filepath.Base(t.path))
	}
	if t.isDir && strings.HasPrefix(newPath, t.path+string(filepath.Separator)) {
		return fmt.Errorf("%s: cannot go into itself", filepath.Base(t.path))
	}
	if _, err := os.Lstat(newPath); err == nil {
		return fmt.Errorf("%s: already exists", filepath.Base(newPath))
	}
	return nil
}

// finishBatch is the common tail of a bulk operation: record what succeeded as
// one undo step, clear the selection (it never outlives the operation it was
// built for), re-scan the touched directories, emit the announcements, and —
// when some entries failed — open the partial-failure report over the result.
func (m *Model) finishBatch(verb string, total int, subs []fileOp, dirs map[string]bool, cmds []tea.Cmd, errs []error) tea.Cmd {
	m.clearSel()
	m.clearMarks()
	m.pushBatch(subs)
	if len(subs) > 0 {
		m.snapCursorTo(batchFocus(subs))
	}
	out := make([]tea.Cmd, 0, len(dirs)+len(cmds))
	for d := range dirs {
		out = append(out, m.refreshDir(d))
	}
	out = append(out, cmds...)
	if len(errs) > 0 {
		m.fail(batchErr(verb, len(subs), total, errs))
	}
	if len(out) == 0 {
		return nil
	}
	return tea.Batch(out...)
}

// batchFocus is the entry the cursor snaps to after a batch: the first
// sub-operation's resulting path — where a rename landed, or what a copy
// created.
func batchFocus(subs []fileOp) string {
	if subs[0].newPath != "" {
		return subs[0].newPath
	}
	return subs[0].path
}

// pushBatch records a bulk operation as a single undo step. A one-entry batch
// is pushed as the plain operation it is, so undo/redo of a degenerate batch
// behaves exactly like the single-entry command.
func (m *Model) pushBatch(subs []fileOp) {
	switch {
	case len(subs) == 1:
		m.pushOp(subs[0])
	case len(subs) > 1:
		m.pushOp(fileOp{kind: subs[0].kind, batch: subs})
	}
}

// batchErr is the partial-failure report (#2166): how much of the batch went
// through, followed by the per-entry reasons for the rest. Everything that did
// succeed stayed applied and stayed undoable.
func batchErr(verb string, done, total int, errs []error) error {
	reasons := make([]string, 0, len(errs))
	for _, e := range errs {
		reasons = append(reasons, e.Error())
	}
	if done == 0 {
		return errors.New(strings.Join(reasons, "; "))
	}
	return fmt.Errorf("%s %d of %d — %s", verb, done, total, strings.Join(reasons, "; "))
}

// createdCmd announces a path the explorer created without moving anything (a
// copy), so the app can refresh the git status snapshot.
func createdCmd(path string, isDir bool) tea.Cmd {
	return func() tea.Msg { return FileCreatedMsg{Path: path, IsDir: isDir} }
}

// copyTree copies src to dst recursively, preserving file modes. Symlinks are
// recreated as symlinks rather than followed, so a copied tree keeps its shape
// and a link loop cannot make the copy diverge.
func copyTree(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	case info.IsDir():
		if err := os.Mkdir(dst, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	default:
		return copyFile(src, dst, info.Mode().Perm())
	}
}

// copyFile writes src's contents to a freshly created dst. O_EXCL keeps the
// copy from clobbering a file that appeared since the existence check.
func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
