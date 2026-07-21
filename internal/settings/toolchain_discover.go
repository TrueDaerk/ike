package settings

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// toolchain_discover.go finds interpreter candidates for the toolchain page
// (#94). Discovery is best-effort and injectable: command output, PATH
// lookups and directory globs go through small function seams so tests feed
// fixtures.

// runCommand runs a binary and returns its combined stdout ("" on any error).
type runCommand func(name string, args ...string) string

// lookPath resolves a binary on PATH ("" when absent).
type lookPath func(name string) string

// resolveShim resolves a version-manager shim (pyenv/mise/asdf) to the real
// executable, returning the input unchanged when it is not a shim or the
// resolution fails (#650). Production is lang.ResolveShim.
type resolveShim func(root, path string) string

// globList expands a glob pattern to matching paths (#675); production is
// filepath.Glob. Injectable for tests.
type globList func(pattern string) []string

// runCtx is a context-aware command runner returning the combined
// stdout/stderr (#884): the wizard's cancel kills the child through ctx.
type runCtx func(ctx context.Context, name string, args ...string) (string, error)

// execRunCtx is the production runCtx.
func execRunCtx(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}

// execRun is the production runCommand.
func execRun(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// execLook is the production lookPath.
func execLook(name string) string {
	p, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return p
}

// execGlob is the production globList.
func execGlob(pattern string) []string {
	m, _ := filepath.Glob(pattern)
	return m
}

// candidateSet accumulates existing, non-directory interpreter paths in
// insertion order, deduplicated by resolved path (#675) so a symlinked
// duplicate — e.g. Homebrew's opt/php/bin/php next to the PATH php — appears
// once, under the first (highest-priority) spelling.
type candidateSet struct {
	out  []string
	seen map[string]bool
}

func (s *candidateSet) add(p string) {
	if p == "" {
		return
	}
	if st, err := os.Stat(p); err != nil || st.IsDir() {
		return
	}
	s.addTrusted(p)
}

// addTrusted records p without the existence check — for PATH hits, which
// the lookup already proved (#538).
func (s *candidateSet) addTrusted(p string) {
	if p == "" {
		return
	}
	key := p
	if r, err := filepath.EvalSymlinks(p); err == nil {
		key = r
	}
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	if s.seen[key] {
		return
	}
	s.seen[key] = true
	s.out = append(s.out, p)
}

// homebrewPrefixes are the Homebrew install roots whose opt/ trees the
// versioned-directory scan globs (#675): Apple-silicon default first, the
// /usr/local (Intel / manual brew --prefix) fallback second. Variable for
// tests.
var homebrewPrefixes = []string{"/opt/homebrew", "/usr/local"}

// versionedCandidates globs versioned install directories for one language
// (#675): under every Homebrew prefix the unversioned opt/<formula>/bin/<bin>
// first (the current formula), then opt/<formula>@*/bin/<bin> sorted newest
// version first; extra patterns (pyenv versions, go SDKs) are appended, each
// group also newest-first.
func versionedCandidates(formula, bin string, glob globList, extra ...string) []string {
	var patterns []string
	for _, prefix := range homebrewPrefixes {
		patterns = append(patterns,
			filepath.Join(prefix, "opt", formula, "bin", bin),
			filepath.Join(prefix, "opt", formula+"@*", "bin", bin),
		)
	}
	patterns = append(patterns, extra...)
	var out []string
	for _, pat := range patterns {
		m := glob(pat)
		sortNewestFirst(m)
		out = append(out, m...)
	}
	return out
}

// pathVersion extracts the numeric components of the innermost
// version-carrying directory segment of an interpreter path — "php@8.1" →
// [8 1], "go1.22.3" → [1 22 3], "3.12.4" → [3 12 4] — skipping the binary
// name itself and "bin". Nil when the path carries no version.
func pathVersion(p string) []int {
	segs := strings.Split(p, string(filepath.Separator))
	for i := len(segs) - 2; i >= 0; i-- { // -2: the last segment is the binary
		seg := segs[i]
		if seg == "bin" || !strings.ContainsAny(seg, "0123456789") {
			continue
		}
		var nums []int
		run := -1
		for j := 0; j <= len(seg); j++ {
			if j < len(seg) && seg[j] >= '0' && seg[j] <= '9' {
				if run < 0 {
					run = j
				}
				continue
			}
			if run >= 0 {
				if n, err := strconv.Atoi(seg[run:j]); err == nil {
					nums = append(nums, n)
				}
				run = -1
			}
		}
		return nums
	}
	return nil
}

// sortNewestFirst orders one glob group by extracted version, descending;
// version-less paths (the unversioned formula) sort first.
func sortNewestFirst(paths []string) {
	sort.SliceStable(paths, func(i, j int) bool {
		return versionAfter(pathVersion(paths[i]), pathVersion(paths[j]))
	})
}

// versionAfter reports whether version a orders before b in newest-first
// order: nil (unversioned) outranks everything, otherwise element-wise
// numeric descending.
func versionAfter(a, b []int) bool {
	if a == nil || b == nil {
		return a == nil && b != nil
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return len(a) > len(b)
}

// pythonCandidates lists Python interpreter candidates in pick order: the
// active virtualenv, project-local venvs, uv-managed interpreters, the pyenv
// interpreter, PATH, then the versioned install directories (#675) — pyenv
// versions and Homebrew python@* formulas, newest first. Version-manager
// shims are resolved to the real executable before listing (#650); an
// unresolvable shim stays as-is.
func pythonCandidates(root string, run runCommand, look lookPath, resolve resolveShim, glob globList) []string {
	var s candidateSet
	if v := os.Getenv("VIRTUAL_ENV"); v != "" {
		s.add(filepath.Join(v, "bin", "python"))
	}
	for _, d := range []string{".venv", "venv"} {
		s.add(filepath.Join(root, d, "bin", "python"))
	}
	for _, p := range parseUvPythonList(run("uv", "python", "list")) {
		s.add(p)
	}
	home, homeErr := os.UserHomeDir()
	if homeErr == nil {
		s.add(resolve(root, filepath.Join(home, ".pyenv", "shims", "python")))
	}
	if p := look("python3"); p != "" {
		s.add(resolve(root, p))
	}
	if p := look("python"); p != "" {
		s.add(resolve(root, p))
	}
	var extra []string
	if homeErr == nil {
		extra = append(extra, filepath.Join(home, ".pyenv", "versions", "*", "bin", "python"))
	}
	for _, p := range versionedCandidates("python", "python3", glob, extra...) {
		s.add(p)
	}
	return s.out
}

// parseUvPythonList extracts installed interpreter paths from `uv python
// list` output: one entry per line, "<key>  <path>"; uninstalled versions show
// "<download available>" instead of a path and are skipped.
func parseUvPythonList(out string) []string {
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		p := fields[len(fields)-1]
		if strings.HasPrefix(p, string(filepath.Separator)) {
			paths = append(paths, p)
		}
	}
	return paths
}

// phpCandidates lists PHP interpreter candidates: PATH first (shims resolved,
// #650), common install locations, then the versioned Homebrew php@*
// formulas, newest first (#675).
// phpWellKnown are the fixed PHP install locations probed after PATH.
// Variable for tests.
var phpWellKnown = []string{"/opt/homebrew/bin/php", "/usr/local/bin/php", "/usr/bin/php"}

func phpCandidates(root string, look lookPath, resolve resolveShim, glob globList) []string {
	var s candidateSet
	if p := look("php"); p != "" {
		s.add(resolve(root, p))
	}
	for _, p := range phpWellKnown {
		s.add(p)
	}
	for _, p := range versionedCandidates("php", "php", glob) {
		s.add(p)
	}
	return s.out
}

// wellKnownBinDirs are the install directories the generic candidate lookup
// probes after PATH (#538) — homebrew and the go tarball land outside the PATH
// a GUI-launched process inherits. Variable for tests.
var wellKnownBinDirs = []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/local/go/bin", "/usr/bin"}

// defaultCandidates lists interpreter candidates for languages without
// specific discovery (#538): PATH lookup by language id (shims resolved,
// #650), the id in the well-known install directories, then the versioned
// install directories (#675) — Homebrew <id>@* formulas and, for go, the
// ~/sdk/go* toolchains — newest first.
func defaultCandidates(id, root string, look lookPath, resolve resolveShim, glob globList) []string {
	var s candidateSet
	if p := look(id); p != "" {
		s.addTrusted(resolve(root, p))
	}
	for _, dir := range wellKnownBinDirs {
		s.add(filepath.Join(dir, id))
	}
	var extra []string
	if id == "go" {
		if home, err := os.UserHomeDir(); err == nil {
			extra = append(extra, filepath.Join(home, "sdk", "go*", "bin", "go"))
		}
	}
	for _, p := range versionedCandidates(id, id, glob, extra...) {
		s.add(p)
	}
	return s.out
}

// versionArgs returns the probe invocation for a language's interpreter.
func versionArgs(langID string) []string {
	if langID == "php" {
		return []string{"-v"}
	}
	return []string{"--version"}
}
