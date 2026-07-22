package settings

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// pythonpkg.go is the package-management half of the toolchain page's package
// view (#571, QoL follow-up to #569): install / uninstall / upgrade the
// selected environment's packages and surface available upgrades, mirroring
// PyCharm's interpreter packages table. Everything runs asynchronously in
// tea.Cmds; failures surface the decisive stderr line in the status area.

// pkgBackend selects how package commands are constructed.
type pkgBackend int

const (
	// pkgBackendPip shells out to the interpreter's own pip module.
	pkgBackendPip pkgBackend = iota
	// pkgBackendUvPip uses `uv pip … --python <interp>` (works in envs
	// without pip installed).
	pkgBackendUvPip
	// pkgBackendUvProject uses `uv add`/`uv remove` so pyproject.toml and
	// uv.lock stay in sync with the environment.
	pkgBackendUvProject
)

// detectPkgBackend picks the backend: a uv project (pyproject.toml + uv.lock
// in root, uv on PATH) whose effective interpreter lives INSIDE the project
// (its own .venv) manages packages through the manifest; any other
// interpreter with uv available uses `uv pip`; plain pip otherwise. The
// inside-the-project guard keeps `uv add` from mutating the manifest while
// the user has, say, a pyenv interpreter selected.
func detectPkgBackend(root, interp string, look lookPath) pkgBackend {
	if look("uv") == "" {
		return pkgBackendPip
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = root
	}
	inProject := strings.HasPrefix(interp, absRoot+string(filepath.Separator))
	if inProject &&
		fileExists(filepath.Join(absRoot, "pyproject.toml")) &&
		fileExists(filepath.Join(absRoot, "uv.lock")) {
		return pkgBackendUvProject
	}
	return pkgBackendUvPip
}

// pkgAction is one of the three package operations.
type pkgAction int

const (
	pkgInstall pkgAction = iota
	pkgUninstall
	pkgUpgrade
)

func (a pkgAction) verb() string {
	switch a {
	case pkgUninstall:
		return "uninstall"
	case pkgUpgrade:
		return "upgrade"
	}
	return "install"
}

// pkgCmd is one external command of a package operation.
type pkgCmd struct {
	bin  string
	args []string
}

// pkgCommands builds the command sequence for an action per backend. name may
// carry a `==version` pin on install.
func pkgCommands(backend pkgBackend, action pkgAction, name, root, interp string) []pkgCmd {
	switch backend {
	case pkgBackendUvProject:
		switch action {
		case pkgInstall:
			return []pkgCmd{{"uv", []string{"add", "--directory", root, name}}}
		case pkgUninstall:
			return []pkgCmd{{"uv", []string{"remove", "--directory", root, name}}}
		case pkgUpgrade:
			// The uv-documented upgrade path: re-lock just this package to
			// its latest compatible version, then sync the environment.
			return []pkgCmd{
				{"uv", []string{"lock", "--directory", root, "--upgrade-package", name}},
				{"uv", []string{"sync", "--directory", root}},
			}
		}
	case pkgBackendUvPip:
		switch action {
		case pkgInstall:
			return []pkgCmd{{"uv", []string{"pip", "install", "--python", interp, name}}}
		case pkgUninstall:
			return []pkgCmd{{"uv", []string{"pip", "uninstall", "--python", interp, name}}}
		case pkgUpgrade:
			return []pkgCmd{{"uv", []string{"pip", "install", "--python", interp, "--upgrade", name}}}
		}
	default:
		switch action {
		case pkgInstall:
			return []pkgCmd{{interp, []string{"-m", "pip", "install", name}}}
		case pkgUninstall:
			return []pkgCmd{{interp, []string{"-m", "pip", "uninstall", "-y", name}}}
		case pkgUpgrade:
			return []pkgCmd{{interp, []string{"-m", "pip", "install", "--upgrade", name}}}
		}
	}
	return nil
}

// PkgActionMsg reports a finished package action. On success Pkgs carries the
// refreshed listing (fetched by the same command, so the view updates in one
// message); on failure Output is the decisive stderr line.
type PkgActionMsg struct {
	Path   string // interpreter the action ran against
	Action string // "install foo" — phrased for the status line
	Name   string // bare package name (for latest-column invalidation)
	Err    error
	Output string
	Pkgs   []pkgInfo
}

// runPkgAction executes the command sequence asynchronously, refreshing the
// package listing on success.
func runPkgAction(interp string, action pkgAction, name string, cmds []pkgCmd, runE runCtx, run runCommand, look lookPath) tea.Cmd {
	phrase := action.verb() + " " + name
	return func() tea.Msg {
		for _, c := range cmds {
			out, err := runE(context.Background(), c.bin, c.args...)
			if err != nil {
				return PkgActionMsg{Path: interp, Action: phrase, Name: name, Err: err, Output: decisiveLine(out)}
			}
		}
		msg := listPackages(interp, run, look)()
		pm, _ := msg.(PackagesMsg)
		return PkgActionMsg{Path: interp, Action: phrase, Name: name, Pkgs: pm.Pkgs}
	}
}

// decisiveLine picks the most useful line of a failed command's combined
// output: the last line mentioning "error", else the last non-empty line.
func decisiveLine(out string) string {
	lines := strings.Split(out, "\n")
	last := ""
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		last = l
		if strings.Contains(strings.ToLower(l), "error") {
			return l
		}
	}
	return last
}

// OutdatedMsg delivers the available-upgrades map for an interpreter:
// normalized package name -> latest version.
type OutdatedMsg struct {
	Path   string
	Latest map[string]string
}

// listOutdated fetches available upgrades asynchronously: `uv pip list
// --outdated` when uv is on PATH, pip's own `--outdated` otherwise — both in
// JSON. Errors degrade to an empty map (the column just stays absent); the
// listing itself already reports the hard failures.
func listOutdated(interp string, run runCommand, look lookPath) tea.Cmd {
	return func() tea.Msg {
		var out string
		if look("uv") != "" {
			out = run("uv", "pip", "list", "--outdated", "--python", interp, "--format", "json")
		}
		if strings.TrimSpace(out) == "" {
			out = run(interp, "-m", "pip", "list", "--outdated", "--format=json")
		}
		return OutdatedMsg{Path: interp, Latest: parseOutdated(out)}
	}
}

// parseOutdated parses pip/uv `list --outdated --format json` output into a
// normalized-name -> latest-version map. Both tools emit an array of objects
// with "name" and "latest_version".
func parseOutdated(out string) map[string]string {
	var rows []struct {
		Name   string `json:"name"`
		Latest string `json:"latest_version"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &rows); err != nil {
		return map[string]string{}
	}
	latest := make(map[string]string, len(rows))
	for _, r := range rows {
		if r.Name != "" && r.Latest != "" {
			latest[normalizePkg(r.Name)] = r.Latest
		}
	}
	return latest
}

// normalizePkg normalizes a package name PEP-503 style: lowercase, runs of
// `-`, `_`, `.` collapse to `-` — so freeze output and the outdated JSON
// agree on identity.
func normalizePkg(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if r == '-' || r == '_' || r == '.' {
			if !dash {
				b.WriteByte('-')
			}
			dash = true
			continue
		}
		dash = false
		b.WriteRune(r)
	}
	return b.String()
}

// pkgBaseName strips a `==version` (or other PEP-440 comparator) suffix from
// an install spec, for the latest-column invalidation after installs.
func pkgBaseName(spec string) string {
	if i := strings.IndexAny(spec, "=<>!~ "); i >= 0 {
		return spec[:i]
	}
	return spec
}
