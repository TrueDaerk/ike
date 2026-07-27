package project

import (
	"os"
	"path/filepath"

	"ike/internal/config"
)

// RestoreLastRoot resolves the project root startup should anchor at (#1000):
// with `project.restore_last` enabled and no explicit open target, the most
// recent history entry wins over the current directory. It returns the
// validated root to re-anchor at ("" means stay where startup is) and a
// user-facing notice when the stored project has become unavailable (deleted
// or otherwise invalid) — the caller then stays on cwd and shows the notice.
func RestoreLastRoot(opts config.Options, cwd string) (root, notice string) {
	cfg, _ := config.Load(opts)
	if cfg == nil || !cfg.Project.RestoreLast {
		return "", ""
	}
	// Starting inside a project directory is an explicit target (#1010):
	// `ike` run in a checkout must never be hijacked to the history head —
	// combined with RecordOpen re-recording the restored root, one stray
	// entry would otherwise make the redirect self-sustaining.
	if isProjectDir(cwd) {
		return "", ""
	}
	hist := History(cfg)
	if len(hist) == 0 {
		return "", ""
	}
	last := hist[0].Path
	if last == cwd {
		return "", ""
	}
	abs, err := Validate(last)
	if err != nil {
		return "", "last project unavailable: " + last + " — opening the current directory"
	}
	// The home directory is never a real project (#1010): an accidental
	// `ike` in ~ records it, and restoring into it points the recursive
	// watcher at the whole home tree.
	if isHome(abs) {
		return "", ""
	}
	return abs, ""
}

// isProjectDir reports whether dir carries a project marker — a .git or .ike
// entry — meaning a start there targets that project deliberately.
//
// The home directory never qualifies (#1245): ~/.ike is the user config
// directory, so the marker would match for everyone who ever wrote a setting
// and would suppress every restore started from ~. Dotfile repos make the same
// true of ~/.git. Home is not a project either way — see the target-side guard
// in RestoreLastRoot.
func isProjectDir(dir string) bool {
	if dir == "" || isHome(dir) {
		return false
	}
	for _, marker := range []string{".git", ".ike"} {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return true
		}
	}
	return false
}

// isHome reports whether dir is the user's home directory. dir is cleaned
// before comparing so a trailing separator or "." segment still matches; an
// undiscoverable home makes this false.
func isHome(dir string) bool {
	if dir == "" {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	return filepath.Clean(dir) == filepath.Clean(home)
}
