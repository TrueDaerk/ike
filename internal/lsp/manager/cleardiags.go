package manager

import "ike/internal/lsp/protocol"

// cleardiags.go implements diagnostics teardown (#994): when a server is
// disabled after repeated crashes or deliberately stopped, its last publish
// must leave the editor — no diagnostics is truthful, frozen ones are not.
// Restart attempts deliberately keep diagnostics (a successful restart
// republishes anyway).

// clearServerDiagnostics drops everything a dead server last published — the
// host publish of each of its tracked documents plus any fragment publishes
// it served inside other hosts — and re-emits the merged set per affected
// host. Diagnostics from servers that still run survive the merge.
func (m *Manager) clearServerDiagnostics(srvKey string, docs []*document) {
	republish := map[string]bool{}
	m.mu.Lock()
	for _, d := range docs {
		delete(m.hostDiags, d.path)
		republish[d.path] = true
	}
	for host, fds := range m.frags {
		for slot, fd := range fds {
			if fd.srvKey != srvKey {
				continue
			}
			if _, ok := m.fragDiags[host][slot]; ok {
				delete(m.fragDiags[host], slot)
				republish[host] = true
			}
		}
		if len(m.fragDiags[host]) == 0 {
			delete(m.fragDiags, host)
		}
	}
	m.mu.Unlock()
	for host := range republish {
		m.publishHostDiagnostics(host)
	}
}

// publishEmpty tells the editor a document has no diagnostics anymore. Used
// when the document itself leaves the manager (StopLang, Shutdown), where
// publishHostDiagnostics would no-op on the missing document.
func (m *Manager) publishEmpty(path string, lines []string, version int) {
	if m.cb.Diagnostics == nil {
		return
	}
	m.cb.Diagnostics(path, protocol.PublishDiagnosticsParams{
		URI:         protocol.PathToURI(path),
		Version:     version,
		Diagnostics: []protocol.Diagnostic{},
	}, lines, "")
}
