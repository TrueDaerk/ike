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
// creates it empty — and the directory when missing — and returns the
// absolute path. The extension is dot-optional; empty means "txt".
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
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing scratches: %w", err)
	}
	type item struct {
		path string
		mod  int64
	}
	var items []item
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		items = append(items, item{filepath.Join(dir, e.Name()), info.ModTime().UnixNano()})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].mod != items[j].mod {
			return items[i].mod > items[j].mod
		}
		return items[i].path < items[j].path // stable order for equal times
	})
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.path
	}
	return out, nil
}
