package httphistory

// session.go is the in-memory side of the response history (#3115). An
// exchange whose request carries `# @no-log` must never reach the on-disk
// Store, yet everything that reads "the earlier runs of this request" — the
// re-run diff, the pane's h/l browsing, the `# @capture` values — has to keep
// seeing it for the rest of the session. Those readers merge a Session's
// entries with the Store's, so a no-log exchange behaves exactly like a logged
// one until the IDE exits, and then is gone.

import (
	"sort"
	"sync"
)

// Session keeps entries in memory only, per source file + request key, with
// the same MaxPerRequest ring the on-disk Store prunes to. The zero value is
// ready to use, and a nil *Session lists nothing.
type Session struct {
	mu      sync.Mutex
	entries map[string][]Entry
}

// NewSession returns an empty in-memory history.
func NewSession() *Session { return &Session{} }

func sessionKey(source, key string) string { return source + "\x00" + key }

// Append records one entry, newest first, pruning beyond MaxPerRequest. A
// spooled body (#2157) keeps its dispatcher temp-file path: it lives as long
// as the process does, which is exactly as long as a session entry.
func (s *Session) Append(source, key string, e Entry) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries == nil {
		s.entries = map[string][]Entry{}
	}
	k := sessionKey(source, key)
	list := append([]Entry{e}, s.entries[k]...)
	if len(list) > MaxPerRequest {
		list = list[:MaxPerRequest]
	}
	s.entries[k] = list
}

// List returns the session's entries for one request, newest first, as a
// copy the caller may keep.
func (s *Session) List(source, key string) []Entry {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.entries[sessionKey(source, key)]
	if len(list) == 0 {
		return nil
	}
	return append([]Entry(nil), list...)
}

// Merge interleaves stored and session entries of one request into one
// newest-first list, the order every history reader expects. Equal times keep
// the stored entry first, so the result is deterministic.
func Merge(stored, session []Entry) []Entry {
	if len(session) == 0 {
		return stored
	}
	if len(stored) == 0 {
		return session
	}
	out := make([]Entry, 0, len(stored)+len(session))
	out = append(out, stored...)
	out = append(out, session...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out
}
