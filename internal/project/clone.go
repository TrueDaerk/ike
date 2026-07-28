package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// clone.go is the project-side of "Clone Repository" (#1349): the message that
// opens the dialog and the target resolution. The git subprocess lives in
// internal/vcs (CloneCmd); the dialog and the open-after-clone routing live in
// internal/app. This package only decides *where* a clone may land.

// OpenCloneMsg asks the root model to open the clone-repository dialog.
// Dispatched by the project.clone command.
type OpenCloneMsg struct{}

// CloneTarget resolves the directory a clone named name would land in: the
// project directory (created on demand) joined with name. It rejects an empty
// name, a name that is not a single path segment, and a target that already
// exists with content — so a clone never lands inside an unrelated checkout
// and never half-merges into an existing directory. An existing *empty*
// directory is refused too: git clone requires a free path, and silently
// removing a directory the user created is not this feature's call.
func CloneTarget(name string) (string, error) {
	n := strings.TrimSpace(name)
	if n == "" {
		return "", fmt.Errorf("directory name is empty — enter a name for the clone")
	}
	if n == "." || n == ".." {
		return "", fmt.Errorf("%q is not a directory name", n)
	}
	if strings.ContainsRune(n, '/') || strings.ContainsRune(n, filepath.Separator) {
		return "", fmt.Errorf("%q contains a path separator — enter a plain directory name", n)
	}

	dir, err := EnsureDirectory()
	if err != nil {
		return "", err
	}
	dest := filepath.Join(dir, n)
	if _, err := os.Stat(dest); err == nil {
		return "", fmt.Errorf("%s already exists — choose another name", dest)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("cannot access %s: %v", dest, err)
	}
	return dest, nil
}
