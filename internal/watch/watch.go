// Package watch is the external-file-change service (Roadmap 0140): an
// fsnotify watcher over the project root, recursive, debounced, that reports
// coalesced changes as tea.Msgs via the host's Send. IKE's own writes are
// suppressed through a save epoch per path (MarkSaved), so a save never
// round-trips as an external change. For filesystems where fsnotify
// under-reports (network mounts), Poll drives an mtime+size comparison over
// explicitly tracked files behind the same message shape.
package watch

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
)

// Kind classifies one filesystem event.
type Kind int

const (
	// FileChanged is a content change of a regular file.
	FileChanged Kind = iota
	// FileCreated is a new regular file.
	FileCreated
	// FileRemoved is a deleted (or renamed-away) regular file.
	FileRemoved
	// DirChanged is a directory whose entries changed (create/remove/rename
	// below it); the explorer refreshes from it.
	DirChanged
)

// EventMsg is one debounced, coalesced filesystem event. The root model routes
// file kinds to the editor leaf owning Path and DirChanged to the explorer.
type EventMsg struct {
	Kind Kind
	Path string
}

// debounceWindow coalesces bursts (editors write + rename, git checkouts).
const debounceWindow = 100 * time.Millisecond

// suppressWindow is how long after MarkSaved events for that path are treated
// as self-inflicted and dropped.
const suppressWindow = 500 * time.Millisecond

// Service owns the watcher goroutine and the debounce state.
type Service struct {
	send func(tea.Msg)

	mu      sync.Mutex
	epochs  map[string]time.Time // path -> last MarkSaved
	pending map[string]Kind      // debounced, coalesced events
	timer   *time.Timer
	tracked map[string]fileStamp // poll fallback set
	w       *fsnotify.Watcher
	root    string

	debounce time.Duration
	now      func() time.Time // injectable clock for tests
}

// fileStamp is the poll comparison state for one tracked file.
type fileStamp struct {
	mtime time.Time
	size  int64
	hash  string
}

// New returns a stopped Service that reports events through send (typically
// host.Send, which is a no-op until the program runs).
func New(send func(tea.Msg)) *Service {
	return &Service{
		send:     send,
		epochs:   map[string]time.Time{},
		pending:  map[string]Kind{},
		tracked:  map[string]fileStamp{},
		debounce: debounceWindow,
		now:      time.Now,
	}
}

// Start begins watching root recursively (skipping .git). Idempotent per
// service: a running watcher is stopped first, which is also the project-switch
// (Roadmap 0090) restart path.
func (s *Service) Start(root string) error {
	s.Stop()
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.w = w
	s.root = root
	s.mu.Unlock()
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if d.Name() == ".git" {
			return filepath.SkipDir
		}
		_ = w.Add(path)
		return nil
	})
	go s.loop(w)
	return nil
}

// Stop ends the watcher goroutine. Safe on a stopped service.
func (s *Service) Stop() {
	s.mu.Lock()
	w := s.w
	s.w = nil
	s.mu.Unlock()
	if w != nil {
		_ = w.Close()
	}
}

// MarkSaved records that IKE itself just wrote path, so the watcher drops the
// resulting events instead of reporting a phantom external change.
func (s *Service) MarkSaved(path string) {
	abs := absPath(path)
	s.mu.Lock()
	s.epochs[abs] = s.now()
	_, isTracked := s.tracked[abs]
	s.mu.Unlock()
	if isTracked {
		s.Track(abs) // refresh the poll stamp so our own write never triggers
	}
}

// SavedRecently reports whether path's save epoch is inside the suppression
// window — i.e. an event for it right now would be treated as IKE's own write.
func (s *Service) SavedRecently(path string) bool {
	abs := absPath(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	at, ok := s.epochs[abs]
	return ok && s.now().Sub(at) < suppressWindow
}

// loop drains fsnotify until the watcher closes.
func (s *Service) loop(w *fsnotify.Watcher) {
	for {
		select {
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			s.ingest(ev)
		case _, ok := <-w.Errors:
			if !ok {
				return
			}
		}
	}
}

// ingest maps one raw fsnotify event onto the debounce state.
func (s *Service) ingest(ev fsnotify.Event) {
	path := ev.Name
	if strings.Contains(path, string(filepath.Separator)+".git"+string(filepath.Separator)) {
		return
	}
	switch {
	case ev.Has(fsnotify.Create):
		if st, err := os.Stat(path); err == nil && st.IsDir() {
			// A new directory: watch it and refresh its parent.
			s.mu.Lock()
			if s.w != nil {
				_ = s.w.Add(path)
			}
			s.mu.Unlock()
			s.note(filepath.Dir(path), DirChanged)
			return
		}
		s.note(path, FileCreated)
		s.note(filepath.Dir(path), DirChanged)
	case ev.Has(fsnotify.Remove), ev.Has(fsnotify.Rename):
		s.note(path, FileRemoved)
		s.note(filepath.Dir(path), DirChanged)
	case ev.Has(fsnotify.Write):
		s.note(path, FileChanged)
	}
}

// note records one coalesced event and (re)arms the debounce flush. It is the
// single entry point for raw events — fsnotify and the poll fallback share it.
func (s *Service) note(path string, kind Kind) {
	path = absPath(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	if kind != DirChanged && kind != FileRemoved {
		if at, ok := s.epochs[path]; ok && s.now().Sub(at) < suppressWindow {
			return // IKE's own save echoing back
		}
	}
	if old, ok := s.pending[path]; ok {
		kind = mergeKinds(old, kind)
	}
	s.pending[path] = kind
	if s.timer == nil {
		s.timer = time.AfterFunc(s.debounce, s.flush)
	} else {
		s.timer.Reset(s.debounce)
	}
}

// mergeKinds coalesces a new raw kind onto an existing pending one: removal
// wins, creation survives follow-up writes, everything else keeps the latest.
func mergeKinds(old, next Kind) Kind {
	switch {
	case next == FileRemoved || old == FileRemoved:
		return FileRemoved
	case old == FileCreated && next == FileChanged:
		return FileCreated
	}
	return next
}

// flush emits every pending event as one EventMsg per path.
func (s *Service) flush() {
	s.mu.Lock()
	batch := s.pending
	s.pending = map[string]Kind{}
	s.timer = nil
	send := s.send
	s.mu.Unlock()
	if send == nil {
		return
	}
	for path, kind := range batch {
		send(EventMsg{Kind: kind, Path: path})
	}
}

// absPath normalises a path for epoch/tracking lookups.
func absPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}
