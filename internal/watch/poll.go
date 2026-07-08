package watch

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
)

// poll.go is the fallback for filesystems where fsnotify under-reports
// (network mounts): the app Tracks the files it cares about (open buffers) and
// Poll compares mtime+size on demand — hashing on suspicion — emitting the same
// EventMsg shape through the shared debounce path.

// Track registers path for poll comparison, capturing its current stamp
// (mtime, size and content hash).
func (s *Service) Track(path string) {
	abs := absPath(path)
	stamp := stampOf(abs)
	s.mu.Lock()
	s.tracked[abs] = stamp
	s.mu.Unlock()
}

// Untrack removes path from poll comparison.
func (s *Service) Untrack(path string) {
	abs := absPath(path)
	s.mu.Lock()
	delete(s.tracked, abs)
	s.mu.Unlock()
}

// Poll compares every tracked file against its recorded stamp and reports
// changes/removals through the debounced event path. An mtime change with an
// unchanged size is treated as suspicion only: the content hash decides, so a
// bare touch never reports a phantom change.
func (s *Service) Poll() {
	s.mu.Lock()
	snapshot := make(map[string]fileStamp, len(s.tracked))
	for p, st := range s.tracked {
		snapshot[p] = st
	}
	s.mu.Unlock()

	for path, prev := range snapshot {
		st, err := os.Stat(path)
		if err != nil {
			s.note(path, FileRemoved)
			s.mu.Lock()
			delete(s.tracked, path)
			s.mu.Unlock()
			continue
		}
		if st.ModTime().Equal(prev.mtime) && st.Size() == prev.size {
			continue
		}
		cur := stampOf(path)
		s.mu.Lock()
		s.tracked[path] = cur
		s.mu.Unlock()
		if st.Size() != prev.size || cur.hash != prev.hash {
			s.note(path, FileChanged)
		}
	}
}

// stampOf captures the comparison state of path; a stat/read failure yields a
// zero stamp (so the next Poll reports the file as removed or changed).
func stampOf(path string) fileStamp {
	st, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{mtime: st.ModTime(), size: st.Size(), hash: hashOf(path)}
}

// hashOf returns the hex sha256 of path's content ("" on read failure).
func hashOf(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
