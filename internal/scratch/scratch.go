// Package scratch owns the storage of scratch files (Roadmap 0280): quick
// throwaway buffers for notes, JSON snippets, regex tests, JetBrains-style.
// Scratches are ordinary files under the user state dir — never the project
// tree — so they are language-aware through their extension, survive restarts
// via the normal session mechanics, and need no special buffer type. This
// package is the single owner of scratch naming and location; the app never
// assembles scratch paths itself.
package scratch

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ike/internal/lang"
)

// configDirEnv mirrors config.Discover's user-layer override, so a sandboxed
// IKE (tests, power users) keeps its scratches in the sandbox too.
const configDirEnv = "IKE_CONFIG_DIR"

// Dir resolves the scratch directory: $IKE_CONFIG_DIR/scratches when the
// override is set, else ~/.ike/scratches. An undiscoverable home yields an
// error rather than scattering files into a relative path.
func Dir() (string, error) {
	if d := os.Getenv(configDirEnv); d != "" {
		return filepath.Join(d, "scratches"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving scratch dir: %w", err)
	}
	return filepath.Join(home, ".ike", "scratches"), nil
}

// Create allocates the next free scratch-N.<ext> (N counting up from 1),
// creates it — and the directory when missing — seeded with the language's
// file template (#1223: a PHP scratch opens with "<?php", so it is runnable
// as created), and returns the absolute path. Languages without a template
// yield an empty file. The extension is dot-optional; empty means "txt".
func Create(ext string) (string, error) { return create(ext, nil) }

// CreateWithContent allocates a scratch like Create but seeds it with content
// instead of the language template (#2134: the test-data generator writes a
// finished CSV/JSON/… document). Allocation stays race-free and the content is
// still written through the winning handle, so a generated scratch can never
// land in a file another creator got first. A nil content is Create.
func CreateWithContent(ext string, content []byte) (string, error) {
	if content == nil {
		content = []byte{}
	}
	return create(ext, content)
}

// create is the single allocation path behind Create and CreateWithContent:
// a nil seed means "use the language template".
func create(ext string, seed []byte) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating scratch dir: %w", err)
	}
	ext = strings.TrimPrefix(ext, ".")
	if ext == "" {
		ext = "txt"
	}
	for n := 1; ; n++ {
		path := filepath.Join(dir, fmt.Sprintf("scratch-%d.%s", n, ext))
		// O_EXCL makes allocation race-free: the first creator wins.
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			// The seed is written through the winning handle, so the content
			// belongs to the allocation that won the O_EXCL race.
			data := seed
			if data == nil {
				data = []byte(lang.TemplateFor(path))
			}
			if len(data) > 0 {
				if _, err := f.Write(data); err != nil {
					f.Close()
					return "", fmt.Errorf("seeding scratch content: %w", err)
				}
			}
			f.Close()
			return path, nil
		}
		if !os.IsExist(err) {
			return "", fmt.Errorf("creating scratch: %w", err)
		}
	}
}

// List returns the existing scratch files newest-first by modification time.
// A missing directory is an empty list, not an error.
func List() ([]string, error) {
	entries, err := Entries()
	if err != nil {
		return nil, err
	}
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Path
	}
	return out, nil
}

// Entry is one scratch file: its absolute path plus the modification time the
// listing sorts on plus the file's byte size. The scratch-files panel (#1932)
// renders name, extension and age from it and the scratch manager (#2256) its
// size, so nothing outside this package stats the scratch dir.
type Entry struct {
	Path    string
	ModTime time.Time
	Size    int64
}

// Entries is List with the mod times kept: the existing scratch files
// newest-first, ties broken by path so the order is stable. A missing
// directory is an empty list, not an error; an entry that vanished between
// the directory read and its stat is skipped.
func Entries() ([]Entry, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	des, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing scratches: %w", err)
	}
	var out []Entry
	for _, e := range des {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Entry{
			Path:    filepath.Join(dir, e.Name()),
			ModTime: info.ModTime(),
			Size:    info.Size(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].ModTime.Equal(out[j].ModTime) {
			return out[i].ModTime.After(out[j].ModTime)
		}
		return out[i].Path < out[j].Path // stable order for equal times
	})
	return out, nil
}

// firstLineScanCap bounds how much of a scratch FirstLine reads before giving
// up: a large scratch (a pasted log, a JSON export) yields a title from its
// head without paying for a full read, matching the picker's "don't load the
// whole file" requirement (#2057).
const firstLineScanCap = 64 * 1024

// FirstLine returns path's first non-empty, whitespace-trimmed line — the
// scratch picker's title source (#2057), read lazily and capped at
// firstLineScanCap bytes rather than loading the whole file. "" when the file
// is empty, all-whitespace within the cap, or unreadable; callers render
// that as a placeholder.
func FirstLine(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(io.LimitReader(f, firstLineScanCap))
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			return line
		}
	}
	return ""
}

// Delete removes one scratch file (#1932). The path must name a file directly
// inside the scratch dir — anything else (a nested path, a directory, a
// traversal through "..") is refused rather than deleted, so the panel's
// delete action can never reach outside the store this package owns.
func Delete(path string) error {
	abs, err := inStore(path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return fmt.Errorf("deleting scratch: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("not a scratch file: %s", path)
	}
	if err := os.Remove(abs); err != nil {
		return fmt.Errorf("deleting scratch: %w", err)
	}
	return nil
}

// Rename gives one scratch file a new base name and returns the new absolute
// path (#1963, the explorer section's rename). Both ends are guarded like
// Delete: path must name a file directly inside the scratch dir, and name must
// be a bare file name — a separator or a ".."/"." component would let a rename
// walk the file out of the store. An existing target is refused rather than
// overwritten.
func Rename(path, name string) (string, error) {
	abs, err := inStore(path)
	if err != nil {
		return "", err
	}
	if name == "" || name != filepath.Base(name) || name == "." || name == ".." ||
		strings.ContainsRune(name, filepath.Separator) || strings.ContainsRune(name, '/') {
		return "", fmt.Errorf("not a valid scratch name: %s", name)
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", fmt.Errorf("renaming scratch: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("not a scratch file: %s", path)
	}
	target := filepath.Join(filepath.Dir(abs), name)
	if target == abs {
		return abs, nil
	}
	if _, err := os.Lstat(target); err == nil {
		return "", fmt.Errorf("already exists: %s", name)
	}
	if err := os.Rename(abs, target); err != nil {
		return "", fmt.Errorf("renaming scratch: %w", err)
	}
	return target, nil
}

// SetExt re-languages one scratch (#2256): it keeps the file's stem and gives
// it ext instead — "scratch-1.txt" with ext "py" becomes "scratch-1.py" — and
// returns the new absolute path. Everything language-aware in the editor flows
// from the extension, so swapping it *is* the language change; the guards are
// Rename's, since that is what it runs, and an existing target is refused
// rather than overwritten. The extension is dot-optional and empty means
// "txt", exactly like Create's; a scratch whose name is all extension
// (".env") keeps its whole name as the stem.
func SetExt(path, ext string) (string, error) {
	abs, err := inStore(path)
	if err != nil {
		return "", err
	}
	ext = strings.TrimPrefix(ext, ".")
	if ext == "" {
		ext = "txt"
	}
	base := filepath.Base(abs)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if stem == "" {
		stem = base
	}
	return Rename(abs, stem+"."+ext)
}

// IsScratch reports whether path names a file directly inside the scratch
// store. It is the predicate behind "promote this scratch" (#2339): a command
// that only applies to a store file must be able to say so before acting,
// instead of failing inside the move.
func IsScratch(path string) bool {
	if path == "" {
		return false
	}
	_, err := inStore(path)
	return err == nil
}

// Promote moves one scratch out of the store to target (#2339): the exit the
// store was missing, for a scratch that turned into something worth keeping.
// The source must be a file directly inside the store — the same boundary
// Delete and Rename enforce — and target must lie outside it, because moving
// a scratch to another scratch name is a Rename, not a promotion. An existing
// target is refused rather than overwritten, mirroring Rename; missing parent
// directories of target are created.
//
// The move is os.Rename when it can be (atomic, keeps the inode) and a
// copy-then-remove otherwise, since the store lives under the user state dir
// and the project tree may well be on a different filesystem. The source is
// only removed once the copy is durably written, so a failing write can never
// leave the scratch half-promoted — a store entry gone with no file to show
// for it is exactly the failure mode this ordering rules out.
func Promote(path, target string) error {
	abs, err := inStore(path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return fmt.Errorf("promoting scratch: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("not a scratch file: %s", path)
	}
	if target == "" {
		return fmt.Errorf("promoting scratch: the target path is required")
	}
	dst, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolving target path: %w", err)
	}
	dir, err := Dir()
	if err != nil {
		return err
	}
	if filepath.Dir(dst) == filepath.Clean(dir) {
		return fmt.Errorf("target is inside the scratch store: %s", target)
	}
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("already exists: %s", target)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("creating target directory: %w", err)
	}
	if err := os.Rename(abs, dst); err == nil {
		return nil
	}
	// Cross-device (or any other rename refusal): copy, flush, then unlink.
	if err := copyFile(abs, dst, info.Mode().Perm()); err != nil {
		os.Remove(dst) // never leave a partial file behind
		return err
	}
	if err := os.Remove(abs); err != nil {
		return fmt.Errorf("removing promoted scratch: %w", err)
	}
	return nil
}

// copyFile writes src to dst with perm, flushing to disk before returning so
// Promote can remove the source knowing the copy survived.
func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("reading scratch: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return fmt.Errorf("writing target file: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("writing target file: %w", err)
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return fmt.Errorf("writing target file: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("writing target file: %w", err)
	}
	return nil
}

// inStore resolves path to absolute and verifies it names an entry directly
// inside the scratch dir — the shared boundary guard of Delete and Rename.
func inStore(path string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving scratch path: %w", err)
	}
	if filepath.Dir(abs) != filepath.Clean(dir) {
		return "", fmt.Errorf("not a scratch file: %s", path)
	}
	return abs, nil
}
