// Package httphistory persists the last dispatched responses of .http
// requests (#1251, epic #1247) under the project-local .ike/http/ directory,
// following the internal/localhistory conventions: JSON files, best-effort
// writes, automatic pruning. Responses are keyed by source file plus request
// key (the request's ### name or stable index, httpfile.Request.Key), so
// every request in a multi-request file keeps its own history. Each entry
// also carries the request as it was sent (#1832), so a stored response can be
// re-dispatched verbatim; entries written before that field existed load
// unchanged and simply have no snapshot.
package httphistory

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"ike/internal/httpclient"
)

// MaxPerRequest is how many responses are kept per request; older ones are
// pruned on append.
const MaxPerRequest = 5

// Entry is one stored response. In memory the body is always raw bytes; on
// disk it is written readable whenever it can be (#1267) — see MarshalJSON.
type Entry struct {
	Time       time.Time   `json:"time"`
	Status     string      `json:"status"`
	StatusCode int         `json:"statusCode"`
	Proto      string      `json:"proto"`
	Headers    http.Header `json:"headers,omitempty"`
	Body       []byte      `json:"-"`
	// BodyFile holds the *whole* body of a response that was too large to
	// keep in memory (#2157). In memory it is an absolute path — the
	// dispatcher's spool file before the entry is stored, the store's own
	// adopted copy afterwards; on disk it is a name relative to the store
	// directory, so moving a project does not strand it. "" for the ordinary
	// case, where Body is already everything.
	BodyFile string `json:"-"`
	// BodySize is the total body size in bytes, which for a spooled entry is
	// larger than len(Body) — that holds the head only. 0 means "len(Body)".
	BodySize  int           `json:"-"`
	Truncated bool          `json:"truncated,omitempty"`
	Duration  time.Duration `json:"duration"`
	Warnings  []string      `json:"warnings,omitempty"`
	// Captured holds the values this response's `# @capture` directives took
	// out of it (#1993). They are stored with the entry rather than kept in
	// memory, so a captured value lives exactly as long as the response it
	// came from: re-opening the project restores both, and pruning the entry
	// drops the value with it.
	Captured map[string]string `json:"captured,omitempty"`
	// Request is the request as it was sent (#1832), so this very exchange can
	// be repeated without re-reading the .http file. nil for entries written
	// before the capture existed — those load fine and only lose re-send.
	Request *httpclient.RequestSnapshot `json:"-"`
}

// wireEntry is the on-disk shape of an Entry (#1267). A text body is stored
// as a plain JSON string under "bodyText" so the history file stays readable
// and diffable in the editor; only a binary body falls back to "body", which
// encoding/json base64-encodes. Readers accept both, so files written before
// this split keep loading.
type wireEntry struct {
	Time       time.Time         `json:"time"`
	Status     string            `json:"status"`
	StatusCode int               `json:"statusCode"`
	Proto      string            `json:"proto"`
	Headers    http.Header       `json:"headers,omitempty"`
	BodyText   *string           `json:"bodyText,omitempty"`
	Body       []byte            `json:"body,omitempty"`     // base64, binary bodies only
	BodyFile   string            `json:"bodyFile,omitempty"` // spooled body, relative to the store dir (#2157)
	BodySize   int               `json:"bodySize,omitempty"` // total body size when spooled (#2157)
	Truncated  bool              `json:"truncated,omitempty"`
	Duration   time.Duration     `json:"duration"`
	Warnings   []string          `json:"warnings,omitempty"`
	Captured   map[string]string `json:"captured,omitempty"` // capture directives (#1993)
	Request    *wireRequest      `json:"request,omitempty"`  // as-sent snapshot (#1832)
}

// wireRequest is the on-disk shape of the as-sent request snapshot (#1832).
// The body follows the response body's rule — readable "bodyText" for text,
// base64 "body" for binary — so a JSON payload stays diffable in the editor.
// A file without the field predates the capture and reads back as no
// snapshot, which is what disables re-send for that entry.
type wireRequest struct {
	Method   string      `json:"method"`
	URL      string      `json:"url"`
	Headers  http.Header `json:"headers,omitempty"`
	BodyText *string     `json:"bodyText,omitempty"`
	Body     []byte      `json:"body,omitempty"`
}

// toWire converts a snapshot into its on-disk shape; nil stays nil.
func toWire(s *httpclient.RequestSnapshot) *wireRequest {
	if s == nil {
		return nil
	}
	w := &wireRequest{Method: s.Method, URL: s.URL, Headers: s.Headers}
	switch {
	case len(s.Body) == 0:
	case isText(s.Body):
		text := string(s.Body)
		w.BodyText = &text
	default:
		w.Body = s.Body
	}
	return w
}

// fromWire converts a stored snapshot back; nil (missing field) stays nil.
func fromWire(w *wireRequest) *httpclient.RequestSnapshot {
	if w == nil {
		return nil
	}
	s := &httpclient.RequestSnapshot{Method: w.Method, URL: w.URL, Headers: w.Headers}
	switch {
	case w.BodyText != nil:
		s.Body = []byte(*w.BodyText)
	case len(w.Body) > 0:
		s.Body = w.Body
	}
	return s
}

// isText reports whether a body can be stored as a JSON string: valid UTF-8
// without NUL bytes — the same rule the viewer uses to decide it can render a
// body at all.
func isText(b []byte) bool {
	return utf8.Valid(b) && !bytes.ContainsRune(b, 0)
}

// MarshalJSON writes the readable form for text bodies (#1267).
func (e Entry) MarshalJSON() ([]byte, error) {
	w := wireEntry{
		Time: e.Time, Status: e.Status, StatusCode: e.StatusCode, Proto: e.Proto,
		Headers: e.Headers, Truncated: e.Truncated, Duration: e.Duration,
		Warnings: e.Warnings, Captured: e.Captured, Request: toWire(e.Request),
		BodyFile: e.BodyFile, BodySize: e.BodySize,
	}
	switch {
	case len(e.Body) == 0:
	case isText(e.Body):
		text := string(e.Body)
		w.BodyText = &text
	default:
		w.Body = e.Body
	}
	return json.Marshal(w)
}

// UnmarshalJSON reads both shapes: the readable "bodyText" and the base64
// "body" older files (and binary responses) carry.
func (e *Entry) UnmarshalJSON(data []byte) error {
	var w wireEntry
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*e = Entry{
		Time: w.Time, Status: w.Status, StatusCode: w.StatusCode, Proto: w.Proto,
		Headers: w.Headers, Truncated: w.Truncated, Duration: w.Duration,
		Warnings: w.Warnings, Captured: w.Captured, Request: fromWire(w.Request),
		BodyFile: w.BodyFile, BodySize: w.BodySize,
	}
	switch {
	case w.BodyText != nil:
		e.Body = []byte(*w.BodyText)
	case len(w.Body) > 0:
		e.Body = w.Body
	}
	return nil
}

// FullBody returns the entry's whole body: Body for an ordinary entry, the
// contents of the spooled body file for a large one (#2157). A body file that
// has gone missing degrades to the stored head — a shortened comparison beats
// no comparison. Callers should let the result go again: it is the copy the
// spooling exists to avoid keeping.
func (e Entry) FullBody() []byte {
	if e.BodyFile == "" {
		return e.Body
	}
	data, err := os.ReadFile(e.BodyFile)
	if err != nil {
		return e.Body
	}
	return data
}

// BodySizeBytes is the entry's total body size — larger than len(Body) for a
// spooled entry, which holds only the head.
func (e Entry) BodySizeBytes() int {
	if e.BodySize > 0 {
		return e.BodySize
	}
	return len(e.Body)
}

// Response converts a stored entry back into the viewer's response shape.
func (e Entry) Response(requestKey string) *httpclient.Response {
	return &httpclient.Response{
		Status:     e.Status,
		StatusCode: e.StatusCode,
		Proto:      e.Proto,
		Headers:    e.Headers,
		Body:       e.Body,
		SpoolPath:  e.BodyFile,
		BodySize:   e.BodySize,
		Truncated:  e.Truncated,
		Duration:   e.Duration,
		RequestKey: requestKey,
		Warnings:   e.Warnings,
		Request:    e.Request,
	}
}

// FromResponse converts a dispatch result into a storable entry.
func FromResponse(resp *httpclient.Response, at time.Time) Entry {
	return Entry{
		Time:       at,
		Status:     resp.Status,
		StatusCode: resp.StatusCode,
		Proto:      resp.Proto,
		Headers:    resp.Headers,
		Body:       resp.Body,
		BodyFile:   resp.SpoolPath,
		BodySize:   resp.BodySize,
		Truncated:  resp.Truncated,
		Duration:   resp.Duration,
		Warnings:   resp.Warnings,
		Captured:   resp.CapturedValues(),
		Request:    resp.Request,
	}
}

// Store reads and writes per-request response history below Dir.
type Store struct {
	Dir string
}

// New returns a store rooted at dir (created lazily on the first append).
func New(dir string) *Store { return &Store{Dir: dir} }

// file maps a source file + request key onto the history file path. The name
// stays debuggable (base name prefix) while the hash keeps it unique and
// filesystem-safe for any path/key content.
func (s *Store) file(source, key string) string {
	sum := sha256.Sum256([]byte(source + "\x00" + key))
	base := filepath.Base(source)
	if len(base) > 40 {
		base = base[:40]
	}
	return filepath.Join(s.Dir, base+"-"+hex.EncodeToString(sum[:8])+".json")
}

// bodiesDir is where the store keeps the spooled bodies of large responses
// (#2157), one file per entry, next to the history files themselves.
const bodiesDir = "bodies"

// List returns the stored responses for one request, newest first. A missing
// or unreadable file yields nil — history is always best effort. A spooled
// body's file name is resolved against the store directory, so what callers
// see is a path they can open.
func (s *Store) List(source, key string) []Entry {
	data, err := os.ReadFile(s.file(source, key))
	if err != nil {
		return nil
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil
	}
	for i := range entries {
		if entries[i].BodyFile != "" {
			entries[i].BodyFile = filepath.Join(s.Dir, entries[i].BodyFile)
		}
	}
	return entries
}

// adoptBody copies a dispatcher spool file (#2157) into the store's own
// bodies/ directory and rewrites the entry's BodyFile to the name it should
// be recorded under. The dispatcher's file is a per-process temp file that
// disappears on exit, so the copy is what makes a large body outlive the
// session the way every other part of an entry does. A copy that fails costs
// the entry its full body — the stored head still shows — never the entry.
func (s *Store) adoptBody(e *Entry) {
	if e.BodyFile == "" {
		return
	}
	dir := filepath.Join(s.Dir, bodiesDir)
	src, err := os.Open(e.BodyFile)
	if err != nil {
		e.BodyFile, e.BodySize = "", 0
		return
	}
	defer src.Close()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.BodyFile, e.BodySize = "", 0
		return
	}
	dst, err := os.CreateTemp(dir, "body-*.bin")
	if err != nil {
		e.BodyFile, e.BodySize = "", 0
		return
	}
	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(dst.Name())
		e.BodyFile, e.BodySize = "", 0
		return
	}
	e.BodyFile = filepath.Join(bodiesDir, filepath.Base(dst.Name()))
}

// dropBodies removes the body files of entries the prune dropped, so bodies/
// follows the five-entry ring instead of growing without bound.
func (s *Store) dropBodies(dropped []Entry) {
	for _, e := range dropped {
		if e.BodyFile == "" {
			continue
		}
		path := e.BodyFile
		if !filepath.IsAbs(path) {
			path = filepath.Join(s.Dir, path)
		}
		if filepath.Dir(path) != filepath.Join(s.Dir, bodiesDir) {
			continue // never delete outside our own bodies/ directory
		}
		_ = os.Remove(path)
	}
}

// Append stores one response for the request, pruning beyond MaxPerRequest.
// Errors are swallowed like the local-history store's: losing a history
// entry must never fail a dispatch.
func (s *Store) Append(source, key string, e Entry) {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return
	}
	// A spooled body (#2157) becomes the store's own file before the entry is
	// written, so the recorded name still resolves once the dispatcher's temp
	// directory is gone.
	s.adoptBody(&e)
	entries := append([]Entry{e}, s.List(source, key)...)
	var dropped []Entry
	if len(entries) > MaxPerRequest {
		entries, dropped = entries[:MaxPerRequest], entries[MaxPerRequest:]
	}
	// List handed back absolute body paths; they are stored relative.
	for i := range entries {
		if entries[i].BodyFile != "" {
			if rel, err := filepath.Rel(s.Dir, entries[i].BodyFile); err == nil {
				entries[i].BodyFile = rel
			}
		}
	}
	// Indented so the file reads (and diffs) in the editor (#1267).
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(s.file(source, key), data, 0o644); err != nil {
		return
	}
	s.dropBodies(dropped)
}
