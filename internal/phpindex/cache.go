package phpindex

// cache.go persists the index across sessions (#2885). A cold walk of a large
// project parses every PHP file — 55 s for 6.8k files in the wild — and until
// it finishes the trait features have nothing to answer with. The walk's
// per-file extractions are therefore written to the project's .ike directory
// (php-index.gob, next to the other per-project state) once a scan finishes,
// each with the size and mtime of the file it came from. The next session's
// walk seeds itself from that file: a file whose stamp still matches is taken
// as it is, a changed or new one is parsed, and one the walk no longer finds
// (deleted, excluded, now beyond the file cap) is simply never visited, so the
// cache cannot resurrect it.
//
// The file is a gob stream: a header (format version, project root) followed
// by one record per file. The version is cacheVersion; a cache written under
// another one, or for another root, is ignored and the walk runs cold.

import (
	"bufio"
	"encoding/gob"
	"io"
	"os"
	"path/filepath"

	"ike/internal/complete/langindex"
)

// cacheVersion stamps the cache file's format. Bump it whenever fileDecls (or
// a type it holds) changes shape, or the extractor's output changes for the
// same input — a cache from before the change must not seed a new session.
// TestCacheVersionTracksFormat fails when the shape moves without a bump.
const cacheVersion = 1

// cacheName is the cache file's name inside the per-project state directory.
const cacheName = "php-index.gob"

// CacheFile is where the index for root persists (#2885): the project's .ike
// directory, or IKE_CONFIG_DIR when set — the same redirection seam every
// per-project state file follows. "" for an empty root (nothing to persist).
func CacheFile(root string) string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, cacheName)
	}
	if root == "" {
		return ""
	}
	return filepath.Join(root, ".ike", cacheName)
}

// cacheHeader opens the cache file. Root guards the IKE_CONFIG_DIR case,
// where one directory serves whichever project was opened last.
type cacheHeader struct {
	Version int
	Root    string
	Files   int
}

// cacheRecord is one file's extraction. Decl.Path and Member.Path always
// equal the file's own path, so they are stored once per record and restored
// on load — on a large project the repetition is most of the file.
type cacheRecord struct {
	Path  string
	Stamp langindex.Stamp
	Decls fileDecls
}

// loadCache reads the seed for root from file; nil for a missing, foreign,
// outdated or damaged cache — the walk then runs cold.
func loadCache(file, root string) map[string]langindex.Seeded[fileDecls] {
	if file == "" {
		return nil
	}
	f, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer f.Close()
	dec := gob.NewDecoder(bufio.NewReader(f))
	var h cacheHeader
	if err := dec.Decode(&h); err != nil || h.Version != cacheVersion || h.Root != root || h.Files < 0 {
		return nil
	}
	seed := make(map[string]langindex.Seeded[fileDecls], h.Files)
	for i := 0; i < h.Files; i++ {
		var r cacheRecord
		if err := dec.Decode(&r); err != nil {
			return nil
		}
		restorePaths(&r.Decls, r.Path)
		seed[r.Path] = langindex.Seeded[fileDecls]{Stamp: r.Stamp, Value: r.Decls}
	}
	return seed
}

// saveCache writes p's PHP extractions for root to file, atomically: a
// temporary sibling renamed into place, so a crash mid-write leaves the old
// cache (or none), never a torn one. A walk without a single PHP file writes
// nothing (and drops a cache left over), so a project that is not PHP never
// grows the file.
func saveCache(file, root string, p *langindex.Index[fileDecls]) error {
	if file == "" || p == nil {
		return nil
	}
	var recs []cacheRecord
	p.EachStamped("php", func(path string, st langindex.Stamp, v fileDecls) {
		recs = append(recs, cacheRecord{Path: path, Stamp: st, Decls: v})
	})
	if len(recs) == 0 {
		removeCache(file)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), cacheName+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := writeCache(tmp, root, recs); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), file)
}

// writeCache encodes the header and records to w.
func writeCache(w io.Writer, root string, recs []cacheRecord) error {
	bw := bufio.NewWriter(w)
	enc := gob.NewEncoder(bw)
	if err := enc.Encode(cacheHeader{Version: cacheVersion, Root: root, Files: len(recs)}); err != nil {
		return err
	}
	for _, r := range recs {
		r.Decls = stripPaths(r.Decls)
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// removeCache deletes the cache file (php.index.cache turned off); a missing
// file is the state asked for.
func removeCache(file string) {
	if file != "" {
		_ = os.Remove(file)
	}
}

// stripPaths returns a copy of fd with every declaration's and member's Path
// cleared; the live extraction is shared with the index and stays intact.
func stripPaths(fd fileDecls) fileDecls {
	out := fileDecls{Refs: fd.Refs, Decls: make([]Decl, len(fd.Decls))}
	for i, d := range fd.Decls {
		d.Path = ""
		ms := make([]Member, len(d.Members))
		for j, m := range d.Members {
			m.Path = ""
			ms[j] = m
		}
		d.Members = ms
		out.Decls[i] = d
	}
	return out
}

// restorePaths sets every declaration's and member's Path back to path.
func restorePaths(fd *fileDecls, path string) {
	for i := range fd.Decls {
		d := &fd.Decls[i]
		d.Path = path
		for j := range d.Members {
			d.Members[j].Path = path
		}
	}
}
