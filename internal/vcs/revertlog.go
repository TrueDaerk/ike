package vcs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// revertlog.go persists pre-revert snapshots (#556): before vcs.revertFile
// checks a file out to HEAD, its current content is recorded here so
// vcs.undoRevert can restore it — reverts stop being one-way. One JSON file
// per document lives under the state store (IKE_CONFIG_DIR when set,
// otherwise the project's ".ike"), keyed by a hash of the absolute path, the
// undostore layout. Entries are newest-first, capped by count and age;
// oversized contents are skipped rather than blowing up the store. Errors
// are swallowed throughout — failing to log must never block the revert.

const (
	revertLogVersion = 1
	// maxRevertEntries caps the snapshots kept per document.
	maxRevertEntries = 10
	// maxRevertBytes caps one snapshot's content; bigger files revert
	// unlogged (the confirmation prompt stays honest about that risk being
	// theirs — 1 MiB of hand-edits is not a terminal-IDE scenario).
	maxRevertBytes = 1 << 20
	// maxRevertAge prunes snapshots on read and write.
	maxRevertAge = 30 * 24 * time.Hour
)

// RevertSnapshot is one recorded pre-revert state of a document.
type RevertSnapshot struct {
	At      time.Time `json:"at"`
	Changed int       `json:"changed"` // lines that differed from HEAD when taken
	Content string    `json:"content"`
}

// revertEnvelope is the on-disk schema; Path is informational (debugging a
// hashed filename).
type revertEnvelope struct {
	Version int              `json:"version"`
	Path    string           `json:"path"`
	Entries []RevertSnapshot `json:"entries"` // newest first
}

// revertDir mirrors the undostore's discovery: IKE_CONFIG_DIR overrides the
// base (tests redirect writes), otherwise the project's ".ike".
func revertDir() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "reverts")
	}
	return filepath.Join(".ike", "reverts")
}

// revertFileFor maps a document path to its log file via the hash of the
// absolute path, so relative spellings share one history.
func revertFileFor(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	sum := sha256.Sum256([]byte(abs))
	return filepath.Join(revertDir(), hex.EncodeToString(sum[:])+".json")
}

// SaveRevertSnapshot prepends one snapshot to the document's revert log. The
// read-modify-write is unsynchronized: concurrent saves of the same document
// can lose one snapshot — acceptable for a log fed by a single interactive
// revert flow.
func SaveRevertSnapshot(path, content string, changed int) {
	if len(content) > maxRevertBytes {
		return
	}
	entries := append([]RevertSnapshot{{At: time.Now(), Changed: changed, Content: content}},
		loadRevertEntries(path)...)
	if len(entries) > maxRevertEntries {
		entries = entries[:maxRevertEntries]
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	data, err := json.Marshal(revertEnvelope{Version: revertLogVersion, Path: abs, Entries: entries})
	if err != nil {
		return
	}
	if err := os.MkdirAll(revertDir(), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(revertFileFor(path), data, 0o644)
}

// RevertSnapshots returns the document's recorded pre-revert states, newest
// first, with expired entries pruned.
func RevertSnapshots(path string) []RevertSnapshot {
	return loadRevertEntries(path)
}

// loadRevertEntries reads and age-filters the log; anything malformed or
// wrong-version reads as empty.
func loadRevertEntries(path string) []RevertSnapshot {
	data, err := os.ReadFile(revertFileFor(path))
	if err != nil {
		return nil
	}
	var env revertEnvelope
	if json.Unmarshal(data, &env) != nil || env.Version != revertLogVersion {
		return nil
	}
	cutoff := time.Now().Add(-maxRevertAge)
	kept := env.Entries[:0]
	for _, e := range env.Entries {
		if e.At.After(cutoff) {
			kept = append(kept, e)
		}
	}
	return kept
}
