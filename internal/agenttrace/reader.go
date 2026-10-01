package agenttrace

import (
	"bytes"
	"errors"
	"io"
	"os"
	"sync"
)

// Reader tails one transcript file. Every Update reads the bytes appended
// since the last one and feeds the complete lines to the Parser; a trailing
// line without its newline (the harness is still writing it, possibly in
// several writes) stays unread until a later Update finds it complete. A
// file that was replaced (renamed over, recreated), shrank (truncated) or
// rewritten in place so the byte before the read position is no longer a
// line end is parsed from the start again with a fresh Session (#2857).
//
// Update and Read are serialized by the Reader itself, so a read the host
// believes lost can never race the next one.
type Reader struct {
	mu     sync.Mutex
	path   string
	offset int64
	parser *Parser
	// file is the transcript the offset belongs to; another file under the
	// same path means it was replaced.
	file os.FileInfo
	// rev counts the Updates that changed the Session (and the resets).
	rev int
	// ReadFile is handed to the Parser for edit line resolution; nil means
	// os.ReadFile.
	ReadFile func(path string) ([]byte, error)
}

// NewReader returns a Reader positioned at the start of path. Nothing is read
// until Update.
func NewReader(path string) *Reader {
	r := &Reader{path: path}
	r.reset()
	return r
}

func (r *Reader) reset() {
	r.offset = 0
	r.parser = NewParser()
	r.parser.ReadFile = r.ReadFile
	r.rev++
}

// Revision changes whenever the Session does: an Update that added or
// completed events, or a restart from scratch. The host compares it with the
// revision it last showed instead of trusting a single read's count — a
// result it had to drop is then still shown by the next read.
func (r *Reader) Revision() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rev
}

// Read runs one Update and then view on the Session while no other read can
// touch it — the host builds its tree there, off the Update loop. It
// reports the Update's count and the revision after it.
func (r *Reader) Read(view func(*Session)) (added, rev int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	added, err = r.update()
	if view != nil {
		view(r.parser.Session())
	}
	return added, r.rev, err
}

// Path returns the transcript file being tailed.
func (r *Reader) Path() string { return r.path }

// Offset returns the byte position the next Update reads from.
func (r *Reader) Offset() int64 { return r.offset }

// Session returns the transcript parsed so far. The pointer stays valid
// across Updates; the Events slice grows in place.
func (r *Reader) Session() *Session { return r.parser.Session() }

// Update consumes the lines appended since the last call and reports how
// many events were added or completed. A missing file is not an error: the
// harness may not have written the first line yet.
func (r *Reader) Update() (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.update()
}

func (r *Reader) update() (int, error) {
	f, err := os.Open(r.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if r.replaced(f, st) {
		r.reset()
	}
	r.file = st
	if st.Size() == r.offset {
		return 0, nil
	}
	if _, err := f.Seek(r.offset, io.SeekStart); err != nil {
		return 0, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return 0, err
	}
	// Edits from an earlier batch may have moved lines; resolve against the
	// file as it is now, once per batch.
	r.parser.ReadFile = r.ReadFile
	r.parser.cache = map[string][]byte{}
	changed := 0
	for {
		nl := bytes.IndexByte(data, '\n')
		if nl < 0 {
			break
		}
		if r.parser.Line(data[:nl]) {
			changed++
		}
		r.offset += int64(nl + 1)
		data = data[nl+1:]
	}
	if changed > 0 {
		r.rev++
	}
	return changed, nil
}

// replaced reports whether the bytes already consumed no longer belong to
// the file at path: another file (rename over, delete + create), a shorter
// one (truncated), or one whose byte before the read position is not the
// line end the last read stopped behind (truncated and rewritten past the
// old size).
func (r *Reader) replaced(f *os.File, st os.FileInfo) bool {
	if r.offset == 0 {
		return false
	}
	if r.file != nil && !os.SameFile(r.file, st) {
		return true
	}
	if st.Size() < r.offset {
		return true
	}
	var b [1]byte
	if _, err := f.ReadAt(b[:], r.offset-1); err != nil || b[0] != '\n' {
		return true
	}
	return false
}
