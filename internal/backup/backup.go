// Package backup implements crash-recovery snapshots for dirty editor buffers
// (Roadmap 0210). It is deliberately dumb: one file per document holding the
// full text plus a small header (the buffer's stable key, the base file's path,
// and the base file's mtime + hash and a timestamp), written atomically. Full
// text — not deltas — means recovery never depends on replaying edit history.
//
// The package holds no editor state and does no scheduling: the debounce timing
// lives in Debouncer (driven by an injectable clock) and the file I/O in Service
// (pointed at a directory). The app wires the two into its event loop — marking a
// buffer on the change seam, snapshotting due buffers off the Update loop, and
// removing a snapshot on save / discard / clean shutdown.
package backup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ext is the snapshot file extension; magic tags the header's first line.
const (
	ext   = ".ikebak"
	magic = "IKEBAK1"
)

// Doc is a buffer to snapshot. Key is a stable per-buffer identity (the absolute
// path for saved files, a synthetic token for untitled buffers) and determines
// the snapshot filename; Path is the on-disk base file ("" for an untitled
// buffer, recorded as "no base file"). BaseMTime/BaseHash describe the base
// version so the restore flow can detect a file that changed since the snapshot.
type Doc struct {
	Key       string
	Path      string
	Text      string
	BaseMTime time.Time
	BaseHash  string
}

// Snapshot is a decoded snapshot file.
type Snapshot struct {
	File      string
	Key       string
	Path      string
	HasBase   bool
	BaseMTime time.Time
	BaseHash  string
	Timestamp time.Time
	Text      string
}

// Service reads and writes snapshot files under a directory.
type Service struct {
	dir   string
	clock func() time.Time
}

// New returns a Service writing under dir. clock supplies snapshot timestamps; a
// nil clock defaults to time.Now.
func New(dir string, clock func() time.Time) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{dir: dir, clock: clock}
}

// Dir returns the snapshot directory under a project state base (sibling of
// layout.json / session.json).
func Dir(base string) string { return filepath.Join(base, "backups") }

// fileFor maps a buffer key to its snapshot path (a hash keeps the name filesystem
// -safe and fixed-length regardless of the original path).
func (s *Service) fileFor(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:])+ext)
}

// Snapshot atomically writes d's snapshot, creating the directory if needed.
func (s *Service) Snapshot(d Doc) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	return writeFileAtomic(s.fileFor(d.Key), encode(d, s.clock()))
}

// Remove deletes the snapshot for key. A missing snapshot is not an error.
func (s *Service) Remove(key string) error {
	if err := os.Remove(s.fileFor(key)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// List returns every readable snapshot in the directory, oldest first. Unreadable
// or malformed files are skipped; a missing directory yields no snapshots.
func (s *Service) List() ([]Snapshot, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Snapshot
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ext) {
			continue
		}
		p := filepath.Join(s.dir, e.Name())
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		snap, err := decode(p, data)
		if err != nil {
			continue
		}
		out = append(out, snap)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Timestamp.Equal(out[j].Timestamp) {
			return out[i].Key < out[j].Key
		}
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	return out, nil
}

// Prune removes every snapshot older than maxAge (against the service clock)
// and returns how many it removed. Age-based GC (#167) runs at startup, only
// after the restore prompt has had its say, so nothing is pruned unseen.
func (s *Service) Prune(maxAge time.Duration) (int, error) {
	snaps, err := s.List()
	if err != nil {
		return 0, err
	}
	cutoff := s.clock().Add(-maxAge)
	pruned := 0
	for _, snap := range snaps {
		if snap.Timestamp.Before(cutoff) {
			if err := os.Remove(snap.File); err != nil && !errors.Is(err, os.ErrNotExist) {
				return pruned, err
			}
			pruned++
		}
	}
	return pruned, nil
}

// Purge removes every snapshot file, malformed ones included, and returns how
// many it removed. Disabling the subsystem calls it: snapshots hold file
// contents, so [backup] enable = false must not leave any behind.
func (s *Service) Purge() (int, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	purged := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ext) {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, e.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return purged, err
		}
		purged++
	}
	return purged, nil
}

// BaseInfo stats and hashes the on-disk file at path for a snapshot's base
// header. ok is false for an empty path (untitled buffer) or an unreadable file.
func BaseInfo(path string) (mtime time.Time, hash string, ok bool) {
	if path == "" {
		return time.Time{}, "", false
	}
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, "", false
	}
	sum := sha256.Sum256(data)
	return info.ModTime(), hex.EncodeToString(sum[:]), true
}

// encode renders a snapshot: a magic line, "key: value" header lines, a blank
// line, then the full text verbatim.
func encode(d Doc, ts time.Time) []byte {
	hasBase := d.Path != ""
	var b strings.Builder
	b.WriteString(magic + "\n")
	b.WriteString("key: " + d.Key + "\n")
	b.WriteString("path: " + d.Path + "\n")
	b.WriteString("has_base: " + strconv.FormatBool(hasBase) + "\n")
	if hasBase {
		b.WriteString("base_mtime: " + d.BaseMTime.UTC().Format(time.RFC3339Nano) + "\n")
		b.WriteString("base_hash: " + d.BaseHash + "\n")
	} else {
		b.WriteString("base_mtime: \n")
		b.WriteString("base_hash: \n")
	}
	b.WriteString("timestamp: " + ts.UTC().Format(time.RFC3339Nano) + "\n")
	b.WriteString("\n")
	b.WriteString(d.Text)
	return []byte(b.String())
}

// decode parses a snapshot file. The header runs up to the first blank line; the
// rest is the text verbatim (further blank lines belong to the text).
func decode(file string, data []byte) (Snapshot, error) {
	idx := bytes.Index(data, []byte("\n\n"))
	if idx < 0 {
		return Snapshot{}, errors.New("backup: no header terminator")
	}
	header := string(data[:idx])
	lines := strings.Split(header, "\n")
	if len(lines) == 0 || lines[0] != magic {
		return Snapshot{}, errors.New("backup: bad magic")
	}
	s := Snapshot{File: file, Text: string(data[idx+2:])}
	for _, ln := range lines[1:] {
		k, v, ok := strings.Cut(ln, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "key":
			s.Key = v
		case "path":
			s.Path = v
		case "has_base":
			s.HasBase = v == "true"
		case "base_mtime":
			if v != "" {
				s.BaseMTime, _ = time.Parse(time.RFC3339Nano, v)
			}
		case "base_hash":
			s.BaseHash = v
		case "timestamp":
			if v != "" {
				s.Timestamp, _ = time.Parse(time.RFC3339Nano, v)
			}
		}
	}
	return s, nil
}

// writeFileAtomic writes data to a temp file in the target directory, fsyncs it,
// then renames it over path so a reader never sees a half-written snapshot.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".ikebak-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // harmless no-op once the rename succeeds
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil { // best-effort durability of the rename
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
