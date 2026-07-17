package terminal

import (
	"os"
	"path/filepath"
	"strings"
)

// env.go is the toolchain environment activation (Roadmap 0170, #98, #652):
// the EFFECTIVE interpreter per language — explicit settings-page choice
// beating project detection, the same lang.Interpreter seam LSP, debug and
// the statusline read — is activated in fresh IDE terminals the way
// JetBrains does it, so `which python3` shows the real interpreter.
//
// Per mapping, PlanActivation picks one of four modes:
//
//   - venv: the interpreter's bin parent carries pyvenv.cfg → activate like
//     `source bin/activate`: prepend <venv>/bin to PATH and set VIRTUAL_ENV.
//     No shim; `which python3`/`python`/`pip` all show the venv.
//   - prepend: the interpreter lives in a private toolchain directory (pyenv
//     versions, mise/asdf installs, /usr/local/go/bin — anything outside the
//     shared system list) → prepend that directory, real paths win `which`.
//     Exception: a DETECTED interpreter whose directory already wins the
//     base PATH lookup for its own name is skipped — the environment is what
//     PATH gives anyway, no need to touch it.
//   - shim: an EXPLICIT choice inside a shared system directory (/bin,
//     /usr/bin, /usr/local/bin, /opt/homebrew/bin, sbin variants).
//     Prepending would reorder the whole PATH and shadow unrelated tools, so
//     a per-project shim directory holds `#!/bin/sh` exec wrappers for just
//     that language's command names instead.
//   - none: a DETECTED interpreter in a shared system directory (ambient —
//     it is what PATH resolves to already, or close enough that reordering
//     shared dirs would do more harm than good), or an empty mapping.
//
// The overlay applies to the spawn environment of NEW terminals; running
// sessions keep their environment (a venv prepend cannot retarget a live
// shell — JetBrains behaves the same). Shims that lose their reason are
// swept by WriteShims.
//
// Windows note: the shims are POSIX sh scripts; a windows port writes
// `<name>.cmd` wrappers into the same directory instead (`@"%target%" %*`).
// darwin/linux land first, matching the rest of the PTY stack.

// Mapping is one effective interpreter choice for a language.
type Mapping struct {
	Lang        string // language id ("python", "php")
	Interpreter string // absolute interpreter path
	Source      string // "config" (settings page) or "detected"
}

// sharedSystemDirs are the directories shared by unrelated tools: prepending
// one of them would reorder the whole PATH (e.g. an explicit /usr/bin/php
// pushing /usr/bin above homebrew), so interpreters living there activate
// via shim (explicit) or not at all (detected).
var sharedSystemDirs = map[string]bool{
	"/bin":               true,
	"/sbin":              true,
	"/usr/bin":           true,
	"/usr/sbin":          true,
	"/usr/local/bin":     true,
	"/usr/local/sbin":    true,
	"/opt/homebrew/bin":  true,
	"/opt/homebrew/sbin": true,
}

// Plan is the activation decision for a mapping set: which mappings keep an
// exec shim, which directories go ahead of PATH, which conventional
// variables are set, and which mappings inject at all (the title indicator).
type Plan struct {
	Shims   []Mapping // shim mode: explicit choices in shared system dirs
	Prepend []string  // deduped directories to put ahead of the base PATH
	Vars    []string  // conventional variables (VIRTUAL_ENV=…)
	Active  []Mapping // every mapping that injects, in input order
}

// PlanActivation classifies each mapping per the rules in the file header.
// Mappings are expected pre-sorted (by language) for a stable overlay.
func PlanActivation(mappings []Mapping, basePATH string) Plan {
	var p Plan
	seen := map[string]bool{}
	prepend := func(dir string) {
		if !seen[dir] {
			seen[dir] = true
			p.Prepend = append(p.Prepend, dir)
		}
	}
	for _, m := range mappings {
		if m.Interpreter == "" {
			continue
		}
		if venv, ok := venvRoot(m.Interpreter); ok {
			p.Vars = append(p.Vars, "VIRTUAL_ENV="+venv)
			prepend(filepath.Join(venv, "bin"))
			p.Active = append(p.Active, m)
			continue
		}
		dir := filepath.Dir(m.Interpreter)
		if sharedSystemDirs[dir] {
			if m.Source == "config" {
				p.Shims = append(p.Shims, m)
				p.Active = append(p.Active, m)
			}
			continue // detected + shared: ambient, leave PATH alone
		}
		if m.Source != "config" && pathWinner(filepath.Base(m.Interpreter), basePATH) == dir {
			continue // detected and already first on PATH: nothing to fix
		}
		prepend(dir)
		p.Active = append(p.Active, m)
	}
	return p
}

// Overlay composes the spawn-environment overrides for the plan: the prepend
// directories — plus shimDir when shims are active — go ahead of the base
// PATH, and the conventional variables ride along. An empty plan returns
// nil: the environment stays untouched.
func (p Plan) Overlay(shimDir, basePATH string) []string {
	dirs := p.Prepend
	if len(p.Shims) > 0 {
		dirs = append(append([]string{}, dirs...), shimDir)
	}
	if len(dirs) == 0 {
		return nil
	}
	path := strings.Join(dirs, string(os.PathListSeparator))
	if basePATH != "" {
		path += string(os.PathListSeparator) + basePATH
	}
	return append(append([]string{}, p.Vars...), "PATH="+path)
}

// pathWinner returns the first basePATH directory holding an executable file
// named name — the directory a `which name` would report — or "" when none
// does.
func pathWinner(name, basePATH string) string {
	for _, dir := range filepath.SplitList(basePATH) {
		if dir == "" {
			continue
		}
		if st, err := os.Stat(filepath.Join(dir, name)); err == nil && !st.IsDir() {
			return dir
		}
	}
	return ""
}

// shimNames returns the command names a language's shim covers: python
// answers for both `python` and `python3`.
func shimNames(lang string) []string {
	switch lang {
	case "python":
		return []string{"python", "python3"}
	}
	return []string{lang}
}

// knownShimNames is every name WriteShims may have created, for stale sweep.
var knownShimNames = []string{"php", "python", "python3"}

// WriteShims (re)generates dir's shims for the given shim-mode mappings and
// removes stale ones whose language lost its shim (setting removed, or the
// mapping moved to venv/prepend activation). It reports whether any shim is
// active afterwards.
func WriteShims(dir string, mappings []Mapping) (bool, error) {
	if len(mappings) == 0 {
		for _, name := range knownShimNames {
			_ = os.Remove(filepath.Join(dir, name))
		}
		return false, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	wanted := map[string]string{}
	for _, m := range mappings {
		if m.Interpreter == "" {
			continue
		}
		for _, name := range shimNames(m.Lang) {
			wanted[name] = m.Interpreter
		}
	}
	for _, name := range knownShimNames {
		target, ok := wanted[name]
		path := filepath.Join(dir, name)
		if !ok {
			_ = os.Remove(path)
			continue
		}
		script := "#!/bin/sh\nexec \"" + target + "\" \"$@\"\n"
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			return false, err
		}
	}
	return len(wanted) > 0, nil
}

// venvRoot reports the virtualenv root when interp lives in one (its bin
// directory's parent carries pyvenv.cfg).
func venvRoot(interp string) (string, bool) {
	root := filepath.Dir(filepath.Dir(interp))
	if _, err := os.Stat(filepath.Join(root, "pyvenv.cfg")); err == nil {
		return root, true
	}
	return "", false
}

// MergeEnv overlays override entries onto a base environment: keys present
// in the overlay replace the base entry (getenv implementations pick the
// first match, so duplicates are not an option).
func MergeEnv(base, overlay []string) []string {
	if len(overlay) == 0 {
		return base
	}
	replaced := map[string]bool{}
	for _, kv := range overlay {
		if i := strings.IndexByte(kv, '='); i > 0 {
			replaced[kv[:i]] = true
		}
	}
	out := make([]string, 0, len(base)+len(overlay))
	for _, kv := range base {
		if i := strings.IndexByte(kv, '='); i > 0 && replaced[kv[:i]] {
			continue
		}
		out = append(out, kv)
	}
	return append(out, overlay...)
}
