package manager

import (
	"os"
	"path/filepath"
)

// roots.go locates a workspace root for a file by walking up the directory tree
// until a configured root marker (go.mod, composer.json, pyproject.toml, .git, …)
// is found. The root is the key (together with language) under which a server is
// shared, so all files in one project talk to one server instance.

// detectRoot returns the nearest ancestor directory of path containing any of
// markers. With no marker found (or none configured), it falls back to the
// file's own directory, so a server still starts for a loose file.
func detectRoot(path string, markers []string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	start := filepath.Dir(abs)
	dir := start
	for {
		for _, mk := range markers {
			if mk == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(dir, mk)); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached filesystem root
		}
		dir = parent
	}
	return start
}
