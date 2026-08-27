package httpclient

// spool.go keeps large response bodies out of memory (#2157).
//
// Until now every dispatch answered with one `[]byte` holding the whole body,
// which the viewer then composed into rows, indexed for highlighting and
// scanned for folds — several derived copies of the same megabytes, all alive
// for as long as the response was on show, and five of them at once once the
// history ring filled up. A 10 MiB JSON answer made the pane stutter for
// seconds and never gave the memory back.
//
// A body larger than SpoolThreshold is therefore streamed to a **spool file**
// while it is being received. `Response.Body` then holds only the first
// SpoolThreshold bytes — the *head*, which is what the viewer renders — and
// `Response.SpoolPath` names the file holding the whole thing. Everything
// that genuinely needs all the bytes (a `# @capture` expression, the raw-body
// file save, a response diff) asks for them explicitly through FullBody or
// BodyReader, so the full copy exists for the length of one operation instead
// of for the length of the session.
//
// Spool files live in one per-process directory under the OS temp dir and are
// removed by CleanupSpool on exit. A crash leaves the directory behind, so
// creating ours also sweeps the ones older than spoolMaxAge — the same
// best-effort posture the history store takes.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// SpoolThreshold is how much of a response body is kept in memory. Above
	// it the body streams to a spool file and Response.Body holds this much:
	// the head the viewer renders, sized so an ordinary API answer (a few
	// hundred KiB) never touches the disk at all.
	SpoolThreshold = 1 << 20 // 1 MiB

	// spoolPrefix names the per-process spool directory inside the OS temp
	// dir. The prefix is what the stale sweep recognizes.
	spoolPrefix = "ike-http-spool-"

	// spoolMaxAge is how long a spool directory left behind by a crashed
	// process may survive before another process removes it.
	spoolMaxAge = 24 * time.Hour
)

var (
	spoolMu   sync.Mutex
	spoolRoot string
)

// spoolDir returns the process's spool directory, creating it on first use.
// Creating it also sweeps the leftovers of crashed processes.
func spoolDir() (string, error) {
	spoolMu.Lock()
	defer spoolMu.Unlock()
	if spoolRoot != "" {
		return spoolRoot, nil
	}
	sweepStaleSpools(os.TempDir(), time.Now())
	dir, err := os.MkdirTemp("", spoolPrefix+"*")
	if err != nil {
		return "", err
	}
	spoolRoot = dir
	return spoolRoot, nil
}

// CleanupSpool removes this process's spool directory and everything in it.
// Called from main on exit; safe to call when nothing was ever spooled.
func CleanupSpool() {
	spoolMu.Lock()
	defer spoolMu.Unlock()
	if spoolRoot == "" {
		return
	}
	_ = os.RemoveAll(spoolRoot)
	spoolRoot = ""
}

// sweepStaleSpools removes spool directories older than spoolMaxAge from tmp.
// A live process's directory is younger than that as long as it keeps writing
// — and even a long-idle one only loses files it no longer shows, since the
// viewer reads the head from memory. Best effort throughout: a temp directory
// we may not remove is not worth a diagnostic.
func sweepStaleSpools(tmp string, now time.Time) {
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), spoolPrefix) {
			continue
		}
		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) < spoolMaxAge {
			continue
		}
		_ = os.RemoveAll(filepath.Join(tmp, e.Name()))
	}
}

// bodySink collects a response body: the first SpoolThreshold bytes into
// memory, everything beyond into a spool file, and never more than max bytes
// in total (MaxBodyBytes — the existing truncation cap, unchanged).
//
// The spool file holds the *whole* body, head included, so it is a complete
// artifact on its own: "open as file" in the viewer opens the response, not
// its tail, and a save can stream straight from it.
type bodySink struct {
	head      []byte
	threshold int
	max       int

	file  *os.File
	path  string
	total int

	truncated bool
	// spoolErr records why spooling stopped (a full disk, a read-only temp
	// dir). The bytes that did not fit are dropped like a truncation — the
	// exchange itself succeeded and is worth showing.
	spoolErr error
}

// newBodySink returns a sink capped at max total bytes, spilling past
// threshold. A threshold at or above max means "never spool".
func newBodySink(threshold, max int) *bodySink {
	return &bodySink{threshold: threshold, max: max}
}

// Write makes the sink an io.Writer for the collect path. It always claims
// the whole chunk: bytes past the cap are dropped on purpose, and reporting a
// short write would make io.Copy fail an exchange that in fact succeeded.
func (s *bodySink) Write(p []byte) (int, error) {
	s.add(p)
	return len(p), nil
}

// add takes one received chunk and reports how many bytes were kept, which is
// short of len(p) exactly when the total cap was hit. The streaming path needs
// that number: the live view must show what was kept, not what arrived.
func (s *bodySink) add(p []byte) int {
	if s.truncated {
		return 0
	}
	if room := s.max - s.total; len(p) > room {
		p, s.truncated = p[:room], true
	}
	if len(p) == 0 {
		return 0
	}
	if inMem := s.threshold - len(s.head); inMem > 0 {
		n := min(inMem, len(p))
		s.head = append(s.head, p[:n]...)
	}
	if s.total+len(p) > s.threshold {
		s.spill(p)
	}
	s.total += len(p)
	return len(p)
}

// spill writes p (and, on the first call, the head that preceded it) to the
// spool file. A spool that cannot be created degrades to a truncation at the
// threshold: the head is still shown, with a warning saying why it ends there.
func (s *bodySink) spill(p []byte) {
	if s.spoolErr != nil {
		return
	}
	if s.file == nil {
		dir, err := spoolDir()
		if err != nil {
			s.spoolErr = err
			return
		}
		f, err := os.CreateTemp(dir, "body-*.bin")
		if err != nil {
			s.spoolErr = err
			return
		}
		s.file, s.path = f, f.Name()
		if _, err := s.file.Write(s.head); err != nil {
			s.fail(err)
			return
		}
	}
	// The head bytes of this very chunk are already in the file (they were
	// written above, or by an earlier chunk): only what sits past the
	// threshold is new.
	from := max(0, s.threshold-s.total)
	if from >= len(p) {
		return
	}
	if _, err := s.file.Write(p[from:]); err != nil {
		s.fail(err)
	}
}

// fail abandons the spool after a write error, dropping the partial file.
func (s *bodySink) fail(err error) {
	s.spoolErr = err
	if s.file != nil {
		_ = s.file.Close()
		_ = os.Remove(s.path)
		s.file, s.path = nil, ""
	}
}

// close finishes the spool file and returns the head, the spool path ("" when
// the body stayed in memory) and the total byte count.
func (s *bodySink) close() (head []byte, path string, total int) {
	if s.file != nil {
		if err := s.file.Close(); err != nil {
			s.fail(err)
		}
	}
	if s.spoolErr != nil {
		// Nothing beyond the head survived: report the body as head-sized so
		// no caller believes in bytes it cannot read.
		return s.head, "", len(s.head)
	}
	return s.head, s.path, s.total
}

// warnings describes what the sink had to give up: the total cap, or a spool
// that could not be written. Appended to the response's own warnings.
func (s *bodySink) warnings() []string {
	var out []string
	if s.spoolErr != nil {
		out = append(out, fmt.Sprintf("response body could not be spooled to disk (%v) — showing the first %d bytes", s.spoolErr, len(s.head)))
	}
	if s.truncated {
		out = append(out, fmt.Sprintf("response body exceeded %d bytes and was truncated", s.max))
	}
	return out
}

// Spooled reports whether the body lives in a file rather than fully in
// Response.Body (#2157).
func (r *Response) Spooled() bool { return r != nil && r.SpoolPath != "" }

// BodyBytes is the total size of the received body, spooled or not — what
// Response.Body would have been before #2157.
func (r *Response) BodyBytes() int {
	if r == nil {
		return 0
	}
	if r.BodySize > 0 {
		return r.BodySize
	}
	return len(r.Body)
}

// ErrSpoolGone reports that a spooled body's file is no longer readable — the
// process that wrote it exited, or the temp directory was cleaned. Callers
// fall back to the head rather than failing: a shortened answer beats none.
var ErrSpoolGone = errors.New("the spooled response body is no longer available")

// FullBody returns the whole body: Response.Body when it was small enough to
// keep, the spool file's contents otherwise. The result is a fresh slice the
// caller may hold for as long as it needs — and, for a spooled body, should
// let go of again.
func (r *Response) FullBody() ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	if !r.Spooled() {
		return r.Body, nil
	}
	data, err := os.ReadFile(r.SpoolPath)
	if err != nil {
		return r.Body, fmt.Errorf("%w: %v", ErrSpoolGone, err)
	}
	return data, nil
}

// BodyReader streams the whole body, so a copy to disk never holds it. The
// caller closes the returned reader.
func (r *Response) BodyReader() (io.ReadCloser, error) {
	if r == nil {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	if !r.Spooled() {
		return io.NopCloser(bytes.NewReader(r.Body)), nil
	}
	f, err := os.Open(r.SpoolPath)
	if err != nil {
		return io.NopCloser(bytes.NewReader(r.Body)), fmt.Errorf("%w: %v", ErrSpoolGone, err)
	}
	return f, nil
}

// BodyRange returns bytes [off, off+n) of the body — the viewer's "load more"
// (#2157), which grows what it renders one window at a time instead of
// pulling the whole spool back in. A range inside the head is served from
// memory; one past it reads exactly that window off the spool. A short read at
// the end of the body is not an error: the returned slice is simply shorter.
func (r *Response) BodyRange(off, n int) ([]byte, error) {
	if r == nil || off < 0 || n <= 0 {
		return nil, nil
	}
	if end := r.BodyBytes(); off >= end {
		return nil, nil
	} else if off+n > end {
		n = end - off
	}
	if !r.Spooled() {
		if off >= len(r.Body) {
			return nil, nil
		}
		return r.Body[off:min(off+n, len(r.Body))], nil
	}
	f, err := os.Open(r.SpoolPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSpoolGone, err)
	}
	defer f.Close()
	buf := make([]byte, n)
	got, err := f.ReadAt(buf, int64(off))
	if got > 0 {
		return buf[:got], nil
	}
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("%w: %v", ErrSpoolGone, err)
	}
	return nil, nil
}
