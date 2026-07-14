package langpython

import (
	"os"
	"path/filepath"
	"strings"

	"ike/internal/lang"
)

// Run-command contribution (0350, #575): how to run a Python file. The
// interpreter comes pre-resolved (venv/pyenv/explicit config); the fallback
// mirrors the detector's PATH order.

var (
	_ lang.RunCommandProvider = toolchain{}
	_ lang.ModuleResolver     = toolchain{}
)

// RunCommand implements lang.RunCommandProvider: `python -m package.module`
// when the spec carries a module spelling, else `python file.py`, with the
// program's own args appended.
func (toolchain) RunCommand(_ string, spec lang.RunSpec, interpreter string) ([]string, bool) {
	if interpreter == "" {
		interpreter = "python3"
	}
	argv := []string{interpreter}
	if spec.Module != "" {
		argv = append(argv, "-m", spec.Module)
	} else {
		argv = append(argv, spec.File)
	}
	return append(argv, spec.Args...), true
}

// Module implements lang.ModuleResolver: the dotted module path when every
// directory between root and the file is a package (__init__.py all the way
// down), so `python -m` run from root resolves it. A package's __main__.py
// maps to the package itself; __init__.py has no runnable module form.
func (toolchain) Module(root, file string) (string, bool) {
	rel, err := filepath.Rel(root, file)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", false
	}
	base := filepath.Base(rel)
	if base == "__init__.py" || !strings.HasSuffix(base, ".py") {
		return "", false
	}
	dir := filepath.Dir(rel)
	if dir == "." {
		return "", false // top-level scripts run as files, not modules
	}
	// Every directory on the way down must be a package.
	parts := strings.Split(dir, string(os.PathSeparator))
	probe := root
	for _, p := range parts {
		probe = filepath.Join(probe, p)
		if st, err := os.Stat(filepath.Join(probe, "__init__.py")); err != nil || st.IsDir() {
			return "", false
		}
	}
	if base == "__main__.py" {
		return strings.Join(parts, "."), true
	}
	return strings.Join(append(parts, strings.TrimSuffix(base, ".py")), "."), true
}
