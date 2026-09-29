package gzfile

// extract.go decompresses a plain gzip file onto disk (#2805) — the explorer's
// "Extract here" / "Extract to…" on app.log.gz. It is the streaming twin of
// Read: the same decompressed-byte cap applies, because a gzip bomb is just as
// unbounded on disk as it is in memory, and the output is written to a
// temporary file that only replaces the target once the stream ended inside
// the cap.

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"ike/internal/archive"
)

// ErrExtractTooLarge is returned by Extract when the decompressed stream
// crosses the cap. It is the archive extractor's error, so callers report both
// paths with one message.
var ErrExtractTooLarge = archive.ErrExtractTooLarge

// ExtractName is the name the decompressed file gets: the .gz (or .gzip)
// suffix stripped — app.log.gz becomes app.log. A gzip file without that
// suffix has no name to fall back to that could not collide with itself, so
// it gains ".out".
func ExtractName(path string) string {
	base := filepath.Base(path)
	if ext := filepath.Ext(base); strings.EqualFold(ext, ".gz") || strings.EqualFold(ext, ".gzip") {
		if stem := strings.TrimSuffix(base, ext); stem != "" {
			return stem
		}
	}
	return base + ".out"
}

// Extract decompresses the gzip file src into dest, writing at most limit
// bytes (0 or negative disables the cap). Past the cap nothing is left behind
// and ErrExtractTooLarge is returned; an existing dest is replaced only once
// the whole stream decompressed — the caller asks before that.
func Extract(src, dest string, limit int64) (int64, error) {
	if st, err := os.Stat(dest); err == nil && st.IsDir() {
		return 0, fmt.Errorf("%s is a directory", filepath.Base(dest))
	}
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	zr, err := gzip.NewReader(in)
	if err != nil {
		return 0, fmt.Errorf("read gzip: %w", err)
	}
	defer zr.Close()

	tmp, err := os.CreateTemp(filepath.Dir(dest), ".ike-gunzip-*.tmp")
	if err != nil {
		return 0, err
	}
	name := tmp.Name()
	fail := func(err error) (int64, error) {
		tmp.Close()
		os.Remove(name)
		return 0, err
	}
	var r io.Reader = zr
	if limit > 0 {
		// One byte past the cap separates "exactly at it" from "over it".
		r = io.LimitReader(zr, limit+1)
	}
	n, err := io.Copy(tmp, r)
	if err != nil {
		return fail(fmt.Errorf("read gzip: %w", err))
	}
	if limit > 0 && n > limit {
		return fail(ErrExtractTooLarge)
	}
	if err := tmp.Close(); err != nil {
		return fail(err)
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return fail(err)
	}
	if err := os.Rename(name, dest); err != nil {
		return fail(err)
	}
	if !zr.ModTime.IsZero() {
		_ = os.Chtimes(dest, zr.ModTime, zr.ModTime)
	}
	return n, nil
}
