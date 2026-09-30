package agenttrace

import (
	"bytes"
	"errors"
	"io"
	"os"
)

// Reader tails one transcript file. Every Update reads the bytes appended
// since the last one and feeds the complete lines to the Parser; a trailing
// line without its newline (the harness is still writing it) stays unread
// until the next Update. A file that shrank (rotated, truncated) is parsed
// from the start again with a fresh Session.
type Reader struct {
	path   string
	offset int64
	parser *Parser
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
	if st.Size() < r.offset {
		r.reset()
	}
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
	return changed, nil
}
