package manager

import (
	"os"
	"path/filepath"

	"ike/internal/scratch"
)

// roots.go locates a workspace root for a file by walking up the directory tree
// until a configured root marker (go.mod, composer.json, pyproject.toml, .git, …)
// is found. The root is the key (together with language) under which a server is
// shared, so all files in one project talk to one server instance. A scratch
// file is the one documented exception (#2612): it is attached to the active
// project root instead of the root its own location detects — see rootFor.

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

// rootFor returns the workspace root a document is served under: normally the
// root detected from the file's own location, but the **active project root**
// for a scratch file (#2612). A scratch lives under the user state dir
// (`~/.ike/scratches`), so walking upwards from it never reaches the project:
// it would get a second server rooted at the scratch store and analysed
// against the system toolchain, where a project dependency reads as an
// unresolved import. Attaching it to the project root instead puts it on the
// very server the project's files talk to — with the project's interpreter,
// module cache and settings — which is the rule `run.file` already follows for
// running a scratch (wiki/architecture/scratch-files.md).
//
// The rule is deliberately narrow: only a file in the scratch store moves, and
// only when it lies outside the project root. Any other file opened from
// elsewhere on disk keeps its detected root, so a file belonging to another
// project still gets that project's server.
func (m *Manager) rootFor(path string, markers []string) string {
	if root := m.ProjectRoot(); root != "" && !underRoot(path, root) && scratch.IsScratch(path) {
		return root
	}
	return detectRoot(path, markers)
}

// SetProjectRoot tells the manager which directory the active workspace is
// rooted at (#2612); "" clears it. The manager uses it for nothing but
// attaching out-of-tree scratch files to the project's server (rootFor), so a
// manager that is never told keeps the plain detectRoot behaviour.
func (m *Manager) SetProjectRoot(root string) {
	if root != "" {
		if abs, err := filepath.Abs(root); err == nil {
			root = abs
		}
		root = filepath.Clean(root)
	}
	m.mu.Lock()
	m.projectRoot = root
	m.mu.Unlock()
}

// ProjectRoot returns the active workspace root the manager was told about, ""
// when it was never told.
func (m *Manager) ProjectRoot() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.projectRoot
}
