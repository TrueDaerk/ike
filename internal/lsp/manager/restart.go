package manager

import (
	"strconv"
	"strings"
	"time"

	"ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// restart.go implements crash recovery: when a server's connection ends
// unexpectedly, the manager respawns it after an exponential backoff, re-runs
// initialize (via ensureServer), and re-opens every document the crashed server
// was tracking, so the user keeps working without touching the file. Each stage
// reports itself on the status line — restarting (attempt i/N) while waiting,
// ready once the respawn answered initialize. After maxRestarts consecutive
// crashes the server is disabled (#2148): the key is blocked from further
// automatic spawns and the user gets a toast naming the manual restart command,
// rather than the manager thrashing a server that dies on every start.

const maxRestarts = 3

// restartDelays is the backoff schedule per attempt (1-based); attempts beyond
// the table reuse its last entry. Exponential on purpose: an instantly dying
// server must not turn into a restart storm, while the common case (a one-off
// crash) recovers within a second.
var restartDelays = []time.Duration{1 * time.Second, 5 * time.Second, 30 * time.Second}

// restartStableRun is the healthy uptime after which a crash counts as a new
// incident: a server that worked for this long before dying gets a fresh
// restart budget, so one bad hour does not disable LSP for the rest of a
// long session. Shorter-lived servers keep consuming the current budget,
// which is what makes the give-up reachable for a server that dies at once.
const restartStableRun = 2 * time.Minute

// restart respawns a crashed server and re-opens its documents. It runs on its
// own goroutine (off watchExit). errLine is the decisive error extracted from
// the crash's stderr tail ("" when none) — the terminal disable names it so
// the user sees why, not just that, the server went away (#990).
func (m *Manager) restart(old *server, docs []*document, errLine string) {
	k := old.key()
	m.mu.Lock()
	if !old.startedAt.IsZero() && time.Since(old.startedAt) >= m.stableRunWindow() {
		m.restarts[k] = 0 // it ran healthily for a while: fresh budget (#2148)
	}
	m.restarts[k]++
	n := m.restarts[k]
	m.mu.Unlock()

	if n > maxRestarts {
		m.giveUp(old, docs, errLine)
		return
	}

	attempt := strconv.Itoa(n) + "/" + strconv.Itoa(maxRestarts)
	appendLog(old.lang, "restarting (attempt "+attempt+")")
	// Persistent state while the backoff runs: the status line says the
	// subsystem is coming back, not that it is simply gone (#2148).
	m.status(old.lang, old.lang+" language server restarting (attempt "+attempt+")", lsp.ServerState)
	time.Sleep(m.backoff(n))

	// The world may have moved during the backoff: a shutdown, a manual
	// restart, or the last document of this server closing. Respawning then
	// would leak a process nobody asked for.
	m.mu.Lock()
	_, live := m.servers[k]
	blocked := m.disabled[k]
	docs = m.docsForServerLocked(k)
	wanted := len(docs) > 0 || m.hasFragmentsLocked(k)
	m.mu.Unlock()
	if live || blocked || !wanted {
		return
	}

	srv, err := m.ensureServer(old.lang, old.root, old.spec)
	if err != nil {
		text, kind := statusForErr(old.spec.Command, err)
		m.status(old.lang, text, kind)
		return
	}

	for _, d := range docs {
		m.mu.Lock()
		text := strings.Join(d.lines, "\n")
		version := d.version
		path, langID := d.path, d.langID
		// The fresh server never issued the cached semantic-token result id;
		// asking it for a delta against one would fail (#1912's refresh drops
		// them for the same reason).
		d.semData, d.semResultID = nil, ""
		m.mu.Unlock()
		_ = srv.cl.DidOpen(protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        protocol.PathToURI(path),
				LanguageID: langID,
				Version:    version,
				Text:       text,
			},
		})
	}

	// Fragment documents served by the crashed server (0300, #413): the
	// respawned server shares its key, so re-opening them restores state.
	m.mu.Lock()
	var frags []*fragmentDoc
	for _, fds := range m.frags {
		for _, fd := range fds {
			if fd.srvKey == k {
				frags = append(frags, fd)
			}
		}
	}
	m.mu.Unlock()
	for _, fd := range frags {
		m.mu.Lock()
		text := strings.Join(fd.frag.Lines, "\n")
		version := fd.version
		uri, lang := fd.uri, fd.lang
		m.mu.Unlock()
		_ = srv.cl.DidOpen(protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        uri,
				LanguageID: lang,
				Version:    version,
				Text:       text,
			},
		})
	}
	m.status(old.lang, old.lang+" language server restarted", lsp.ServerEventInfo)
	// Diagnostics come back on their own (the fresh server publishes), but
	// pull-style decorations only re-request on demand — the same refresh the
	// server itself would ask for (#1912) makes hints/lenses/tokens reappear
	// without an edit.
	m.refreshDecorations()
}

// giveUp disables a server key after maxRestarts consecutive crashes: no more
// automatic spawns until a manual restart, a status-line state saying so, and
// an error toast naming both the restart command and the log (#2148, #715).
func (m *Manager) giveUp(old *server, docs []*document, errLine string) {
	k := old.key()
	m.mu.Lock()
	m.disabled[k] = true
	m.mu.Unlock()

	// Persistent state for the status line, plus an error toast so the user
	// notices the subsystem went away — pointing at the recovery command and
	// the log (#715).
	m.status(old.lang, disabledStateText(old.lang), lsp.ServerState)
	reason := ""
	if errLine != "" {
		reason = " (" + errLine + ")"
	}
	m.status(old.lang, old.lang+" language server disabled after repeated crashes"+reason+
		" — restart: \""+restartCommandTitle+"\", details: \"LSP: Show Server Log\", diagnose: \"LSP: Doctor\"", lsp.ServerEventError)
	appendLog(old.lang, "disabled after repeated crashes")
	// Nobody maintains the dead server's findings anymore — drop them
	// from every affected editor (#994).
	m.clearServerDiagnostics(k, docs)
	m.flushPublished(old.lang) // stale project-wide findings go too (#1102)
}

// restartCommandTitle is the palette entry that revives a disabled server
// (plugins/lsp: command id lsp.restart). Named in the give-up toast and the
// status-line state so the way out is always one search away.
const restartCommandTitle = "LSP: Restart Servers"

// disabledStateText is the status-line state of a server crash recovery gave
// up on: it says the feature is off and how to get it back.
func disabledStateText(lang string) string {
	return lang + " language server failed — restart: \"" + restartCommandTitle + "\""
}

// backoff returns the wait before restart attempt n (1-based), from the
// manager's schedule (tests inject a fast one) or the default table.
func (m *Manager) backoff(attempt int) time.Duration {
	m.mu.Lock()
	fn := m.backoffFn
	m.mu.Unlock()
	if fn != nil {
		return fn(attempt)
	}
	return backoff(attempt)
}

// backoff grows exponentially with the attempt count: 1s, 5s, 30s.
func backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > len(restartDelays) {
		attempt = len(restartDelays)
	}
	return restartDelays[attempt-1]
}

// stableRunWindow is the configured healthy-uptime threshold. Caller may hold
// m.mu (it only reads an immutable-after-New field).
func (m *Manager) stableRunWindow() time.Duration {
	if m.stableRun > 0 {
		return m.stableRun
	}
	return restartStableRun
}

// hasFragmentsLocked reports whether any embedded-fragment document is served
// by srvKey. Caller holds m.mu.
func (m *Manager) hasFragmentsLocked(srvKey string) bool {
	for _, fds := range m.frags {
		for _, fd := range fds {
			if fd.srvKey == srvKey {
				return true
			}
		}
	}
	return false
}

// refreshDecorations asks the host to re-request the pull-style decorations for
// its open documents (#1912's callback), used after a restart re-opened the
// documents on a fresh server.
func (m *Manager) refreshDecorations() {
	if m.cb.Refresh == nil {
		return
	}
	for _, kind := range []string{"semanticTokens", "inlayHint", "codeLens"} {
		// Off the caller's goroutine, like the server-initiated refresh
		// (manager.go's onRequest): the host re-requests per open document.
		go m.cb.Refresh(kind)
	}
}
