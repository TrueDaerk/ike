// Package largefile holds the large-file policy (#149): the size/line
// thresholds that flag a document as "large" (code insight degraded: no
// highlighting, no LSP, no content hashing) and the per-document override set
// the editor.forceCodeInsight command punches through it with. It is a leaf
// package so the editor, the LSP bridge, and the app share one decision
// without importing each other.
package largefile

import (
	"path/filepath"
	"strconv"
	"sync"
)

// Defaults, mirrored by internal/config's Files defaults.
const (
	DefaultMaxKB    = 1024
	DefaultMaxLines = 100_000
)

// Limits are the evaluated thresholds. A zero or negative value disables that
// guard (a file can then only be flagged by the other one).
type Limits struct {
	MaxBytes int64
	MaxLines int
}

// Getter is the config lookup shape (host.Config.Get) — a function type so
// this package needs no host import.
type Getter func(key string) (string, bool)

// LimitsFrom reads files.large_file_kb and files.large_file_lines from get,
// falling back to the defaults when get is nil or a key is unset/malformed.
func LimitsFrom(get Getter) Limits {
	l := Limits{MaxBytes: DefaultMaxKB * 1024, MaxLines: DefaultMaxLines}
	if get == nil {
		return l
	}
	if v, ok := get("files.large_file_kb"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			l.MaxBytes = int64(n) * 1024
		}
	}
	if v, ok := get("files.large_file_lines"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			l.MaxLines = n
		}
	}
	return l
}

// Exceeded reports whether a document of the given byte size and line count
// crosses either enabled threshold.
func (l Limits) Exceeded(bytes int64, lines int) bool {
	if l.MaxBytes > 0 && bytes > l.MaxBytes {
		return true
	}
	if l.MaxLines > 0 && lines > l.MaxLines {
		return true
	}
	return false
}

// Override set: paths whose user forced code insight back on despite the flag
// (editor.forceCodeInsight). Process-wide because the editor document flag and
// the LSP bridge's didOpen gate must agree, and the bridge only ever sees a
// path. Keyed by absolute path, mirroring the watcher's canonicalization.
var (
	mu        sync.Mutex
	forced    = map[string]bool{}
	dismissed = map[string]bool{}
)

// Force marks path as insight-forced: Forced(path) reports true until Reset.
func Force(path string) {
	mu.Lock()
	forced[canon(path)] = true
	mu.Unlock()
}

// Forced reports whether the user forced code insight back on for path.
func Forced(path string) bool {
	mu.Lock()
	defer mu.Unlock()
	return forced[canon(path)]
}

// DismissNotice records that the user dismissed the large-file banner for
// path (#1124); per document, so it survives tab switches and other flagged
// files still show theirs.
func DismissNotice(path string) {
	mu.Lock()
	dismissed[canon(path)] = true
	mu.Unlock()
}

// NoticeDismissed reports whether the banner was dismissed for path.
func NoticeDismissed(path string) bool {
	mu.Lock()
	defer mu.Unlock()
	return dismissed[canon(path)]
}

// Reset clears every override (tests; project switch keeps them — the paths
// are absolute, so stale entries are harmless).
func Reset() {
	mu.Lock()
	forced = map[string]bool{}
	dismissed = map[string]bool{}
	mu.Unlock()
}

// canon resolves path to its absolute form so callers agree on the key
// regardless of how they spelled the path; failure keeps it verbatim.
func canon(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}
