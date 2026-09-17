package langpython

// sitepackages.go resolves the dependency-marker directories Python declares
// through lang.DepWatch.Dirs (#2613). A `pip install -U <pkg>` in the project
// venv writes into `<prefix>/lib/python3.12/site-packages`, which the
// recursive project watch prunes twice over (`.venv` is a dot directory,
// `site-packages` is a vendor-noise name). Watching that one directory —
// never its subtree — is enough: every install, uninstall and upgrade creates
// or removes a `<pkg>-<version>.dist-info` entry directly inside it.
//
// The prefix comes from the same interpreter resolution the toolchain hands
// pyright (toolchain.go), so the watched tree is always the one the server
// actually reads.

import (
	"os"
	"path/filepath"
	"sort"
)

// sitePackages returns the existing site-packages directories of the
// project's *own* interpreter, POSIX and Windows layouts alike. Nil when the
// project has none, or when the layout holds no site-packages — the static
// manifests still carry the project in that case.
//
// Deliberately not `interpreter(root)`: that falls back to `python3` on PATH,
// so every Go or PHP project on the machine would register a watch on the
// system site-packages — hundreds of file descriptors on kqueue for a
// dependency tree that project never touches.
func sitePackages(root string) []string {
	p, ok := projectInterpreter(root)
	if !ok {
		return nil
	}
	// <prefix>/bin/python, <prefix>/Scripts/python.exe → <prefix>.
	prefix := filepath.Dir(filepath.Dir(p))
	seen := map[string]bool{}
	var out []string
	for _, pat := range []string{
		filepath.Join(prefix, "lib", "python*", "site-packages"),
		filepath.Join(prefix, "Lib", "site-packages"),
	} {
		matches, err := filepath.Glob(pat)
		if err != nil {
			continue
		}
		for _, dir := range matches {
			if seen[dir] {
				continue
			}
			if st, err := os.Stat(dir); err == nil && st.IsDir() {
				seen[dir] = true
				out = append(out, dir)
			}
		}
	}
	sort.Strings(out)
	return out
}

// projectInterpreter resolves the interpreter a project brings with it — the
// active virtualenv, a project-local venv, or a .python-version pin — and
// stops there. It is interpreter() minus the PATH fallback, which is a
// machine default rather than a property of this project.
func projectInterpreter(root string) (string, bool) {
	if v := os.Getenv("VIRTUAL_ENV"); v != "" {
		if p, ok := venvPython(v); ok {
			return p, true
		}
	}
	for _, d := range []string{".venv", "venv"} {
		if p, ok := venvPython(filepath.Join(root, d)); ok {
			return p, true
		}
	}
	return pyenvPython(root)
}
