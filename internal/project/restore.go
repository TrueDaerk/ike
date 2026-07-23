package project

import "ike/internal/config"

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
	return abs, ""
}
