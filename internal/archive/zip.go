package archive

// zip.go is the zip half of the members seam (#2594). A zip is not a stream:
// its central directory sits at the end of the file and every member is
// opened on its own, so the walk is an index into that directory rather than
// a cursor through headers. Everything above the seam — List, ReadEntry,
// PlanExtract, Extract, the pane — is unaware of the difference.
//
// Two zip facts shape the code here:
//
//   - archive/zip decompresses store and deflate only. A member packed with
//     anything else (bzip2, lzma, zstd, or an unknown method number) must
//     fail on that member alone: the listing still shows it, and preview or
//     extraction reports ErrUnsupportedMethod instead of crashing.
//   - a directory member is a name ending in "/", and a zip may leave them
//     out entirely; the pane's tree builds the missing levels itself, the
//     same way it does for a tar that omits them.

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
)

// ErrUnsupportedMethod is returned when a zip member uses a compression
// method the standard library cannot decompress. It carries the method
// number, so the pane's notification names what is wrong with the file.
var ErrUnsupportedMethod = errors.New("unsupported zip compression method")

// maxLinkTarget caps the bytes read for a symlink member's target. A link
// target is a path; anything longer is not one, and the listing must not pull
// a gigabyte into memory to fill in one column.
const maxLinkTarget = 4096

// zipMembers walks the central directory of a zip archive.
type zipMembers struct {
	zr *zip.ReadCloser
	// i is the index of the current member; it starts at -1, before the
	// first, so next advances into the directory the way tar.Next does.
	i int
}

// next moves to the following member, skipping the directory records that
// carry no name of their own.
func (z *zipMembers) next() (Entry, error) {
	for {
		z.i++
		if z.i >= len(z.zr.File) {
			return Entry{}, io.EOF
		}
		if e, ok := z.entryOf(z.zr.File[z.i]); ok {
			return e, nil
		}
	}
}

// open returns the current member's content. An unreadable compression method
// is reported here rather than at listing time, so one exotic member costs
// the archive nothing but its own preview.
func (z *zipMembers) open() (io.ReadCloser, error) {
	if z.i < 0 || z.i >= len(z.zr.File) {
		return nil, ErrNotFound
	}
	f := z.zr.File[z.i]
	rc, err := f.Open()
	if errors.Is(err, zip.ErrAlgorithm) {
		return nil, fmt.Errorf("%w %d", ErrUnsupportedMethod, f.Method)
	}
	return rc, err
}

func (z *zipMembers) Close() error { return z.zr.Close() }

// entryOf converts a zip file header into an Entry, mirroring the tar side:
// the name is normalised the same way, and the fields zip does not carry
// (a hard-link target, an owner) stay zero.
func (z *zipMembers) entryOf(f *zip.File) (Entry, bool) {
	info := f.FileInfo()
	name := path.Clean(strings.TrimPrefix(strings.ReplaceAll(f.Name, "\\", "/"), "./"))
	if name == "." || name == "/" || name == "" {
		return Entry{}, false
	}
	name = strings.TrimSuffix(name, "/")
	e := Entry{
		Name: name,
		Size: int64(f.UncompressedSize64),
		Mode: info.Mode(),
		// Zip stores a local-time DOS timestamp unless the extended field is
		// present; Modified is the one that accounts for both.
		ModTime: f.Modified,
		IsDir:   info.IsDir(),
	}
	if e.IsDir {
		e.Size = 0
	}
	if e.Mode&fs.ModeSymlink != 0 {
		e.Link = z.linkTarget(f)
	}
	return e, true
}

// linkTarget reads a symlink member's target, which zip stores as the
// member's content. A member that cannot be read leaves the target empty —
// the entry is still listed, and it is skipped on extraction either way,
// because a link is never materialized (see extract.go).
func (z *zipMembers) linkTarget(f *zip.File) string {
	rc, err := f.Open()
	if err != nil {
		return ""
	}
	defer rc.Close()
	target, err := io.ReadAll(io.LimitReader(rc, maxLinkTarget))
	if err != nil {
		return ""
	}
	return string(target)
}
