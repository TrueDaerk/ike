package app

// depwatch.go registers the per-language dependency-update markers (#2613) as
// non-recursive per-path watches, and tags the watcher events they produce so
// the LSP bridge can act on them.
//
// A `pip install -U`, `go get`, `npm install` or `composer update` moves files
// the recursive project watch deliberately prunes — `.venv/…/site-packages`,
// `node_modules`, `vendor` — so the running language server keeps serving its
// stale dependency index until a manual `lsp.restart`. Rather than watching
// those trees (thousands of files, which is why they are pruned), each
// language declares a handful of cheap marker paths through the registry
// (`lang.DepWatch`, see internal/lang/depwatch.go) and only those are watched.
//
// Registration is reconciled, like the file-diff watches next door: the marker
// set is resolved once per project root and re-resolved whenever a marker
// itself changed — a rewritten pyproject.toml or a freshly created venv can
// move where site-packages lives.

import (
	langreg "ike/internal/lang"
)

// syncDepWatches reconciles the registered dependency-marker watches with the
// markers the current project root declares. It runs once per settled Update
// pass and is a plain root-string compare in the overwhelmingly common case:
// the marker set is only re-resolved when the project root changed or a
// previous marker event marked it stale, because resolving it runs each
// language's toolchain probe (interpreter lookup, venv glob).
func (m *Model) syncDepWatches() {
	if m.watcher == nil {
		return
	}
	root := m.watcher.Root()
	if root == "" {
		return
	}
	if root == m.depWatchRoot && !m.depWatchDirty {
		return
	}
	m.depWatchRoot, m.depWatchDirty = root, false
	want := langreg.DepWatchPaths(root)
	for p := range m.depWatched {
		if _, keep := want[p]; !keep {
			m.watcher.UnwatchPath(p)
		}
	}
	for p := range want {
		if _, had := m.depWatched[p]; !had {
			m.watcher.WatchPath(p)
		}
	}
	m.depWatched = want
}

// depWatchLang reports the language whose dependency markers cover path and
// the project root they were resolved for, both empty for an ordinary file.
// A hit also marks the marker set stale so the next settled pass re-resolves
// it: an install that rewrote pyproject.toml may have created the venv whose
// site-packages nothing was watching yet.
func (m *Model) depWatchLang(path string) (lang, root string) {
	if len(m.depWatched) == 0 {
		return "", ""
	}
	l, ok := langreg.DepWatchMatch(m.depWatchRoot, path, m.depWatched)
	if !ok {
		return "", ""
	}
	m.depWatchDirty = true
	return l, m.depWatchRoot
}
