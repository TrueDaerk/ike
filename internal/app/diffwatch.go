package app

// diffwatch.go makes a file-vs-file diff follow its two files on disk
// (#2506). `diff.files` used to read both sides once and keep showing that
// snapshot forever: editing one side in another pane, a build regenerating an
// output file or a `git checkout` left the diff quietly stale until it was
// re-opened. Now the 0140 watcher's events (see watchroute.go) re-read the
// side that changed and hand it to the model's in-place re-diff, so scroll
// position, current hunk and expanded gaps survive.
//
// Only *file-backed* diffs reload. A HEAD/commit diff, a clipboard diff and a
// local-history diff are snapshots by definition — one side names no file at
// all — and keep their snapshot semantics.

import (
	"os"
	"path/filepath"

	"ike/internal/pane"
)

// syncDiffWatches reconciles the per-path watches the open file diffs need
// with the ones currently registered. It runs once per settled Update pass:
// with no file diff open (the overwhelmingly common case) it walks the
// content instances, finds nothing, and compares two empty sets.
//
// The watcher only covers the project root, so a diff between /tmp/a.json and
// a project file would never hear about the outside side without an explicit
// registration (watch.Service.WatchPath). Registering the in-root side too
// costs nothing — WatchPath records it but adds no second fsnotify watch when
// the recursive walk already covers it — and keeps this bookkeeping free of a
// second copy of the "is it inside the root" rule.
func (m *Model) syncDiffWatches() {
	if m.watcher == nil {
		return
	}
	want := m.diffWatchPaths()
	if len(want) == len(m.diffWatched) {
		same := true
		for p := range want {
			if !m.diffWatched[p] {
				same = false
				break
			}
		}
		if same {
			return
		}
	}
	for p := range m.diffWatched {
		if !want[p] {
			m.watcher.UnwatchPath(p)
		}
	}
	for p := range want {
		if !m.diffWatched[p] {
			m.watcher.WatchPath(p)
		}
	}
	m.diffWatched = want
}

// diffWatchPaths collects the absolute paths every open file diff is backed
// by. Nil when there is none, so the no-diff pass allocates nothing.
func (m Model) diffWatchPaths() map[string]bool {
	var want map[string]bool
	m.contentInstances(func(_ string, _ int, c *pane.Instance) bool {
		left, right, ok := fileDiffPaths(c)
		if !ok {
			return true
		}
		if want == nil {
			want = map[string]bool{}
		}
		want[left], want[right] = true, true
		return true
	})
	return want
}

// fileDiffPaths returns the two absolute paths a diff instance compares, and
// whether it is a live file-vs-file diff at all: both sides must name a file
// and neither may be pinned to a revision (a HEAD or commit blob is a
// snapshot, not something that changes under the reader).
func fileDiffPaths(c *pane.Instance) (left, right string, ok bool) {
	if c == nil || c.Kind() != pane.KindDiff {
		return "", "", false
	}
	d := c.Diff()
	if lr, rr := d.Revs(); lr != "" || rr != "" {
		return "", "", false
	}
	lp, rp := d.LeftPath(), d.RightPath()
	if lp == "" || rp == "" {
		return "", "", false
	}
	return absDiffPath(lp), absDiffPath(rp), true
}

// absDiffPath normalises a diff side's path the way the watcher normalises
// its event paths, so the two compare.
func absDiffPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

// reloadDiffsForPath re-reads every open file diff one of whose sides is
// path, and re-diffs it in place (#2506). Called from routeWatchEvent for
// each file event of a debounce flush — including a removal, which shows the
// side as empty plus a footer notice rather than an error dialog, and is
// undone by writing the file again.
//
// A diff in edit mode (#496) is skipped: its right column *is* a live editor
// buffer, which reloads through the editor's own external-change path
// (auto-reload when clean, the conflict guard when dirty); overwriting the
// model's text from disk here would fight it.
func (m *Model) reloadDiffsForPath(path string) {
	abs := absDiffPath(path)
	m.contentInstances(func(_ string, _ int, c *pane.Instance) bool {
		left, right, ok := fileDiffPaths(c)
		if !ok || (left != abs && right != abs) || c.DiffEditor() != nil {
			return true
		}
		d := c.Diff()
		d.ReloadContents(readFileOrEmpty(d.LeftPath()), readFileOrEmpty(d.RightPath()))
		d.SetNotice(diffRemovedNotice(left, right))
		return true
	})
}

// diffRemovedNotice is the diff footer's one-line report of a side that is
// gone from disk (#2506); "" while both files are there, which clears it.
func diffRemovedNotice(left, right string) string {
	l, r := !diffFileExists(left), !diffFileExists(right)
	switch {
	case l && r:
		return "left and right file removed"
	case l:
		return "left file removed"
	case r:
		return "right file removed"
	}
	return ""
}

// diffFileExists reports whether path names a readable file right now.
func diffFileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
