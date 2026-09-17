package manager

// depwatch.go turns a dependency-marker change into something the language
// servers actually act on (#2613).
//
// After `pip install -U <pkg>`, `go get`, `npm install` or `composer update`
// the running server keeps serving the dependency index it built at startup:
// imports of new symbols stay flagged unresolved and completion does not offer
// them until `lsp.restart`. The app watches a handful of cheap marker paths
// per language (internal/app/depwatch.go, declared via lang.DepWatch) and
// tags their events; this is where the two reactions live:
//
//  1. the marker paths go out as workspace/didChangeWatchedFiles like any
//     other external change — enough for gopls, tsserver and Intelephense,
//     which re-resolve their module / node_modules / vendor index from it;
//  2. a language whose servers demonstrably do *not* re-scan on that
//     notification (Python: pyright resolves site-packages once) is restarted
//     for the affected root, debounced, with a toast explaining the brief
//     diagnostics blip.
//
// The debounce is the load-bearing part: a single `pip install` touches
// dozens of `dist-info` entries over a second or more, and one restart per
// entry would be worse than no restart at all.

import (
	"time"

	langreg "ike/internal/lang"
	"ike/internal/lsp"
)

// depRestartDebounce is how long marker events accumulate before the affected
// servers restart. Package managers touch files for a while — pip unpacks a
// wheel entry by entry, npm rewrites node_modules over seconds — so the
// window sits well above the watched-files one (200 ms) that only has to fold
// a git checkout.
const depRestartDebounce = 1500 * time.Millisecond

// depMarker records which language declared a pending marker path, and the
// project root its markers were resolved for. The root is what scopes the
// restart: a monorepo's other projects keep their servers.
type depMarker struct{ lang, root string }

// DepEvent records one dependency-marker change: lang declared the marker,
// root is the project root it was resolved for, path is the marker (or an
// entry of a marker directory) and typ a protocol.FileChange* value.
//
// It is FileEvent plus two things: the path is tagged as a marker, so the
// batch reaches the language's servers even when the ordinary
// language-mapping fallback would drop it (go.sum and a `.dist-info` entry
// map to no language at all), and — for a restarting language — the restart
// timer is (re)armed.
func (m *Manager) DepEvent(lang, root, path string, typ int) {
	if lang == "" {
		m.FileEvent(path, typ)
		return
	}
	abs, absRoot := normalizeAbs(path), normalizeAbs(root)
	// The markers belong to the declaring language, but the servers are keyed
	// by the language whose server handles it (#1063): a declaration on a
	// delegating language ("go.mod" → the go server) has to reach that one.
	srvLang := serverLangFor(lang)
	m.mu.Lock()
	if m.watchedMarkers == nil {
		m.watchedMarkers = make(map[string]depMarker)
	}
	m.watchedMarkers[abs] = depMarker{lang: srvLang, root: absRoot}
	m.mu.Unlock()
	m.FileEvent(abs, typ)
	if langreg.DepWatchRestart(lang) {
		m.armDepRestart(srvLang, absRoot)
	}
}

// armDepRestart adds one (language, root) pair to the pending restart set and
// (re)starts the debounce timer, so a burst of marker events costs exactly one
// restart per pair.
func (m *Manager) armDepRestart(lang, root string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.depPending == nil {
		m.depPending = make(map[depMarker]bool)
	}
	m.depPending[depMarker{lang: lang, root: root}] = true
	if m.depTimer == nil {
		m.depTimer = time.AfterFunc(m.depDelay, m.flushDepRestarts)
	} else {
		m.depTimer.Reset(m.depDelay)
	}
}

// flushDepRestarts restarts the servers of every pending (language, root)
// pair. Runs on the debounce-timer goroutine, so the blocking restart — it
// runs an initialize handshake per server — never touches the Update
// goroutine. A pair with no live server is silently dropped: there is nothing
// to restart and nothing to explain.
func (m *Manager) flushDepRestarts() {
	m.mu.Lock()
	pending := m.depPending
	m.depPending, m.depTimer = nil, nil
	m.mu.Unlock()
	for p := range pending {
		name := m.serverName(p.lang, p.root)
		if name == "" {
			continue
		}
		m.RestartRoot(p.lang, p.root)
		m.status(p.lang, name+" restarted: dependencies changed", lsp.ServerEventInfo)
	}
}

// serverName returns the command name of a live server for lang under root —
// "pyright-langserver", so the toast names what the user sees in the Language
// Servers pane rather than the language id. "" means no such server runs.
func (m *Manager) serverName(lang, root string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, srv := range m.servers {
		if srv.lang != lang || !underRoot(srv.root, root) {
			continue
		}
		if srv.spec.Command != "" {
			return srv.spec.Command
		}
		return lang
	}
	return ""
}

// markerWants reports whether a pending marker event is relevant for srv: the
// marker's language is the server's, and the two agree on a root — either the
// server sits inside the project root the markers were resolved for (the
// monorepo sub-root case) or the marker itself lies inside the server's root.
// Markers deliberately bypass both the registered-glob filter and the
// language-mapping fallback: go.sum maps to no registered language, and a
// `<pkg>-1.2.dist-info` directory is not a file any glob would name.
func markerWants(dm depMarker, srv *server, path string) bool {
	if dm.lang != srv.lang {
		return false
	}
	return underRoot(srv.root, dm.root) || underRoot(path, srv.root)
}
