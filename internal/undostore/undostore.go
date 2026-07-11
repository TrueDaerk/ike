// Package undostore persists per-document undo history across restarts
// (#148, vim's undofile). One JSON file per document lives under the state
// store (the same root the session/layout store uses: IKE_CONFIG_DIR when
// set, otherwise the project's ".ike" directory), keyed by a hash of the
// document's absolute path. Each file records the content hash the stacks
// were taken against; Load hands the stacks back only when the just-read
// file content still hashes to the same value — any mismatch (external
// change, git checkout) discards silently, correctness over continuity.
package undostore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"ike/internal/editor/history"
)

const (
	version = 1
	// maxFileBytes caps one serialized history: a stack that would exceed it
	// is not persisted (and a previously written file is removed, so stale
	// stacks never linger under a still-matching hash).
	maxFileBytes = 1 << 20 // 1 MiB
	// maxStoreFiles caps the store as a whole: past it, the least recently
	// written undo files are pruned.
	maxStoreFiles = 200
)

// envelope is the on-disk schema. Path is informational (debugging a hashed
// filename); Hash gates adoption.
type envelope struct {
	Version int              `json:"version"`
	Path    string           `json:"path"`
	Hash    string           `json:"hash"`
	History history.Snapshot `json:"history"`
}

// Hash returns the content hash used to key adoption (hex sha256).
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// dir mirrors the session store's discovery: IKE_CONFIG_DIR overrides the
// base directory (tests redirect writes), otherwise the project's ".ike".
// Everything lives under one "undo" subdirectory so wiping the state store
// wipes the undo files with it.
func dir() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "undo")
	}
	return filepath.Join(".ike", "undo")
}

// fileFor maps a document path to its undo file: the store directory plus the
// hash of the absolute path, so the same file opened via different relative
// spellings shares one history.
func fileFor(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return filepath.Join(dir(), Hash([]byte(abs))+".json")
}

// Save persists snap for path, stamped with the hash of the file content the
// stacks describe. An empty snapshot removes the undo file instead (nothing
// worth keeping), as does a snapshot over the per-file size cap. Errors are
// swallowed: failing to persist undo must never disrupt a save or shutdown.
func Save(path, hash string, snap history.Snapshot) {
	file := fileFor(path)
	if hash == "" || snap.Empty() {
		_ = os.Remove(file)
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	data, err := json.Marshal(envelope{Version: version, Path: abs, Hash: hash, History: snap})
	if err != nil || len(data) > maxFileBytes {
		_ = os.Remove(file)
		return
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(file, data, 0o644)
	prune()
}

// Load returns the persisted snapshot for path if one exists and was taken
// against content hashing to hash. Any missing, malformed, wrong-version, or
// hash-mismatched file yields ok=false.
func Load(path, hash string) (history.Snapshot, bool) {
	data, err := os.ReadFile(fileFor(path))
	if err != nil {
		return history.Snapshot{}, false
	}
	var env envelope
	if json.Unmarshal(data, &env) != nil {
		return history.Snapshot{}, false
	}
	if env.Version != version || env.Hash == "" || env.Hash != hash {
		return history.Snapshot{}, false
	}
	return env.History, true
}

// prune drops the least recently written undo files once the store exceeds
// maxStoreFiles.
func prune() {
	entries, err := os.ReadDir(dir())
	if err != nil || len(entries) <= maxStoreFiles {
		return
	}
	type aged struct {
		name string
		mod  int64
	}
	var files []aged
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, aged{name: e.Name(), mod: info.ModTime().UnixNano()})
	}
	if len(files) <= maxStoreFiles {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod < files[j].mod })
	for _, f := range files[:len(files)-maxStoreFiles] {
		_ = os.Remove(filepath.Join(dir(), f.name))
	}
}
