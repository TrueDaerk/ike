package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ike/internal/config"
)

// directory.go resolves the project directory (#1348): the default parent for
// projects IKE creates itself, JetBrains' ~/IdeaProjects. Only features that
// place a new project there (project.clone) touch it — nothing here runs at
// startup, and the directory is created on demand, never speculatively.

// DefaultDirectory is the project directory used when [project] directory is
// unset. Relative to the user's home directory.
const DefaultDirectory = "IkeProjects"

// ProjectsDir returns the configured project directory as an absolute, cleaned
// path. An empty setting (or one that is only whitespace) selects
// ~/DefaultDirectory; a leading `~` is expanded; a relative path is resolved
// against the working directory. The directory need not exist.
func ProjectsDir() (string, error) {
	return resolveDir(config.Get().Project.Directory)
}

// resolveDir is ProjectsDir for an explicit setting value; split out so the
// resolution is testable without touching the global config.
func resolveDir(setting string) (string, error) {
	p := strings.TrimSpace(setting)
	if p == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot resolve the project directory: home directory unknown (%v)", err)
		}
		return filepath.Join(home, DefaultDirectory), nil
	}

	if p == "~" || strings.HasPrefix(p, "~"+string(filepath.Separator)) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot expand %q: home directory unknown (%v)", p, err)
		}
		p = filepath.Join(home, p[1:])
	}

	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("cannot resolve the project directory %q: %v", p, err)
	}
	return filepath.Clean(abs), nil
}

// EnsureDirectory resolves the project directory and creates it (and its
// parents) if it is missing, returning the absolute path. An existing
// directory is left untouched; an existing *file* at the path is an error, so
// callers never clone into something that is not a directory.
func EnsureDirectory() (string, error) {
	dir, err := ProjectsDir()
	if err != nil {
		return "", err
	}
	info, err := os.Stat(dir)
	switch {
	case err == nil && !info.IsDir():
		return "", fmt.Errorf("%s is a file, not a directory — change the project directory in the settings", dir)
	case err == nil:
		return dir, nil
	case !os.IsNotExist(err):
		return "", fmt.Errorf("cannot access the project directory %s: %v", dir, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create the project directory %s: %v", dir, err)
	}
	return dir, nil
}
