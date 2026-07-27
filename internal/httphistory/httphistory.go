// Package httphistory persists the last dispatched responses of .http
// requests (#1251, epic #1247) under the project-local .ike/http/ directory,
// following the internal/localhistory conventions: JSON files, best-effort
// writes, automatic pruning. Responses are keyed by source file plus request
// key (the request's ### name or stable index, httpfile.Request.Key), so
// every request in a multi-request file keeps its own history.
package httphistory

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	Time       time.Time     `json:"time"`
	Status     string        `json:"status"`
	StatusCode int           `json:"statusCode"`
	Proto      string        `json:"proto"`
	Headers    http.Header   `json:"headers,omitempty"`
	Body       []byte        `json:"-"`
	Truncated  bool          `json:"truncated,omitempty"`
	Duration   time.Duration `json:"duration"`
	Warnings   []string      `json:"warnings,omitempty"`
}

// wireEntry is the on-disk shape of an Entry (#1267). A text body is stored
// as a plain JSON string under "bodyText" so the history file stays readable
// and diffable in the editor; only a binary body falls back to "body", which
// encoding/json base64-encodes. Readers accept both, so files written before
// this split keep loading.
type wireEntry struct {
	Time       time.Time     `json:"time"`
	Status     string        `json:"status"`
	StatusCode int           `json:"statusCode"`
	Proto      string        `json:"proto"`
	Headers    http.Header   `json:"headers,omitempty"`
	BodyText   *string       `json:"bodyText,omitempty"`
	Body       []byte        `json:"body,omitempty"` // base64, binary bodies only
	Truncated  bool          `json:"truncated,omitempty"`
	Duration   time.Duration `json:"duration"`
	Warnings   []string      `json:"warnings,omitempty"`
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
		Warnings: e.Warnings,
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
		Warnings: w.Warnings,
	}
	switch {
	case w.BodyText != nil:
		e.Body = []byte(*w.BodyText)
	case len(w.Body) > 0:
		e.Body = w.Body
	}
	return nil
}

// Response converts a stored entry back into the viewer's response shape.
func (e Entry) Response(requestKey string) *httpclient.Response {
	return &httpclient.Response{
		Status:     e.Status,
		StatusCode: e.StatusCode,
		Proto:      e.Proto,
		Headers:    e.Headers,
		Body:       e.Body,
		Truncated:  e.Truncated,
		Duration:   e.Duration,
		RequestKey: requestKey,
		Warnings:   e.Warnings,
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
		Truncated:  resp.Truncated,
		Duration:   resp.Duration,
		Warnings:   resp.Warnings,
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

// List returns the stored responses for one request, newest first. A missing
// or unreadable file yields nil — history is always best effort.
func (s *Store) List(source, key string) []Entry {
	data, err := os.ReadFile(s.file(source, key))
	if err != nil {
		return nil
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil
	}
	return entries
}

// Append stores one response for the request, pruning beyond MaxPerRequest.
// Errors are swallowed like the local-history store's: losing a history
// entry must never fail a dispatch.
func (s *Store) Append(source, key string, e Entry) {
	entries := append([]Entry{e}, s.List(source, key)...)
	if len(entries) > MaxPerRequest {
		entries = entries[:MaxPerRequest]
	}
	// Indented so the file reads (and diffs) in the editor (#1267).
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(s.file(source, key), data, 0o644)
}
