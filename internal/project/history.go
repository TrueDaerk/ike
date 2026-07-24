package project

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/ui"
)

// history.go owns the recent-projects history content: loading it from the
// typed config, the upsert/dedupe/cap rules, and persisting the new list
// through config's typed setter (list semantics: replace, not append).
//
// History persists to the *user* layer, not the project layer DefaultScope
// would pick for a `project.*` key: the list spans projects on this machine,
// and the picker must see it whichever project is currently open.

// History returns cfg's recent-projects list as typed entries, in stored
// order (most-recent-first).
func History(cfg *config.Config) []Entry {
	out := make([]Entry, len(cfg.Project.History))
	for i, e := range cfg.Project.History {
		out[i] = fromConfig(e)
	}
	return out
}

// upsert returns history with e recorded as the most recent open: moved (or
// added) to the front, deduped by path, capped at max entries (max < 0 means
// unbounded; the config default is validated to >= 0).
func upsert(history []Entry, e Entry, max int) []Entry {
	out := []Entry{e}
	for _, h := range history {
		if h.Path != e.Path {
			out = append(out, h)
		}
	}
	if max >= 0 && len(out) > max {
		out = out[:max]
	}
	return out
}

// RecordOpen records a successful open of root at openedAt: it validates the
// path, upserts it into the persisted history and writes the new list back
// through config's typed setter. It is called for the initial project open at
// startup and (Roadmap 0090, #3) after a completed switch — never on a failed
// or cancelled attempt: an invalid root returns the validation error and
// leaves the stored history untouched.
func RecordOpen(opts config.Options, root string, openedAt time.Time) error {
	abs, err := Validate(root)
	if err != nil {
		return err
	}
	cfg, _ := config.Load(opts)
	entries := upsert(History(cfg), NewEntry(abs, openedAt), cfg.Project.MaxHistory)

	raw := make([]map[string]any, len(entries))
	for i, e := range entries {
		c := e.toConfig()
		raw[i] = map[string]any{"path": c.Path, "name": c.Name, "last_opened": c.LastOpened}
	}
	return config.WriteKey(opts, config.UserScope, "project.history", raw)
}

// RemoveFromHistory deletes the entry at path from the persisted history
// (#842) and writes the shortened list back through config's typed setter.
// A path not in the list is a no-op. The comparison is by the stored
// (absolute, cleaned) path, like the upsert dedupe.
func RemoveFromHistory(opts config.Options, path string) error {
	cfg, _ := config.Load(opts)
	var out []map[string]any
	for _, e := range History(cfg) {
		if e.Path == path {
			continue
		}
		c := e.toConfig()
		out = append(out, map[string]any{"path": c.Path, "name": c.Name, "last_opened": c.LastOpened})
	}
	return config.WriteKey(opts, config.UserScope, "project.history", out)
}

// RemoveFromHistoryCmd wraps RemoveFromHistory as a tea.Cmd, mirroring
// RecordOpenCmd: the write never blocks the Update loop.
func RemoveFromHistoryCmd(opts config.Options, path string) tea.Cmd {
	return func() tea.Msg {
		return RemovedFromHistoryMsg{Path: path, Err: RemoveFromHistory(opts, path)}
	}
}

// RelTime renders how long ago t was, compact ("just now", "5m ago",
// "3h ago", "4d ago", "6w ago") for the picker's last-opened badge (#842).
// The zero time (legacy entries without a timestamp) yields "". The
// implementation moved to ui.RelTime (#1113) so the palette's recent-files
// mode can share it; this wrapper keeps the established project API.
func RelTime(t, now time.Time) string { return ui.RelTime(t, now) }

// RecordedMsg reports a RecordOpenCmd outcome. Err is nil on success.
type RecordedMsg struct {
	Root string
	Err  error
}

// RecordOpenCmd wraps RecordOpen as a tea.Cmd so the Update loop never blocks
// on the validation stat or the config write (Roadmap 0090 design rule).
func RecordOpenCmd(opts config.Options, root string, openedAt time.Time) tea.Cmd {
	return func() tea.Msg {
		return RecordedMsg{Root: root, Err: RecordOpen(opts, root, openedAt)}
	}
}
