package manager

import (
	"strings"
	"time"

	"ike/internal/lsp/protocol"
)

// restart.go implements crash recovery: when a server's connection ends
// unexpectedly, the manager respawns it with a backoff, re-runs initialize (via
// ensureServer), and re-opens every document the crashed server was tracking, so
// the user keeps working. After maxRestarts consecutive crashes the server is
// disabled with a status message rather than thrashing.

const maxRestarts = 3

// restart respawns a crashed server and re-opens its documents. It runs on its
// own goroutine (off watchExit).
func (m *Manager) restart(old *server, docs []*document) {
	k := old.key()
	m.mu.Lock()
	m.restarts[k]++
	n := m.restarts[k]
	m.mu.Unlock()

	if n > maxRestarts {
		m.status(old.lang, old.lang+" language server disabled after repeated crashes")
		return
	}

	time.Sleep(backoff(n))

	srv, err := m.ensureServer(old.lang, old.root, old.spec)
	if err != nil {
		m.status(old.lang, statusForErr(old.spec.Command, err))
		return
	}

	for _, d := range docs {
		m.mu.Lock()
		text := strings.Join(d.lines, "\n")
		version := d.version
		path, lang := d.path, d.lang
		m.mu.Unlock()
		_ = srv.cl.DidOpen(protocol.DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:        protocol.PathToURI(path),
				LanguageID: lang,
				Version:    version,
				Text:       text,
			},
		})
	}
	m.status(old.lang, old.lang+" language server restarted")
}

// backoff grows linearly with the attempt count, capped at 5s.
func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}
