package archive

// compress.go is the write direction (#2805): the explorer's "Compress (gzip)"
// and "Compress (zip)" actions. Both stay deliberately plain — the standard
// library's default level, no options — so the result is predictable:
//
//   - WriteGzip packs one regular file into a .gz beside it, the original kept;
//   - WriteZip packs files and directory trees into a .zip whose member names
//     are relative to the sources' parent directory (compressing ./src yields
//     src/main.go, never an absolute path). Symlinks are not followed and not
//     stored — the extractor refuses link entries anyway — and neither are
//     sockets, devices or fifos; each is counted as skipped.
//
// Both write into a temporary file in the destination directory and rename it
// into place only once the archive is complete, so a failed or interrupted run
// never leaves a half-written archive under the target name, and an existing
// target is replaced atomically (the caller asks before that happens).

import (
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// PackResult reports what a compression wrote.
type PackResult struct {
	// Files is the number of regular files packed, Bytes their total
	// uncompressed size.
	Files int
	Bytes int64
	// Skipped counts the entries left out: symlinks and special files.
	Skipped int
}

// WriteGzip compresses the regular file src into dest (conventionally
// src + ".gz") at the default level. The gzip header carries the original
// name and modification time, as gzip(1) writes them.
func WriteGzip(src, dest string) (PackResult, error) {
	st, err := os.Stat(src)
	if err != nil {
		return PackResult{}, err
	}
	if !st.Mode().IsRegular() {
		return PackResult{}, fmt.Errorf("%s is not a regular file", filepath.Base(src))
	}
	in, err := os.Open(src)
	if err != nil {
		return PackResult{}, err
	}
	defer in.Close()
	var n int64
	err = writeAtomic(dest, func(w io.Writer) error {
		zw := gzip.NewWriter(w)
		zw.Name = filepath.Base(src)
		zw.ModTime = st.ModTime()
		var err error
		if n, err = io.Copy(zw, in); err != nil {
			return err
		}
		return zw.Close()
	})
	if err != nil {
		return PackResult{}, err
	}
	return PackResult{Files: 1, Bytes: n}, nil
}

// WriteZip packs srcs — files and directories, walked recursively — into the
// zip dest. Member names are slash-separated and relative to each source's
// parent directory, so a directory keeps its own name as the top-level folder.
// dest itself is never packed, even when it sits inside a walked tree.
func WriteZip(srcs []string, dest string) (PackResult, error) {
	var res PackResult
	absDest, _ := filepath.Abs(dest)
	err := writeAtomic(dest, func(w io.Writer) error {
		zw := zip.NewWriter(w)
		for _, src := range srcs {
			base := filepath.Dir(src)
			// WalkDir never follows symlinks: a linked directory is reported
			// as a symlink entry, which is skipped below.
			err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if abs, _ := filepath.Abs(p); abs == absDest || isTempOf(abs, absDest) {
					return nil
				}
				rel, err := filepath.Rel(base, p)
				if err != nil {
					return err
				}
				name := filepath.ToSlash(rel)
				info, err := d.Info()
				if err != nil {
					return err
				}
				switch {
				case d.IsDir():
					hdr, err := zip.FileInfoHeader(info)
					if err != nil {
						return err
					}
					hdr.Name = name + "/"
					_, err = zw.CreateHeader(hdr)
					return err
				case !info.Mode().IsRegular():
					res.Skipped++
					return nil
				}
				n, err := zipFile(zw, p, name, info)
				if err != nil {
					return err
				}
				res.Files++
				res.Bytes += n
				return nil
			})
			if err != nil {
				return err
			}
		}
		return zw.Close()
	})
	if err != nil {
		return PackResult{}, err
	}
	return res, nil
}

// zipFile adds one regular file as a deflated member.
func zipFile(zw *zip.Writer, p, name string, info fs.FileInfo) (int64, error) {
	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return 0, err
	}
	hdr.Name = name
	hdr.Method = zip.Deflate
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return 0, err
	}
	f, err := os.Open(p)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return io.Copy(w, f)
}

// tempPattern names the in-progress file writeAtomic creates next to dest.
const tempPattern = ".ike-pack-*.tmp"

// isTempOf reports whether abs is one of writeAtomic's temporary files for
// absDest — the archive being written must not pack itself.
func isTempOf(abs, absDest string) bool {
	ok, _ := filepath.Match(tempPattern, filepath.Base(abs))
	return ok && filepath.Dir(abs) == filepath.Dir(absDest)
}

// writeAtomic runs write against a temporary file beside dest and renames it
// over dest once write succeeded; on failure the temporary file is removed and
// dest is untouched.
func writeAtomic(dest string, write func(io.Writer) error) error {
	if st, err := os.Stat(dest); err == nil && st.IsDir() {
		return fmt.Errorf("%s is a directory", filepath.Base(dest))
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), tempPattern)
	if err != nil {
		return err
	}
	name := tmp.Name()
	if err := write(tmp); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, dest); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
