package manager

import (
	"errors"
	"strings"
)

// manualrestart.go is the user-driven counterpart to restart.go's crash
// recovery (#2148): the `lsp.restart` palette command and the Language Servers
// settings page stop servers *and* re-open the documents they were tracking, so
// language features come back on the buffers already on screen — including
// after crash recovery gave up, which is the only way to lift the give-up
// block. Stopping alone (StopLang/Shutdown) leaves the manager empty until the
// next file open, which is why those are not enough on their own.

// errServerDisabled is returned by ensureServer for a server key crash recovery
// gave up on. Callers treat it as "no server, and nothing to report" — the
// give-up already told the user, with the restart command.
var errServerDisabled = errors.New("language server disabled after repeated crashes")

// reopenDoc is a document snapshot taken before the servers go down.
type reopenDoc struct {
	path   string
	langID string
	text   string
}

// RestartAll stops every server and re-opens all tracked documents against the
// respawned ones. Blocking (it runs the initialize handshake per language), so
// callers stay off the Update goroutine — the bridge wraps it in a tea.Cmd.
func (m *Manager) RestartAll() {
	docs := m.snapshotDocs("")
	m.Shutdown() // clears servers, documents, diagnostics and the give-up blocks
	m.reopen(docs)
}

// RestartLang stops one language's servers (all roots) and re-opens that
// language's documents. Same contract as RestartAll, scoped to lang.
func (m *Manager) RestartLang(lang string) {
	docs := m.snapshotDocs(lang)
	m.StopLang(lang)
	m.reopen(docs)
}

// snapshotDocs copies the open documents (all, or one server language) so they
// can be re-opened after the servers went down. Documents are keyed by the
// *server* language, while the didOpen languageId is the document's own (#1063).
func (m *Manager) snapshotDocs(lang string) []reopenDoc {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []reopenDoc
	for path, doc := range m.docs {
		if lang != "" && doc.lang != lang {
			continue
		}
		out = append(out, reopenDoc{path: path, langID: doc.langID, text: strings.Join(doc.lines, "\n")})
	}
	return out
}

// reopen re-registers snapshotted documents, spawning their servers again, and
// asks the host to re-pull the decorations that only refresh on request.
func (m *Manager) reopen(docs []reopenDoc) {
	if len(docs) == 0 {
		return
	}
	for _, d := range docs {
		_ = m.Open(d.path, d.langID, d.text) // failures report themselves via status
	}
	m.refreshDecorations()
}
