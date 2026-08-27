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

// Feature is one per-edit service the large-file policy can degrade
// individually (#2159). Past the base thresholds every feature is off (the
// #149 cliff); each feature can additionally be switched off *earlier* by its
// own byte threshold, so the heaviest services stop before the file counts as
// fully large.
type Feature int

const (
	// FeatureHighlight is the Tree-sitter parse + lint + Unicode scan that a
	// buffer change schedules (editor parseCmd).
	FeatureHighlight Feature = iota
	// FeatureLSP is full-document didChange sync (and the bridge's didOpen
	// gate): each change re-joins and ships the whole buffer.
	FeatureLSP
	// FeatureVCS is the gutter diff-marker recompute: `git show` of the HEAD
	// version plus a whole-file diff per refresh.
	FeatureVCS
	// FeatureSearch is the search match tally: a capped but still large buffer
	// scan per keystroke while search highlights are armed.
	FeatureSearch
	// FeatureFormat is the on-save LSP chain (organize imports / format),
	// which ships and rewrites the full buffer.
	FeatureFormat
	// FeatureCount bounds iteration over the features.
	FeatureCount
)

// featureKeys are the per-feature config keys, indexed by Feature. Each holds
// a KB threshold; 0 (the default) means the feature follows the base limits.
var featureKeys = [FeatureCount]string{
	FeatureHighlight: "files.large_file_highlight_kb",
	FeatureLSP:       "files.large_file_lsp_kb",
	FeatureVCS:       "files.large_file_vcs_kb",
	FeatureSearch:    "files.large_file_search_kb",
	FeatureFormat:    "files.large_file_format_kb",
}

// featureLabels are the user-facing names, indexed by Feature (the status-line
// detail popup lists them).
var featureLabels = [FeatureCount]string{
	FeatureHighlight: "syntax highlighting",
	FeatureLSP:       "LSP sync",
	FeatureVCS:       "VCS gutter diff",
	FeatureSearch:    "search match counter",
	FeatureFormat:    "format on save",
}

// Label returns the feature's user-facing name.
func (f Feature) Label() string {
	if f < 0 || f >= FeatureCount {
		return ""
	}
	return featureLabels[f]
}

// Key returns the feature's config key (the per-feature KB threshold).
func (f Feature) Key() string {
	if f < 0 || f >= FeatureCount {
		return ""
	}
	return featureKeys[f]
}

// Thresholds is the full evaluated policy: the base limits plus the optional
// per-feature byte thresholds (0 = that feature follows Base).
type Thresholds struct {
	Base         Limits
	FeatureBytes [FeatureCount]int64
}

// ThresholdsFrom reads the base limits and every per-feature key from get; nil
// or unset keys fall back to the defaults (per-feature: follow base).
func ThresholdsFrom(get Getter) Thresholds {
	t := Thresholds{Base: LimitsFrom(get)}
	if get == nil {
		return t
	}
	for f := Feature(0); f < FeatureCount; f++ {
		if v, ok := get(featureKeys[f]); ok {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				t.FeatureBytes[f] = int64(n) * 1024
			}
		}
	}
	return t
}

// Off reports whether feature f is degraded for a document of the given size:
// past the base limits everything is off; below them a feature with its own
// threshold set switches off once bytes exceed it. Per-feature thresholds only
// ever degrade earlier — they never re-enable a feature past the base cliff.
func (t Thresholds) Off(f Feature, bytes int64, lines int) bool {
	if t.Base.Exceeded(bytes, lines) {
		return true
	}
	if f < 0 || f >= FeatureCount {
		return false
	}
	b := t.FeatureBytes[f]
	return b > 0 && bytes > b
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
// regardless of how they spelled the path; failure keeps it verbatim. The
// empty path (a file-less buffer) short-circuits: resolving it would cost a
// Getwd syscall per lookup — and Forced/Dismissed are consulted per keystroke.
func canon(path string) string {
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}
