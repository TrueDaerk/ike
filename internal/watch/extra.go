package watch

// extra.go is the per-path watch API (#2506). The recursive watch covers the
// project root and nothing else, so a consumer interested in a single file
// *outside* it — the "diff two files" pane comparing /tmp/a.json against a
// project file — had no way to hear about that file at all. WatchPath
// registers one such path; its events arrive as the ordinary FileChanged /
// FileCreated / FileRemoved EventMsgs, through the same debounce and the same
// self-save suppression.
//
// The registration follows the file rather than its directory: on kqueue
// (macOS) adding a *directory* opens one file descriptor per entry in it, so
// watching /tmp — or a home directory — to hear about one file would cost
// thousands of descriptors. The parent directory is only watched while the
// file is *missing*, because a gone file cannot carry a watch and its
// re-creation is a directory event; the file watch comes back the moment it
// does. Events from such a directory watch are filtered down to the
// registered paths, so a shared /tmp never floods the app with its
// neighbours' churn.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fsnotify/fsnotify"
)

// WatchPath registers path for per-path watching, reference-counted: two
// consumers watching the same file each pair their call with UnwatchPath.
// A path the recursive root watch already covers is remembered but registers
// no second fsnotify watch — the root walk reports it already.
func (s *Service) WatchPath(path string) {
	abs := absPath(path)
	if abs == "" {
		return
	}
	s.mu.Lock()
	s.extras[abs]++
	s.mu.Unlock()
	s.armExtras()
}

// UnwatchPath drops one reference to path, removing its fsnotify
// registration once the last consumer is gone.
func (s *Service) UnwatchPath(path string) {
	abs := absPath(path)
	s.mu.Lock()
	if n := s.extras[abs]; n > 1 {
		s.extras[abs] = n - 1
	} else {
		delete(s.extras, abs)
	}
	s.mu.Unlock()
	s.armExtras()
}

// WatchedPaths returns the currently registered per-path watches, sorted.
// Exported state so a consumer's test can assert it leaks none.
func (s *Service) WatchedPaths() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.extras))
	for p := range s.extras {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// isExtra reports whether path is a registered per-path watch.
func (s *Service) isExtra(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.extras[path] > 0
}

// covered reports whether the recursive root watch already reports events for
// path — inside the root and not below a pruned directory. It is Ignored's
// judgement inverted, spelled out here rather than called: this runs on every
// raw fsnotify event, and Ignored would repeat the Abs+Rel pass this one
// already made. Both are absolute paths (the callers normalise).
func covered(root, path string) bool {
	if root == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	segs := strings.Split(filepath.ToSlash(rel), "/")
	for _, seg := range segs[:max(0, len(segs)-1)] { // the base name is a file, not a dir
		if seg == "" || seg == "." {
			continue
		}
		if skipWatchDir(seg) {
			return false
		}
	}
	return true
}

// ingestExtra maps one raw event delivered by a per-path registration. Only
// the registered paths pass: a directory watch held for a missing file also
// reports its neighbours, and /tmp's churn is nobody's external change. No
// DirChanged is noted either — the explorer has no such directory to refresh.
func (s *Service) ingestExtra(ev fsnotify.Event, path string) {
	if !s.isExtra(path) {
		return
	}
	switch {
	case ev.Has(fsnotify.Create):
		s.note(path, FileCreated)
	case ev.Has(fsnotify.Remove), ev.Has(fsnotify.Rename):
		// The watch died with the file (fsnotify drops it): forget the
		// registration so the next arm puts it back — on the parent
		// directory while the file stays gone, on the file itself once a
		// replace-in-place (write temp + rename) brings it back.
		s.mu.Lock()
		delete(s.extraFiles, path)
		s.mu.Unlock()
		s.note(path, FileRemoved)
	case ev.Has(fsnotify.Write):
		s.note(path, FileChanged)
	}
}

// armExtras reconciles the fsnotify registrations backing the per-path
// watches with what they currently need: the file itself while it exists, its
// parent directory while it does not, nothing at all while the recursive root
// watch already covers it. Idempotent — it runs after every registration
// change, after every debounce flush (which is where a removal's directory
// fallback and a re-creation's file watch land) and after a (re)Start.
func (s *Service) armExtras() {
	s.mu.Lock()
	if len(s.extras) == 0 && len(s.extraFiles) == 0 && len(s.extraDirs) == 0 {
		s.mu.Unlock()
		return
	}
	paths := make([]string, 0, len(s.extras))
	for p := range s.extras {
		paths = append(paths, p)
	}
	root := s.root
	s.mu.Unlock()

	// Stat off the lock: it is the one syscall in this path.
	wantFiles := make(map[string]bool, len(paths))
	wantDirs := map[string]bool{}
	for _, p := range paths {
		if covered(root, p) {
			continue
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			wantFiles[p] = true
		} else {
			wantDirs[filepath.Dir(p)] = true
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.w == nil {
		// Stopped: the closed watcher holds no registrations to reconcile.
		s.extraFiles, s.extraDirs = map[string]bool{}, map[string]bool{}
		return
	}
	for p := range s.extraFiles {
		if !wantFiles[p] {
			_ = s.w.Remove(p)
			delete(s.extraFiles, p)
		}
	}
	for d := range s.extraDirs {
		if !wantDirs[d] {
			_ = s.w.Remove(d)
			delete(s.extraDirs, d)
		}
	}
	for p := range wantFiles {
		if !s.extraFiles[p] {
			if err := s.w.Add(p); err == nil {
				s.extraFiles[p] = true
			}
		}
	}
	for d := range wantDirs {
		if !s.extraDirs[d] {
			if err := s.w.Add(d); err == nil {
				s.extraDirs[d] = true
			}
		}
	}
}
