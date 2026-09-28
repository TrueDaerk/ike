package highlight

import (
	"hash/maphash"
	"sync"

	"ike/internal/lang"
)

// segmentmemo.go memoizes the completion layer's segmentation of open
// buffers (#2770). Every insert-mode keystroke in a long HTML buffer used to
// segment the whole text three times over — the completion engine's
// effective-language lookup (Embedded), then the word and the symbol source
// (Segments) each on their own goroutine — at a full Tree-sitter parse of
// the host and every fragment apiece. The memo keys on the language and the
// text's content hash, so those three callers share one pass, and a
// concurrent caller waits for the pass in flight instead of starting its own.
//
// The memo holds the last few texts only: a keystroke storm cycles through
// versions, and each entry costs a copy of the text's spans. Project scans
// (Segments with an only filter, EmbeddedLangs) bypass it — they visit
// thousands of files once each and would only churn it.

// segmentMemoSize is the number of texts the memo keeps, most recent first.
const segmentMemoSize = 4

// segmentKey identifies a text: its language and its content, hashed. Two
// texts of the same length with the same 64-bit hash collide — the odds are
// negligible for this purpose, and the worst case is one stale segmentation.
type segmentKey struct {
	lang    string
	grammar lang.Grammar // identity, not id: a test re-registering an id gets a fresh entry
	hash    uint64
	size    int
}

// segmentEntry is one memoized pass, computed once under its own gate so
// concurrent callers for the same text share the result.
type segmentEntry struct {
	key  segmentKey
	once sync.Once
	seg  segmentation
}

// segmentMemoStore is the memo: a short MRU list under a mutex. Entries move
// to the front on a hit and the oldest one falls off the end.
type segmentMemoStore struct {
	mu      sync.Mutex
	entries []*segmentEntry
	seed    maphash.Seed
}

var segmentMemo = &segmentMemoStore{seed: maphash.MakeSeed()}

// keyFor hashes lines under l's id.
func (s *segmentMemoStore) keyFor(l lang.Language, lines []string) segmentKey {
	var h maphash.Hash
	h.SetSeed(s.seed)
	size := 0
	for _, line := range lines {
		h.WriteString(line)
		h.WriteByte('\n')
		size += len(line) + 1
	}
	return segmentKey{lang: l.ID, grammar: l.Grammar, hash: h.Sum64(), size: size}
}

// get returns the segmentation of lines under l, computing it on the first
// request for this text and answering every later one — or every concurrent
// one — from the same pass.
func (s *segmentMemoStore) get(l lang.Language, lines []string) segmentation {
	key := s.keyFor(l, lines)
	s.mu.Lock()
	var e *segmentEntry
	for i, c := range s.entries {
		if c.key == key {
			e = c
			// Move to the front: the most recently asked-for text is the
			// one the next source asks for again.
			copy(s.entries[1:i+1], s.entries[:i])
			s.entries[0] = e
			break
		}
	}
	if e == nil {
		e = &segmentEntry{key: key}
		s.entries = append([]*segmentEntry{e}, s.entries...)
		if len(s.entries) > segmentMemoSize {
			s.entries = s.entries[:segmentMemoSize]
		}
	}
	s.mu.Unlock()
	e.once.Do(func() { e.seg = buildSegments(l, lines, nil) })
	return e.seg
}

// reset empties the memo (tests).
func (s *segmentMemoStore) reset() {
	s.mu.Lock()
	s.entries = nil
	s.mu.Unlock()
}
