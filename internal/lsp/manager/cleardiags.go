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

// flushPublished emits an empty publish for every path lang's servers ever
// published — unopened paths included — and forgets the set (#1102): the
// Problems store aggregates project-wide, so entries from a stopped or
// disabled language must not survive as stale findings. Open documents are
// additionally covered by the callers' own #994 clears; the double empty
// publish is harmless.
func (m *Manager) flushPublished(lang string) {
	m.mu.Lock()
	paths := m.published[lang]
	delete(m.published, lang)
	// Open documents are cleared by the callers' own #994 paths — skip them
	// here so nobody gets a duplicate empty publish.
	skip := make(map[string]bool)
	for p, d := range m.docs {
		if d.lang == lang {
			skip[p] = true
		}
	}
	m.mu.Unlock()
	for p := range paths {
		if !skip[p] {
			m.publishEmpty(p, nil, 0)
		}
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
