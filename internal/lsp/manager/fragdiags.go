package manager

import (
	"sort"

	"ike/internal/editor/buffer"
	"ike/internal/lsp/protocol"
)

// fragdiags.go maps diagnostics published on fragment documents back onto the
// host buffer and merges them with the host server's own diagnostics (0300,
// #415). The manager keeps the last publish per source — the host server's
// diagnostics per host path, and each fragment server's diagnostics per
// (host, slot) — and re-emits one merged publishDiagnostics for the host
// whenever any source changes. The bridge keeps seeing plain host-path
// diagnostics; it stays fragment-agnostic.

// fragDiagnostic is one fragment-server diagnostic stored in
// fragment-document editor coordinates. Mapping to host coordinates happens
// at publish time through the fragment's *current* range, so diagnostics
// follow the fragment when host edits move it.
type fragDiagnostic struct {
	rng  buffer.Range
	diag protocol.Diagnostic
}

// onFragmentDiagnostics stores a fragment server's publish for its slot and
// re-emits the host's merged diagnostics. A publish for an untracked fragment
// URI (stale, host closed) is dropped — its stored diagnostics are already
// gone.
func (m *Manager) onFragmentDiagnostics(p protocol.PublishDiagnosticsParams) {
	fd, ok := m.fragmentByURI(p.URI)
	if !ok {
		return
	}
	m.mu.Lock()
	enc := ""
	if srv := m.servers[fd.srvKey]; srv != nil {
		enc = srv.cl.Encoding()
	}
	stored := make([]fragDiagnostic, 0, len(p.Diagnostics))
	for _, d := range p.Diagnostics {
		stored = append(stored, fragDiagnostic{
			rng:  protocol.FromLSPRange(fd.frag.Lines, d.Range, enc),
			diag: d,
		})
	}
	fds := m.fragDiags[fd.hostPath]
	if fds == nil {
		fds = map[int][]fragDiagnostic{}
		m.fragDiags[fd.hostPath] = fds
	}
	fds[fd.slot] = stored
	m.mu.Unlock()
	m.publishHostDiagnostics(fd.hostPath)
}

// publishHostDiagnostics emits the merged diagnostics of an open host
// document: the host server's last publish plus every tracked fragment's last
// publish mapped into host coordinates. Fragment ranges are converted in the
// encoding the callback receiver will decode with (the host server's, with
// the same UTF-16 default ConvertDiagnostics applies), so round-trips are
// exact. No-op when the host document is not open.
func (m *Manager) publishHostDiagnostics(path string) {
	m.mu.Lock()
	doc := m.docs[path]
	if doc == nil {
		m.mu.Unlock()
		return
	}
	lines, version := doc.lines, doc.version
	enc := ""
	if srv := m.servers[doc.srvKey]; srv != nil {
		enc = srv.cl.Encoding()
	}
	merged := append([]protocol.Diagnostic(nil), m.hostDiags[path]...)
	slots := make([]int, 0, len(m.fragDiags[path]))
	for slot := range m.fragDiags[path] {
		slots = append(slots, slot)
	}
	sort.Ints(slots)
	for _, slot := range slots {
		fd := m.frags[path][slot]
		if fd == nil {
			continue
		}
		for _, d := range m.fragDiags[path][slot] {
			hr := buffer.Range{Start: fragToHost(fd.frag, d.rng.Start), End: fragToHost(fd.frag, d.rng.End)}
			dd := d.diag
			dd.Range = protocol.ToLSPRange(lines, hr, enc)
			merged = append(merged, dd)
		}
	}
	m.mu.Unlock()
	if m.cb.Diagnostics != nil {
		m.cb.Diagnostics(path, protocol.PublishDiagnosticsParams{
			URI:         protocol.PathToURI(path),
			Version:     version,
			Diagnostics: merged,
		}, lines, enc)
	}
}
