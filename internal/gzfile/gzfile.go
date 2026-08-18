// Package gzfile reads plain gzip files for the transparent gz viewer
// (#1763): a lone `app.log.gz` holds one compressed file whose natural
// viewing form is the decompressed text, so IKE opens that text in a
// read-only buffer instead of a pane of its own.
//
// The package deliberately claims *only* plain gzip. A gzip stream wrapping a
// tar belongs to the archive viewer (#1762), which lists its members; the
// split lives in IsPlain, the single routing decision both handlers agree on.
//
// Everything goes through the standard library (compress/gzip), and every
// read is bounded by a decompressed-byte cap — a gzip bomb is a few kilobytes
// on disk and unbounded in memory, so the compressed size is never a budget.
package gzfile

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"ike/internal/archive"
)

// magic is the gzip header's leading pair.
var magic = []byte{0x1f, 0x8b}

// tarSuffixes are the compound extensions naming a compressed tar. They are
// checked by name as well as by content: a file called backup.tar.gz belongs
// to the archive viewer even when its payload does not sniff as tar, because
// the name is what the user asked for.
var tarSuffixes = []string{".tar.gz", ".tgz", ".tar.bz2", ".tbz", ".tbz2", ".tar.z"}

// Content is a decompressed gzip file held in memory.
type Content struct {
	// Name is the inner file's name — the `.gz` suffix stripped from the
	// archive's own name, or the gzip header's original-name field.
	Name string
	// Data is the decompressed bytes, capped at the caller's limit.
	Data []byte
	// Compressed is the size of the .gz file on disk.
	Compressed int64
	// Original is the decompressed size claimed by the gzip footer, and
	// OriginalOK whether that claim was readable. The footer records the size
	// modulo 2^32, so it is metadata for the notice — never a budget.
	Original   int64
	OriginalOK bool
	// Truncated reports that the cap stopped the read before the stream ended.
	Truncated bool
}

// Ratio is the compression ratio for the metadata notice, from the footer's
// original size when it is trustworthy and from the bytes read otherwise. A
// zero compressed size (or a missing footer on a truncated read) yields 0.
func (c Content) Ratio() float64 {
	size := int64(len(c.Data))
	if c.OriginalOK {
		size = c.Original
	} else if c.Truncated {
		return 0
	}
	if c.Compressed <= 0 {
		return 0
	}
	return float64(size) / float64(c.Compressed)
}

// IsGzip reports the gzip magic in head, which may be short or nil.
func IsGzip(head []byte) bool { return bytes.HasPrefix(head, magic) }

// IsPlain reports whether path is a gzip file this viewer claims — the file
// handler's Match. It is the exact complement of the archive viewer's claim:
// a gzip stream holding a tar, or merely *named* like one, is left to the
// archive pane (#1762), so exactly one of the two handlers ever answers.
func IsPlain(path string, head []byte) bool {
	if !IsGzip(head) {
		return false
	}
	if HasTarSuffix(path) {
		return false
	}
	return !archive.IsArchive(path, head)
}

// IsNestedArchive reports whether a decompressed gzip stream named name is
// itself an archive: the compound name says so, or its first block holds a
// tar header. IKE has no nested archive view, so the archive pane says so
// plainly instead of opening tar blocks as text (#1948).
func IsNestedArchive(name string, data []byte) bool {
	return HasTarSuffix(name) || archive.LooksLikeTar(data)
}

// HasTarSuffix reports whether path's name declares a compressed tar
// (.tar.gz, .tgz, …). The archive viewer asks it of its own members too: a
// nested inner.tar.gz is named an archive whatever its payload turns out to
// be (#1948).
func HasTarSuffix(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	for _, s := range tarSuffixes {
		if strings.HasSuffix(name, s) {
			return true
		}
	}
	return false
}

// InnerName resolves the name of the file inside the archive, which decides
// the language and highlighting of the buffer. Extension stripping comes
// first — app.log.gz is a log whatever the header says — and the gzip
// header's optional original-name field is the fallback for a name that
// carries no `.gz` suffix. A name yielding neither keeps its own base name.
//
// The one case where the header beats the stripped name is when stripping
// leaves no extension behind: dump.gz says nothing about its content, so a
// header naming dump.sql is the only thing that can give the buffer a
// language (#1853). A header without an extension of its own never wins —
// it would trade one anonymous name for another.
func InnerName(path, header string) string {
	base := filepath.Base(path)
	header = strings.TrimSpace(filepath.Base(strings.ReplaceAll(header, "\\", "/")))
	if header == "." || header == "/" {
		header = ""
	}
	stripped := ""
	if ext := filepath.Ext(base); strings.EqualFold(ext, ".gz") || strings.EqualFold(ext, ".gzip") {
		stripped = strings.TrimSuffix(base, ext)
	}
	if stripped != "" {
		if filepath.Ext(stripped) != "" || header == "" || filepath.Ext(header) == "" {
			return stripped
		}
		return header
	}
	if header != "" {
		return header
	}
	return base
}

// Read decompresses path into memory. limit caps the *decompressed* bytes (0
// or negative disables the cap): past it the read stops and Truncated is set,
// so a decompression bomb costs one limit-sized buffer instead of the host's
// memory. The returned Content always carries Name, Compressed and the footer
// metadata, so a caller refusing to show the content can still describe it.
func Read(path string, limit int64) (Content, error) {
	f, err := os.Open(path)
	if err != nil {
		return Content{}, err
	}
	defer f.Close()

	c := Content{Name: InnerName(path, "")}
	if st, err := f.Stat(); err == nil {
		c.Compressed = st.Size()
	}
	c.Original, c.OriginalOK = footerSize(f, c.Compressed)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return c, err
	}
	return c, c.fill(f, path, limit)
}

// ReadBytes decompresses an in-memory gzip stream that never was a file of
// its own — an archive member extracted by the archive viewer (#1948). name
// is the member's path inside the archive and plays the role the file name
// plays in Read: it decides the inner name, and with it the language. Caps
// and footer metadata work exactly as they do there, so a gzip bomb packed
// inside a tar is bounded by the same single buffer.
func ReadBytes(name string, data []byte, limit int64) (Content, error) {
	c := Content{Name: InnerName(name, ""), Compressed: int64(len(data))}
	c.Original, c.OriginalOK = footerSize(bytes.NewReader(data), c.Compressed)
	return c, c.fill(bytes.NewReader(data), name, limit)
}

// fill decompresses r into c, resolving the inner name against the stream's
// header and honouring the decompressed-byte cap. It is the body Read and
// ReadBytes share: the only difference between a .gz on disk and a .gz inside
// a tar is where the bytes come from.
func (c *Content) fill(r io.Reader, name string, limit int64) error {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("read gzip: %w", err)
	}
	defer zr.Close()
	// The header name only matters when the file name gave nothing away;
	// InnerName decides which of the two wins.
	c.Name = InnerName(name, zr.Name)

	var src io.Reader = zr
	if limit > 0 {
		// One byte past the limit distinguishes "exactly at the cap" from
		// "more to come" without buffering the remainder.
		src = io.LimitReader(zr, limit+1)
	}
	data, err := io.ReadAll(src)
	if err != nil {
		// A truncated or corrupt tail still yields the bytes decompressed so
		// far: a partially written log is worth reading.
		if len(data) == 0 {
			return fmt.Errorf("read gzip: %w", err)
		}
		c.Truncated = true
	}
	if limit > 0 && int64(len(data)) > limit {
		data, c.Truncated = data[:limit], true
	}
	c.Data = data
	return nil
}

// footerSize reads the gzip footer's ISIZE field — the decompressed length
// modulo 2^32, in the last four bytes of the file. It is advisory: a
// multi-member or over-4 GiB stream reports the wrong number, which is why no
// allocation is ever sized from it.
func footerSize(r io.ReaderAt, size int64) (int64, bool) {
	if size < 18 { // smallest possible gzip: 10-byte header + 8-byte footer
		return 0, false
	}
	var buf [4]byte
	if _, err := r.ReadAt(buf[:], size-4); err != nil {
		return 0, false
	}
	return int64(binary.LittleEndian.Uint32(buf[:])), true
}
