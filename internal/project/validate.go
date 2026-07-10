package project

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Validate resolves path to the absolute, cleaned directory a project could be
// rooted at, or explains why it cannot be one. It expands a leading `~`,
// makes the path absolute and rejects non-existent paths, non-directories and
// unreadable directories with actionable errors. Callers never partially
// switch: nothing is mutated here, the resolved root is only returned once
// every check passed.
func Validate(path string) (string, error) {
	p := strings.TrimSpace(path)
	if p == "" {
		return "", fmt.Errorf("project path is empty — enter a directory path")
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
		return "", fmt.Errorf("cannot resolve %q to an absolute path: %v", p, err)
	}
	abs = filepath.Clean(abs)

	info, err := os.Stat(abs)
	switch {
	case os.IsNotExist(err):
		return "", fmt.Errorf("%s does not exist — check the path", abs)
	case err != nil:
		return "", fmt.Errorf("cannot access %s: %v", abs, err)
	case !info.IsDir():
		return "", fmt.Errorf("%s is a file, not a directory — a project root must be a directory", abs)
	}

	// A project root must be listable: the explorer roots its tree here.
	f, err := os.Open(abs)
	if err != nil {
		return "", fmt.Errorf("%s is not readable: %v — check its permissions", abs, err)
	}
	defer f.Close()
	// An empty directory is a valid project root: Readdirnames reports io.EOF.
	if _, err := f.Readdirnames(1); err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("%s is not readable: %v — check its permissions", abs, err)
	}

	return abs, nil
}
