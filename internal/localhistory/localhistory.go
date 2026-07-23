// Package localhistory implements the on-disk snapshot store behind Local
// History (#35, MVP slice #1023): every successful editor save records the
// saved content, so the user can browse, diff, and restore earlier versions
// of a file independently of git.
//
// Layout (under the same per-project state directory the layout/session
// stores use, e.g. ".ike/history"):
//
//	history/index.json          – per-file entry lists (path → [{ts, hash}])
//	history/objects/<sha256>    – content blobs, content-addressed
//
// Content addressing dedupes: saving identical content twice in a row stores
// no new entry, and the same content shared across files (or across time)
// stores one blob. Pruning keeps at most MaxPerFile entries per file (default
// 50) and drops entries older than MaxAge (default 30 days); blobs no entry
// references anymore are garbage-collected on the next Record.
package localhistory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Defaults for pruning; Store fields override them when positive.
const (
	// DefaultMaxPerFile bounds how many snapshots one file keeps.
	DefaultMaxPerFile = 50
	// DefaultMaxAge drops snapshots older than this at Record time.
	DefaultMaxAge = 30 * 24 * time.Hour
)

// Entry is one recorded snapshot of a file: when it was saved and the hash
// addressing its content blob.
type Entry struct {
	Time time.Time `json:"ts"`
	Hash string    `json:"hash"`
}

// index is the on-disk metadata schema: per-file entry lists, oldest-first.
type index struct {
	Files map[string][]Entry `json:"files"`
}

// Store reads and writes snapshots under Dir. The zero limits select the
// package defaults. Store methods swallow I/O errors where losing a snapshot
// must never disrupt a save (Record); readers report them.
type Store struct {
	Dir        string
	MaxPerFile int           // per-file entry cap; <=0 selects DefaultMaxPerFile
	MaxAge     time.Duration // age cap; <=0 selects DefaultMaxAge

	now func() time.Time // test seam; nil means time.Now
}

// New returns a store rooted at dir with default pruning limits.
func New(dir string) *Store { return &Store{Dir: dir} }

// Hash returns the content address of data (hex sha256).
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// key canonicalizes a file path into its index key, so the same file saved
// via different spellings (relative vs absolute) shares one history.
func key(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}

// Record stores content as the newest snapshot of path. A content identical
// to the file's newest snapshot is a no-op (consecutive-save dedupe). The
// call prunes the file's list to the count/age caps and garbage-collects
// unreferenced blobs. Errors are swallowed: failing to snapshot must never
// disrupt the save that triggered it.
func (s *Store) Record(path string, content []byte) {
	if s == nil || s.Dir == "" || path == "" {
		return
	}
	k := key(path)
	h := Hash(content)
	idx := s.load()
	entries := idx.Files[k]
	if n := len(entries); n > 0 && entries[n-1].Hash == h {
		return // identical consecutive save: nothing new to store
	}
	if err := s.writeObject(h, content); err != nil {
		return
	}
	entries = append(entries, Entry{Time: s.timeNow(), Hash: h})
	idx.Files[k] = s.prune(entries)
	s.collectGarbage(idx)
	s.save(idx)
}

// List returns path's snapshots newest-first (empty when none).
func (s *Store) List(path string) []Entry {
	if s == nil || s.Dir == "" {
		return nil
	}
	entries := s.load().Files[key(path)]
	out := make([]Entry, len(entries))
	for i, e := range entries {
		out[len(entries)-1-i] = e
	}
	return out
}

// Read returns the content blob addressed by hash.
func (s *Store) Read(hash string) ([]byte, error) {
	return os.ReadFile(filepath.Join(s.objectsDir(), hash))
}

// timeNow returns the store clock (the test seam or the wall clock).
func (s *Store) timeNow() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// prune applies the count and age caps to an oldest-first entry list.
func (s *Store) prune(entries []Entry) []Entry {
	maxN := s.MaxPerFile
	if maxN <= 0 {
		maxN = DefaultMaxPerFile
	}
	maxAge := s.MaxAge
	if maxAge <= 0 {
		maxAge = DefaultMaxAge
	}
	cutoff := s.timeNow().Add(-maxAge)
	kept := entries[:0]
	for _, e := range entries {
		if e.Time.After(cutoff) {
			kept = append(kept, e)
		}
	}
	if len(kept) > maxN {
		kept = kept[len(kept)-maxN:]
	}
	return append([]Entry(nil), kept...)
}

// collectGarbage removes object files no index entry references anymore.
func (s *Store) collectGarbage(idx index) {
	referenced := map[string]bool{}
	for _, entries := range idx.Files {
		for _, e := range entries {
			referenced[e.Hash] = true
		}
	}
	names, err := os.ReadDir(s.objectsDir())
	if err != nil {
		return
	}
	for _, de := range names {
		if !de.IsDir() && !referenced[de.Name()] {
			_ = os.Remove(filepath.Join(s.objectsDir(), de.Name()))
		}
	}
}

// load reads the index, tolerating a missing or malformed file (empty index).
func (s *Store) load() index {
	idx := index{Files: map[string][]Entry{}}
	data, err := os.ReadFile(s.indexFile())
	if err != nil {
		return idx
	}
	var onDisk index
	if json.Unmarshal(data, &onDisk) == nil && onDisk.Files != nil {
		idx.Files = onDisk.Files
	}
	return idx
}

// save persists the index, dropping files whose entry lists emptied out.
func (s *Store) save(idx index) {
	for k, entries := range idx.Files {
		if len(entries) == 0 {
			delete(idx.Files, k)
		}
	}
	data, err := json.Marshal(idx)
	if err != nil {
		return
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(s.indexFile(), data, 0o644)
}

// writeObject stores a content blob under its hash; an existing blob with the
// same address is already this content.
func (s *Store) writeObject(hash string, content []byte) error {
	dir := s.objectsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, hash)
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, content, 0o644)
}

func (s *Store) indexFile() string  { return filepath.Join(s.Dir, "index.json") }
func (s *Store) objectsDir() string { return filepath.Join(s.Dir, "objects") }
