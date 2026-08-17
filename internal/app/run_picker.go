package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/fuzzy"
	"ike/internal/host"
	"ike/internal/palette"
	"ike/internal/run"
	"ike/internal/vscodelaunch"
)

// run_picker.go is the run-configuration picker (#1914, run.select): every
// stored configuration (.ike/runconfigs.json) plus the compatible
// .vscode/launch.json entries, merged at open time — the store stays
// authoritative, a launch.json entry whose name collides is hidden, and
// nothing from launch.json is ever written back. Picking a run-kind row
// launches it like run.file; a debug-kind row (all launch.json imports)
// starts a debug session.

// runConfigsPrefix selects the picker mode inside the palette; opened locked
// only, so the rune has no user-facing prefix story.
const runConfigsPrefix = '('

// RunConfigPickedMsg launches one picked configuration.
type RunConfigPickedMsg struct {
	Cfg    run.Config
	Stored bool // lives in .ike/runconfigs.json (vs a launch.json import)
}

// runConfigEntry is one row of the picker.
type runConfigEntry struct {
	cfg    run.Config
	stored bool
}

// runConfigsMode is the palette Mode listing the merged configurations; the
// model fills entries before each locked open (the httpRequestsMode pattern).
type runConfigsMode struct {
	entries []runConfigEntry
}

func newRunConfigsMode() *runConfigsMode { return &runConfigsMode{} }

// Prefix implements palette.Mode.
func (r *runConfigsMode) Prefix() rune { return runConfigsPrefix }

// Placeholder implements palette.Mode.
func (r *runConfigsMode) Placeholder() string { return "Run configuration…" }

// Results implements palette.Mode: rows fuzzy-matched over the configuration
// name, detailing kind, language and target; launch.json imports name their
// source so the two origins stay distinguishable.
func (r *runConfigsMode) Results(query string, _ palette.Context) []palette.Item {
	var items []palette.Item
	for _, e := range r.entries {
		res, ok := fuzzy.Match(query, e.cfg.Name)
		if !ok {
			continue
		}
		detail := string(e.cfg.Kind)
		if e.cfg.Lang != "" {
			detail += " · " + e.cfg.Lang
		}
		if e.cfg.File != "" {
			detail += " · " + e.cfg.File
		}
		if !e.stored {
			detail += " · .vscode/launch.json"
		}
		items = append(items, palette.Item{
			Title:  e.cfg.Name,
			Spans:  res.Positions,
			Score:  res.Score,
			Detail: detail,
			Msg:    RunConfigPickedMsg{Cfg: e.cfg, Stored: e.stored},
		})
	}
	return items
}

// vscodeLaunchEnabled reads run.vscode_launch (default on): whether the
// picker merges .vscode/launch.json configurations in.
func (m Model) vscodeLaunchEnabled() bool {
	v, ok := m.host.Config().Get("run.vscode_launch")
	return !ok || v != "false"
}

// openRunConfigPicker opens the palette locked to the configurations mode
// (run.select). Nothing to list explains itself instead of showing an empty
// palette.
func (m *Model) openRunConfigPicker() {
	root := projectRoot()
	store := run.Load()
	var entries []runConfigEntry
	names := map[string]bool{}
	for _, cfg := range store.Configs {
		entries = append(entries, runConfigEntry{cfg: cfg, stored: true})
		names[cfg.Name] = true
	}
	if m.vscodeLaunchEnabled() {
		for _, cfg := range vscodelaunch.Configs(root) {
			if names[cfg.Name] {
				continue // the store wins a name collision
			}
			entries = append(entries, runConfigEntry{cfg: cfg})
		}
	}
	if len(entries) == 0 {
		m.host.Notify(host.Info, "run: no configurations yet — run a file first, or add .vscode/launch.json entries")
		return
	}
	m.runConfigs.entries = entries
	m.palette.SetSize(m.width, m.height)
	m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, runConfigsPrefix)
}

// runPickedConfig launches one picker row: debug-kind configurations start a
// debug session through the shared guards, run-kind ones ride the ordinary
// launch funnel (which also registers rerun's last-used memory for stored
// configurations).
func (m *Model) runPickedConfig(msg RunConfigPickedMsg) tea.Cmd {
	root := projectRoot()
	if msg.Cfg.Kind == run.KindDebug {
		if msg.Stored {
			store := run.Load()
			store.Touch(msg.Cfg.Name)
			_ = run.Save(store)
		}
		m.startDebugConfig(root, msg.Cfg)
		return nil
	}
	store := run.Load()
	cfg := &msg.Cfg
	if stored := store.ByName(msg.Cfg.Name); msg.Stored && stored != nil {
		cfg = stored // launch the store's current data, not the open-time copy
	}
	return m.launchRun(root, store, cfg, false)
}
