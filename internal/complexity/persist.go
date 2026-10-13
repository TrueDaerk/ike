package complexity

// persist.go persists the project index across sessions (#2967). A cold walk
// measures every supported file of the project — 3 187 files / 33 949
// functions in IKE, a few seconds during which the tool window says
// scanning… and the Problems feed is empty. The walk's per-file
// measurements are therefore written to the per-project state directory
// (complexity-index.gob, next to the PHP index's php-index.gob) once a scan
// finishes, each with the size and mtime of the file it came from. The next
// session's walk seeds itself from that file through langindex's Persist
// seam: a file whose stamp still matches is taken as it is, a changed or new
// one is measured, and one the walk no longer finds (deleted, excluded, now
// beyond the file cap) is never visited, so the cache cannot resurrect it.
//
// The format mirrors the PHP index's cache (phpindex/cache.go): a gob stream
// of a header (spec version, project root, record count) followed by one
// record per file. A cache written under another SpecVersion, or for another
// root, is ignored and the walk runs cold; a damaged one is ignored too, is
// reported once (Stats.CacheCorrupt, the complexity.scan op's cache field),
// and the finished walk writes a fresh one over it.

import (
	"bufio"
	"encoding/gob"
	"io"
	"os"
	"path/filepath"

	"ike/internal/complete/langindex"
)

// SpecVersion stamps the persisted index. Bump it whenever the measuring
// rules change (a language's spec, the CCN / cognitive / NLOC counting, the
// candidate search) or Function (or a type it holds) changes shape: a cache
// measured under other rules must not seed a new session.
// TestCacheVersionTracksFormat fails when the shape moves without a bump;
// a rule change has no shape, so remember the bump by hand.
const SpecVersion = 1

// cacheName is the cache file's name inside the per-project state directory.
const cacheName = "complexity-index.gob"

// CacheFile is where the index for root persists (#2967): the project's .ike
// directory (ignored by the repository's .ike/ rule), or IKE_CONFIG_DIR when
// set — the same redirection seam every per-project state file follows. ""
// for an empty root (nothing to persist).
func CacheFile(root string) string {
	if root == "" {
		return ""
	}
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, cacheName)
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

// cacheRecord is one file's measurement.
type cacheRecord struct {
	Path      string
	Stamp     langindex.Stamp
	Functions []Function
}

// loadCache reads the seed for root from file. A missing, foreign or
// outdated cache yields nil quietly — the walk runs cold; a damaged one
// yields nil with corrupt set.
func loadCache(file, root string) (seed map[string]langindex.Seeded[[]Function], corrupt bool) {
	if file == "" {
		return nil, false
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	dec := gob.NewDecoder(bufio.NewReader(f))
	var h cacheHeader
	if err := dec.Decode(&h); err != nil || h.Files < 0 {
		return nil, true
	}
	if h.Version != SpecVersion || h.Root != root {
		return nil, false
	}
	seed = make(map[string]langindex.Seeded[[]Function], h.Files)
	for i := 0; i < h.Files; i++ {
		var r cacheRecord
		if err := dec.Decode(&r); err != nil {
			return nil, true
		}
		if r.Functions == nil {
			r.Functions = []Function{}
		}
		seed[r.Path] = langindex.Seeded[[]Function]{Stamp: r.Stamp, Value: r.Functions}
	}
	return seed, false
}

// saveCache writes p's measurements for root to file, atomically: a
// temporary sibling renamed into place, so a crash mid-write leaves the old
// cache (or none), never a torn one. A walk without a single measured file
// writes nothing (and drops a cache left over).
func saveCache(file, root string, p *langindex.Index[[]Function]) error {
	if file == "" || p == nil {
		return nil
	}
	var recs []cacheRecord
	p.EachStamped(indexLang, func(path string, st langindex.Stamp, fns []Function) {
		recs = append(recs, cacheRecord{Path: path, Stamp: st, Functions: fns})
	})
	if len(recs) == 0 {
		_ = os.Remove(file)
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

// writeCache encodes the header and records to w. Function.Baseline is a
// classification a surface stamps in place, not a measurement, so it is
// cleared on the way out.
func writeCache(w io.Writer, root string, recs []cacheRecord) error {
	bw := bufio.NewWriter(w)
	enc := gob.NewEncoder(bw)
	if err := enc.Encode(cacheHeader{Version: SpecVersion, Root: root, Files: len(recs)}); err != nil {
		return err
	}
	for _, r := range recs {
		r.Functions = withoutBaseline(r.Functions)
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// withoutBaseline returns fns with every Baseline cleared — fns itself when
// none is set, a copy otherwise (the live slice is shared with the index).
func withoutBaseline(fns []Function) []Function {
	for i := range fns {
		if fns[i].Baseline != BaselineNew {
			out := append([]Function(nil), fns...)
			for j := range out {
				out[j].Baseline = BaselineNew
			}
			return out
		}
	}
	return fns
}
