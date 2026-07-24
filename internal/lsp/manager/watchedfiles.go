package manager

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"time"

	langreg "ike/internal/lang"
	"ike/internal/lsp/protocol"
)

// watchedfiles.go propagates external file creations/changes/deletions to the
// running servers as workspace/didChangeWatchedFiles (#1144). Workspace-indexing
// servers (Intelephense) only re-index files they were told about: without
// these events a class created outside IKE keeps producing "Undefined type"
// until a manual save pokes the server.
//
// Events enter through FileEvent (the app forwards the 0140 watcher's per-file
// events, and the LSP bridge feeds IKE's own saves as Changed), accumulate for
// watchedDebounce, and flush as one batched notification per interested
// server. Interest is decided per server: against its dynamically registered
// watcher globs (client/registerCapability) when it has any, otherwise by a
// language-match fallback (the file's language resolves to the server's
// language and the file lies under the server's root).

// watchedDebounce is how long external file events accumulate before one
// batched workspace/didChangeWatchedFiles goes out per server. The 0140
// watcher already debounces raw fsnotify bursts (100ms); this second window
// folds its per-path messages — a git checkout touching hundreds of files —
// into a single notification.
const watchedDebounce = 200 * time.Millisecond

// registerWatchers stores the workspace/didChangeWatchedFiles globs of a
// client/registerCapability request on the server; other methods are ignored
// (the caller acknowledges them regardless).
func (m *Manager) registerWatchers(srv *server, regs []protocol.Registration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range regs {
		if r.Method != "workspace/didChangeWatchedFiles" {
			continue
		}
		var opts protocol.DidChangeWatchedFilesRegistrationOptions
		if err := json.Unmarshal(r.RegisterOptions, &opts); err != nil {
			continue
		}
		if srv.watchers == nil {
			srv.watchers = make(map[string][]protocol.FileSystemWatcher)
		}
		srv.watchers[r.ID] = opts.Watchers
	}
}

// unregisterWatchers drops previously registered watcher sets by id.
func (m *Manager) unregisterWatchers(srv *server, unregs []protocol.Unregistration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range unregs {
		if u.Method == "workspace/didChangeWatchedFiles" {
			delete(srv.watchers, u.ID)
		}
	}
}

// FileEvent records one external file event (typ is a protocol.FileChange*
// value) and (re)arms the debounce flush. Safe to call with no servers running
// — the flush simply finds nobody interested.
func (m *Manager) FileEvent(path string, typ int) {
	path = normalizeAbs(path)
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.watchedPending[path]; ok {
		merged, keep := mergeChangeTypes(old, typ)
		if !keep {
			// Created then deleted inside one window: the server never saw
			// the file — nothing to announce.
			delete(m.watchedPending, path)
			return
		}
		m.watchedPending[path] = merged
	} else {
		m.watchedPending[path] = typ
	}
	if m.watchedTimer == nil {
		m.watchedTimer = time.AfterFunc(m.watchedDelay, m.flushWatched)
	} else {
		m.watchedTimer.Reset(m.watchedDelay)
	}
}

// mergeChangeTypes coalesces a follow-up change type onto a pending one.
// keep=false means the pair cancels out (create followed by delete).
func mergeChangeTypes(old, next int) (merged int, keep bool) {
	switch {
	case old == protocol.FileChangeCreated && next == protocol.FileChangeDeleted:
		return 0, false
	case old == protocol.FileChangeCreated && next == protocol.FileChangeChanged:
		return protocol.FileChangeCreated, true // still brand-new to the server
	case old == protocol.FileChangeDeleted && next == protocol.FileChangeCreated:
		return protocol.FileChangeChanged, true // replaced in place
	}
	return next, true
}

// flushWatched sends the accumulated events to every server whose watchers (or
// language fallback) match. Runs on the debounce-timer goroutine; the sends
// happen outside the manager lock.
func (m *Manager) flushWatched() {
	m.mu.Lock()
	batch := m.watchedPending
	m.watchedPending = make(map[string]int)
	m.watchedTimer = nil
	paths := make([]string, 0, len(batch))
	for p := range batch {
		paths = append(paths, p)
	}
	sort.Strings(paths) // deterministic wire order
	type out struct {
		srv     *server
		changes []protocol.FileEvent
	}
	var outs []out
	for _, srv := range m.servers {
		var changes []protocol.FileEvent
		for _, p := range paths {
			if m.serverWantsLocked(srv, p, batch[p]) {
				changes = append(changes, protocol.FileEvent{URI: protocol.PathToURI(p), Type: batch[p]})
			}
		}
		if len(changes) > 0 {
			outs = append(outs, out{srv: srv, changes: changes})
		}
	}
	m.mu.Unlock()
	for _, o := range outs {
		_ = o.srv.cl.DidChangeWatchedFiles(protocol.DidChangeWatchedFilesParams{Changes: o.changes})
	}
}

// serverWantsLocked decides whether one event is relevant for srv. Caller
// holds m.mu. With registered watchers the globs (and their kind bits) decide;
// without any registration the fallback forwards events for files whose
// language the server handles and that lie under the server's root.
func (m *Manager) serverWantsLocked(srv *server, path string, typ int) bool {
	if len(srv.watchers) > 0 {
		for _, set := range srv.watchers {
			for _, w := range set {
				if watcherApplies(w, srv.root, path, typ) {
					return true
				}
			}
		}
		return false
	}
	if !underDir(srv.root, path) {
		return false
	}
	l, ok := langreg.ByPath(path)
	return ok && l.ServerLang() == srv.lang
}

// watcherApplies reports whether one registered watcher covers (path, typ).
func watcherApplies(w protocol.FileSystemWatcher, root, path string, typ int) bool {
	kind := protocol.WatchAll
	if w.Kind != nil {
		kind = *w.Kind
	}
	switch typ {
	case protocol.FileChangeCreated:
		if kind&protocol.WatchCreate == 0 {
			return false
		}
	case protocol.FileChangeChanged:
		if kind&protocol.WatchChange == 0 {
			return false
		}
	case protocol.FileChangeDeleted:
		if kind&protocol.WatchDelete == 0 {
			return false
		}
	}
	pat := filepath.ToSlash(w.GlobPattern.Pattern)
	slashPath := filepath.ToSlash(path)
	if strings.HasPrefix(pat, "/") {
		// Absolute glob: match the full path.
		return globMatch(pat, slashPath)
	}
	// Relative glob: resolve against the RelativePattern base (when given) or
	// the server root, and match the base-relative path.
	base := root
	if w.GlobPattern.BaseURI != "" {
		if p := protocol.URIToPath(w.GlobPattern.BaseURI); p != "" {
			base = p
		}
	}
	if !underDir(base, path) {
		return false
	}
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return globMatch(pat, filepath.ToSlash(rel))
}

// underDir reports whether path lies strictly inside dir.
func underDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// normalizeAbs mirrors watch.absPath: a stable absolute form for map keys.
func normalizeAbs(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

// globMatch is a pragmatic matcher for LSP watcher globs over slash-separated
// paths. Supported: `**` (any number of path segments, including none), `*`
// (any run within one segment), `?` (one character within a segment), `{a,b}`
// (alternation, nestable), `[...]` (character class with ranges, `!`/`^`
// negation). Limits, documented rather than solved (#1144): matching is
// byte-wise (multi-byte runes count per byte under `?`/`[...]`), there is no
// escape character, and an unterminated group matches literally. This covers
// what real servers register (`**/*.php`, `**/composer.json`,
// `**/*.{ts,tsx}`) without a new dependency.
func globMatch(pattern, s string) bool {
	// Expand top-level {a,b} groups first: each alternative becomes its own
	// pattern. Nested groups expand recursively.
	if alts, ok := expandBraces(pattern); ok {
		for _, alt := range alts {
			if globMatch(alt, s) {
				return true
			}
		}
		return false
	}
	return matchGlob(pattern, s)
}

// expandBraces rewrites the first {…} group into one pattern per alternative.
// ok=false when the pattern has no (complete) group.
func expandBraces(pattern string) ([]string, bool) {
	open := strings.IndexByte(pattern, '{')
	if open < 0 {
		return nil, false
	}
	depth := 0
	var alts []string
	segStart := open + 1
	for i := open; i < len(pattern); i++ {
		switch pattern[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				alts = append(alts, pattern[segStart:i])
				out := make([]string, 0, len(alts))
				for _, a := range alts {
					out = append(out, pattern[:open]+a+pattern[i+1:])
				}
				return out, true
			}
		case ',':
			if depth == 1 {
				alts = append(alts, pattern[segStart:i])
				segStart = i + 1
			}
		}
	}
	return nil, false // unterminated: treat literally
}

// matchGlob matches a brace-free pattern against s.
func matchGlob(p, s string) bool {
	if p == "" {
		return s == ""
	}
	if strings.HasPrefix(p, "**") {
		rest := strings.TrimPrefix(p, "**")
		rest = strings.TrimPrefix(rest, "/")
		if rest == "" {
			return true
		}
		// Try the remainder at every segment boundary, including the start
		// ( `**/` matches zero directories).
		if matchGlob(rest, s) {
			return true
		}
		for i := 0; i < len(s); i++ {
			if s[i] == '/' && matchGlob(rest, s[i+1:]) {
				return true
			}
		}
		return false
	}
	if s == "" {
		// Only a trailing run of `*`s can match emptiness.
		return strings.Trim(p, "*") == ""
	}
	switch p[0] {
	case '*':
		// Any run of non-separator bytes (including empty).
		for i := 0; ; i++ {
			if matchGlob(p[1:], s[i:]) {
				return true
			}
			if i >= len(s) || s[i] == '/' {
				return false
			}
		}
	case '?':
		if s[0] == '/' {
			return false
		}
		return matchGlob(p[1:], s[1:])
	case '[':
		end := strings.IndexByte(p, ']')
		if end < 0 {
			break // unterminated class: literal '['
		}
		if s[0] == '/' {
			return false
		}
		if !classMatch(p[1:end], s[0]) {
			return false
		}
		return matchGlob(p[end+1:], s[1:])
	}
	if p[0] != s[0] {
		return false
	}
	return matchGlob(p[1:], s[1:])
}

// classMatch evaluates one [...] body (without brackets) against a byte.
func classMatch(class string, c byte) bool {
	if class == "" {
		return false
	}
	negate := false
	if class[0] == '!' || class[0] == '^' {
		negate = true
		class = class[1:]
	}
	hit := false
	for i := 0; i < len(class); i++ {
		if i+2 < len(class) && class[i+1] == '-' {
			if class[i] <= c && c <= class[i+2] {
				hit = true
			}
			i += 2
			continue
		}
		if class[i] == c {
			hit = true
		}
	}
	return hit != negate
}
