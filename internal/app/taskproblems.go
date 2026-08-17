package app

import (
	"path/filepath"
	"sync"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/editor/buffer"
	ilsp "ike/internal/lsp"
	"ike/internal/matcher"
	"ike/internal/run"
	"ike/internal/terminal"
)

// taskproblems.go tees a run's terminal output through its configuration's
// problem matchers (#1915): the session's raw chunks feed a matcher.Engine on
// the terminal's feed goroutine (off the render loop), matched lines become
// diagnostics under a per-run source in the Problems store, and every launch
// clears its predecessor's findings first — a re-run replaces its problems
// instead of duplicating them. Unmatched output is untouched; the terminal
// stream itself is never altered.

// TaskProblemsMsg carries one run's accumulated matcher findings: the full
// set so far, keyed by absolute path — the store replaces the source
// wholesale, so message loss or reordering cannot corrupt the view.
type TaskProblemsMsg struct {
	Source string
	ByPath map[string][]ilsp.Diagnostic
}

// resolveMatchers turns a configuration's matcher names into matchers:
// built-ins first, then the project's [[tasks.matcher]] entries (a custom
// matcher may shadow a built-in name). Unknown names are skipped — validation
// already reported broken custom entries, and a name referencing nothing
// should not block the run.
func resolveMatchers(names []string) []matcher.Matcher {
	var custom []config.MatcherEntry
	if c := config.Get(); c != nil {
		custom = c.Tasks.Matchers
	}
	var out []matcher.Matcher
	for _, name := range names {
		resolved := false
		for _, e := range custom {
			if e.Name != name {
				continue
			}
			// Validation vouched for the entry; a compile failure here means
			// a stale in-memory config, so skipping is the safe answer.
			if r, err := matcher.Compile(e.Name, e.Pattern, e.File, e.Line, e.Col, e.Severity, e.Message, e.DefaultSeverity); err == nil {
				out = append(out, r)
				resolved = true
			}
			break
		}
		if resolved {
			continue
		}
		if b, ok := matcher.Builtin(name); ok {
			out = append(out, b)
		}
	}
	return out
}

// taskCollector accumulates one run's matcher findings. feed runs on the
// terminal session's feed goroutine, so state is mutex-guarded and results
// travel to the Update loop as TaskProblemsMsg snapshots.
type taskCollector struct {
	mu     sync.Mutex
	eng    *matcher.Engine
	dir    string
	source string
	send   func(tea.Msg)
	byPath map[string][]ilsp.Diagnostic
}

// feed is the session tap: parse the chunk, convert any new problems and
// publish a snapshot. Chunks without a completed match cost one engine pass
// and no message.
func (c *taskCollector) feed(chunk []byte) {
	c.mu.Lock()
	problems := c.eng.Feed(chunk)
	if len(problems) == 0 {
		c.mu.Unlock()
		return
	}
	for _, p := range problems {
		path := p.File
		if !filepath.IsAbs(path) {
			path = filepath.Join(c.dir, path)
		}
		path = filepath.Clean(path)
		line, col := p.Line-1, 0
		if p.Col > 0 {
			col = p.Col - 1
		}
		pos := buffer.Position{Line: line, Col: col}
		c.byPath[path] = append(c.byPath[path], ilsp.Diagnostic{
			Range:    buffer.Range{Start: pos, End: pos},
			Severity: p.Severity,
			Message:  p.Message,
			Source:   c.source,
		})
	}
	snapshot := make(map[string][]ilsp.Diagnostic, len(c.byPath))
	for p, ds := range c.byPath {
		snapshot[p] = append([]ilsp.Diagnostic(nil), ds...)
	}
	c.mu.Unlock()
	c.send(TaskProblemsMsg{Source: c.source, ByPath: snapshot})
}

// taskOutputTap builds the output tee for cfg's launch, clearing the run's
// previous findings as a side effect; nil when the configuration names no
// resolvable matcher (the common case — plain file runs parse nothing).
func (m *Model) taskOutputTap(root string, cfg *run.Config) func([]byte) {
	if len(cfg.Matchers) == 0 {
		return nil
	}
	// A fresh run starts with a clean slate either way — even if no matcher
	// resolves, stale findings from an earlier configuration must not linger.
	m.probStore.ClearTaskSource(cfg.Name)
	m.refreshProblemsPanel()
	ms := resolveMatchers(cfg.Matchers)
	if len(ms) == 0 {
		return nil
	}
	c := &taskCollector{
		eng:    matcher.NewEngine(ms),
		dir:    cfg.Dir(root),
		source: cfg.Name,
		send:   m.host.Send,
		byPath: map[string][]ilsp.Diagnostic{},
	}
	return c.feed
}

// runToolTerminal finds the Run tool's terminal wherever it lives — dedicated
// pane or hosted tab (the startInRunTool lookup, shared with the tap install).
func (m *Model) runToolTerminal() *terminal.Model {
	locs := m.toolLocations(runToolName)
	if len(locs) == 0 {
		return nil
	}
	loc := locs[0]
	inst := m.activeWS().Panes.Get(loc.key)
	if inst == nil {
		return nil
	}
	if loc.tab >= 0 {
		return inst.TabTerminal(loc.tab)
	}
	return inst.Terminal()
}
