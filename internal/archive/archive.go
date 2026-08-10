// Package archive reads archive files for the archive viewer pane (#1762):
// it sniffs whether a file is an archive, lists its entries, and extracts one
// entry into memory. Everything goes through the standard library —
// archive/tar plus compress/gzip and compress/bzip2 — so no dependency is
// added for a viewer.
//
// The package is deliberately format-shaped rather than tar-shaped: Format
// classifies a file and the rest of IKE speaks only of "archives", so a later
// zip reader slots in beside FormatTar without renaming the pane, the pane
// kind, or the UI strings.
package archive

import (
	"archive/tar"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"time"
)

// Format classifies an archive file.
type Format int

const (
	// FormatNone means the file is not a recognised archive.
	FormatNone Format = iota
	// FormatTar is an uncompressed tar.
	FormatTar
	// FormatTarGz is a gzip-compressed tar (.tar.gz, .tgz).
	FormatTarGz
	// FormatTarBz2 is a bzip2-compressed tar (.tar.bz2, .tbz2).
	FormatTarBz2
)

// String names the format for the pane header.
func (f Format) String() string {
	switch f {
	case FormatTar:
		return "tar"
	case FormatTarGz:
		return "tar.gz"
	case FormatTarBz2:
		return "tar.bz2"
	}
	return ""
}

// blockSize is the tar header block size; a sniff needs exactly one.
const blockSize = 512

// maxEntries caps a listing so a hostile or absurd archive cannot exhaust
// memory. A truncated listing is reported, never silently shortened.
const maxEntries = 50_000

// ErrTooLarge is returned by ReadEntry when the entry crosses the caller's
// byte limit (the large-file thresholds, #149).
var ErrTooLarge = errors.New("archive entry exceeds the large-file limit")

// ErrNotFound is returned by ReadEntry when the archive holds no such entry.
var ErrNotFound = errors.New("archive entry not found")

// Entry is one listed member of an archive.
type Entry struct {
	// Name is the entry's path inside the archive, slash-separated and
	// without a trailing slash on directories.
	Name    string
	Size    int64
	Mode    fs.FileMode
	ModTime time.Time
	IsDir   bool
	// Link is the target of a symlink or hard link entry ("" otherwise); the
	// pane shows it instead of a size.
	Link string
}

// Listing is the result of List: the entries plus whether the cap truncated
// them.
type Listing struct {
	Format    Format
	Entries   []Entry
	Truncated bool
}

// Detect classifies path from its leading bytes, opening the file itself when
// the answer needs decompressed content. head is the caller's sniff buffer
// (internal/app's readHead, 512 bytes) and may be short or nil.
//
// A compressed file is only claimed when a tar header sits *inside* it: a
// plain app.log.gz is FormatNone and stays with the gz handler, which is the
// coordination point between the two viewers (#1762).
func Detect(path string, head []byte) Format {
	switch {
	case isGzip(head):
		if innerIsTar(path, func(r io.Reader) (io.Reader, error) { return gzip.NewReader(r) }) {
			return FormatTarGz
		}
	case isBzip2(head):
		if innerIsTar(path, func(r io.Reader) (io.Reader, error) { return bzip2.NewReader(r), nil }) {
			return FormatTarBz2
		}
	case looksLikeTar(head):
		return FormatTar
	}
	return FormatNone
}

// IsArchive reports whether Detect claims path — the file handler's Match.
func IsArchive(path string, head []byte) bool { return Detect(path, head) != FormatNone }

// isGzip reports the gzip magic.
func isGzip(head []byte) bool { return bytes.HasPrefix(head, []byte{0x1f, 0x8b}) }

// isBzip2 reports the bzip2 magic ("BZh" plus the block-size digit).
func isBzip2(head []byte) bool {
	return len(head) >= 4 && bytes.HasPrefix(head, []byte("BZh")) && head[3] >= '1' && head[3] <= '9'
}

// innerIsTar decompresses the first block of path through wrap and checks it
// for a tar header. It reads at most one block of output, so a 10 GB archive
// costs the same as a 10 KB one.
func innerIsTar(p string, wrap func(io.Reader) (io.Reader, error)) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	r, err := wrap(f)
	if err != nil {
		return false
	}
	block := make([]byte, blockSize)
	n, err := io.ReadFull(r, block)
	if err != nil && n < blockSize {
		return false
	}
	return looksLikeTar(block[:n])
}

// looksLikeTar reports whether block is a tar header: the POSIX/GNU "ustar"
// magic at offset 257, or — for headerless v7 tars — a header checksum that
// verifies. The checksum test is what tar itself uses, so a v7 archive with
// no magic is still recognised while random 512-byte prefixes are not.
func looksLikeTar(block []byte) bool {
	if len(block) < blockSize {
		return false
	}
	if bytes.HasPrefix(block[257:], []byte("ustar")) {
		return true
	}
	return checksumOK(block)
}

// checksumOK verifies a tar header's octal checksum field (offset 148, 8
// bytes) against the sum of the block with that field read as spaces. Both
// the unsigned and the historic signed sum are accepted.
func checksumOK(block []byte) bool {
	want, ok := parseOctal(block[148:156])
	if !ok {
		return false
	}
	var unsigned, signed int64
	for i, b := range block[:blockSize] {
		if i >= 148 && i < 156 {
			b = ' '
		}
		unsigned += int64(b)
		signed += int64(int8(b))
	}
	return want == unsigned || want == signed
}

// parseOctal reads a tar numeric field: leading spaces/NULs, octal digits, a
// terminating space or NUL. An all-blank field is not a number.
func parseOctal(f []byte) (int64, bool) {
	s := strings.Trim(string(f), " \x00")
	if s == "" {
		return 0, false
	}
	var n int64
	for _, c := range s {
		if c < '0' || c > '7' {
			return 0, false
		}
		n = n*8 + int64(c-'0')
	}
	return n, true
}

// reader opens path and returns the decompressed tar stream plus a closer for
// the whole chain.
func reader(p string) (*tar.Reader, io.Closer, Format, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, nil, FormatNone, err
	}
	head := make([]byte, blockSize)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return nil, nil, FormatNone, err
	}
	var src io.Reader = f
	format := FormatTar
	switch {
	case isGzip(head):
		gz, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			return nil, nil, FormatNone, err
		}
		src, format = gz, FormatTarGz
	case isBzip2(head):
		src, format = bzip2.NewReader(f), FormatTarBz2
	}
	return tar.NewReader(src), f, format, nil
}

// List reads every entry header of the archive at p. A truncated or corrupt
// archive returns the entries read so far together with the error, so the
// pane can show what it got plus the failure instead of nothing.
func List(p string) (Listing, error) {
	tr, closer, format, err := reader(p)
	if err != nil {
		return Listing{}, err
	}
	defer closer.Close()
	out := Listing{Format: format}
	for {
		if len(out.Entries) >= maxEntries {
			out.Truncated = true
			return out, nil
		}
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return out, fmt.Errorf("read archive: %w", err)
		}
		if e, ok := entryOf(h); ok {
			out.Entries = append(out.Entries, e)
		}
	}
}

// entryOf converts a tar header into an Entry, skipping the metadata-only
// records (PAX headers, GNU long-name blocks) the tar reader already folded
// into the following entry.
func entryOf(h *tar.Header) (Entry, bool) {
	switch h.Typeflag {
	case tar.TypeXHeader, tar.TypeXGlobalHeader, tar.TypeGNULongName, tar.TypeGNULongLink:
		return Entry{}, false
	}
	name := path.Clean(strings.TrimPrefix(strings.ReplaceAll(h.Name, "\\", "/"), "./"))
	if name == "." || name == "/" || name == "" {
		return Entry{}, false
	}
	name = strings.TrimSuffix(name, "/")
	e := Entry{
		Name:    name,
		Size:    h.Size,
		Mode:    h.FileInfo().Mode(),
		ModTime: h.ModTime,
		IsDir:   h.Typeflag == tar.TypeDir,
		Link:    h.Linkname,
	}
	if e.IsDir {
		e.Size = 0
	}
	return e, true
}

// ReadEntry extracts the named entry of the archive at p into memory. limit
// caps the decompressed size (0 or negative disables the cap): an entry over
// it returns ErrTooLarge before any of it is buffered, so the large-file
// policy holds for archive members too (#149).
func ReadEntry(p, name string, limit int64) ([]byte, error) {
	tr, closer, _, err := reader(p)
	if err != nil {
		return nil, err
	}
	defer closer.Close()
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		e, ok := entryOf(h)
		if !ok || e.Name != name {
			continue
		}
		if e.IsDir {
			return nil, ErrNotFound
		}
		if limit > 0 && e.Size > limit {
			return nil, ErrTooLarge
		}
		// The header size is untrusted for a corrupt archive: read one byte
		// past the limit and fail rather than buffering the whole stream.
		var src io.Reader = tr
		if limit > 0 {
			src = io.LimitReader(tr, limit+1)
		}
		data, err := io.ReadAll(src)
		if err != nil {
			return nil, fmt.Errorf("read archive entry: %w", err)
		}
		if limit > 0 && int64(len(data)) > limit {
			return nil, ErrTooLarge
		}
		return data, nil
	}
}
