// Package scratch owns the storage of scratch files (Roadmap 0280): quick
// throwaway buffers for notes, JSON snippets, regex tests, JetBrains-style.
// Scratches are ordinary files under the user state dir — never the project
// tree — so they are language-aware through their extension, survive restarts
// via the normal session mechanics, and need no special buffer type. This
// package is the single owner of scratch naming and location; the app never
// assembles scratch paths itself.
package scratch

import (
	"fmt"
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
func Create(ext string) (string, error) {
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
			// The template is written through the winning handle, so the
			// content belongs to the allocation that won the O_EXCL race.
			if tpl := lang.TemplateFor(path); tpl != "" {
				if _, err := f.WriteString(tpl); err != nil {
					f.Close()
					return "", fmt.Errorf("seeding scratch template: %w", err)
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
// listing sorts on. The scratch-files panel (#1932) renders name, extension
// and age from it, so nothing outside this package stats the scratch dir.
type Entry struct {
	Path    string
	ModTime time.Time
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
		out = append(out, Entry{Path: filepath.Join(dir, e.Name()), ModTime: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].ModTime.Equal(out[j].ModTime) {
			return out[i].ModTime.After(out[j].ModTime)
		}
		return out[i].Path < out[j].Path // stable order for equal times
	})
	return out, nil
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
