package manager

import (
	"strconv"
	"strings"
	"time"

	"ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// restart.go implements crash recovery: when a server's connection ends
// unexpectedly, the manager respawns it with a backoff, re-runs initialize (via
// ensureServer), and re-opens every document the crashed server was tracking, so
// the user keeps working. After maxRestarts consecutive crashes the server is
// disabled with a status message rather than thrashing.

const maxRestarts = 3

// restart respawns a crashed server and re-opens its documents. It runs on its
// own goroutine (off watchExit). errLine is the decisive error extracted from
// the crash's stderr tail ("" when none) — the terminal disable names it so
// the user sees why, not just that, the server went away (#990).
func (m *Manager) restart(old *server, docs []*document, errLine string) {
	k := old.key()
	m.mu.Lock()
	m.restarts[k]++
	n := m.restarts[k]
	m.mu.Unlock()

	if n > maxRestarts {
		// Persistent state for the status line, plus an error toast so the user
		// notices the subsystem went away — pointing at the log (#715).
		m.status(old.lang, old.lang+" language server disabled", lsp.ServerState)
		reason := ""
		if errLine != "" {
			reason = " (" + errLine + ")"
		}
		m.status(old.lang, old.lang+" language server disabled after repeated crashes"+reason+" — details: \"LSP: Show Server Log\"", lsp.ServerEventError)
		appendLog(old.lang, "disabled after repeated crashes")
		// Nobody maintains the dead server's findings anymore — drop them
		// from every affected editor (#994).
		m.clearServerDiagnostics(k, docs)
		m.flushPublished(old.lang) // stale project-wide findings go too (#1102)
		return
	}

	appendLog(old.lang, "restarting (attempt "+strconv.Itoa(n)+"/"+strconv.Itoa(maxRestarts)+")")
	time.Sleep(backoff(n))

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
}

// backoff grows linearly with the attempt count, capped at 5s.
func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}
