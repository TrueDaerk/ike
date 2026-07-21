package manager

import (
	"context"
	"strconv"
	"strings"

	"ike/internal/editor/buffer"
	"ike/internal/highlight"
	"ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// fragments.go implements virtual documents for embedded-language fragments
// (Roadmap 0300, #413): each fragment a detector finds in an open host buffer
// (an SQL string in Python, …) is mirrored into a synthetic in-memory document
// with the fragment's language id, kept in sync with the host, and served by
// that language's ordinary managed server. LSP has no notion of embedded
// fragments, so the seam lives entirely here — the editor and bridge stay
// fragment-agnostic.
//
// Fragment documents use the ike-fragment: URI scheme; protocol.URIToPath
// passes non-file schemes through untouched, so these URIs survive round-trips
// but never collide with real files. Diagnostics published for fragment URIs
// map back onto the host buffer and merge with the host server's diagnostics
// (#415, fragdiags.go).

// FragmentDetector returns the embedded-language fragments of a host buffer.
// The bridge wires this to highlight.Fragments; tests inject fakes. It runs on
// manager goroutines and must be safe for concurrent use.
type FragmentDetector func(lang string, lines []string) []highlight.Fragment

// fragmentDoc is one synthetic document mirroring a fragment of a host buffer.
// slot is the fragment's ordinal in the detector output; it keys the URI, so a
// re-detected fragment in the same slot continues the same server-side
// document instead of churning open/close.
type fragmentDoc struct {
	hostPath string
	slot     int
	uri      string
	lang     string
	version  int
	frag     highlight.Fragment
	srvKey   string
}

const fragmentScheme = "ike-fragment:"

// fragmentURI builds the synthetic URI for a host path's fragment slot, e.g.
// ike-fragment://3/Users/x/app.py (the slot is the URI authority).
func fragmentURI(hostPath string, slot int) string {
	return fragmentScheme + "//" + strconv.Itoa(slot) + hostPath
}

// isFragmentURI reports whether uri belongs to a fragment document.
func isFragmentURI(uri string) bool { return strings.HasPrefix(uri, fragmentScheme) }

// SetFragmentDetector installs the embedded-fragment detector. Without one the
// manager never creates fragment documents.
func (m *Manager) SetFragmentDetector(fn FragmentDetector) {
	m.mu.Lock()
	m.detect = fn
	m.mu.Unlock()
}

// scheduleFragmentSync re-detects fragments for a host document off the caller
// goroutine (Change runs on the UI thread, and fragment servers may need a
// blocking initialize). A generation counter makes the newest schedule win, so
// a slow detection run never clobbers fresher state.
func (m *Manager) scheduleFragmentSync(hostPath string) {
	m.mu.Lock()
	detect := m.detect
	m.mu.Unlock()
	if detect == nil {
		return
	}
	go m.syncFragments(hostPath)
}

func (m *Manager) syncFragments(hostPath string) {
	m.mu.Lock()
	detect := m.detect
	doc, ok := m.docs[hostPath]
	if detect == nil || !ok {
		m.mu.Unlock()
		return
	}
	m.fragGen[hostPath]++
	gen := m.fragGen[hostPath]
	lines, hostLang := doc.lines, doc.lang
	m.mu.Unlock()

	found := detect(hostLang, lines)

	// Serialize reconciliation; only the newest generation may proceed, and the
	// host must still be open.
	m.fragMu.Lock()
	defer m.fragMu.Unlock()
	m.mu.Lock()
	stale := m.fragGen[hostPath] != gen || m.docs[hostPath] == nil
	m.mu.Unlock()
	if stale {
		return
	}
	m.reconcileFragments(hostPath, found)
}

// reconcileFragments diffs the detected fragments against the tracked fragment
// documents slot by slot: same slot + language updates in place (didChange on
// content change), anything else closes the old document and opens a new one.
// Fragments whose language resolves to no server are skipped silently — the
// host buffer just keeps its plain behavior. Caller holds fragMu.
func (m *Manager) reconcileFragments(hostPath string, found []highlight.Fragment) {
	m.mu.Lock()
	old := m.frags[hostPath]
	if old == nil {
		old = map[int]*fragmentDoc{}
	}
	next := map[int]*fragmentDoc{}
	m.mu.Unlock()
	// Slots whose fragment document continued in place; only their published
	// diagnostics stay valid — anything else was closed or reopened fresh.
	kept := map[int]bool{}

	for slot, fr := range found {
		spec, ok := m.resolve(fr.Lang)
		if !ok {
			continue
		}
		text := strings.Join(fr.Lines, "\n")

		if fd := old[slot]; fd != nil && fd.lang == fr.Lang {
			m.mu.Lock()
			srv := m.servers[fd.srvKey]
			oldText := strings.Join(fd.frag.Lines, "\n")
			m.mu.Unlock()
			if srv != nil {
				if text != oldText {
					var changes []protocol.TextDocumentContentChangeEvent
					switch srv.cl.Caps().SyncKind {
					case protocol.SyncNone:
						changes = nil
					case protocol.SyncIncremental:
						ev, changed := incrementalEvent(fd.frag.Lines, text, srv.cl.Encoding())
						if changed {
							changes = []protocol.TextDocumentContentChangeEvent{ev}
						}
					default:
						changes = []protocol.TextDocumentContentChangeEvent{{Text: text}}
					}
					if changes != nil {
						fd.version++
						_ = srv.cl.DidChange(protocol.DidChangeTextDocumentParams{
							TextDocument:   protocol.VersionedTextDocumentIdentifier{URI: fd.uri, Version: fd.version},
							ContentChanges: changes,
						})
					}
				}
				m.mu.Lock()
				fd.frag = fr // the range may shift even when the content did not
				m.mu.Unlock()
				delete(old, slot)
				next[slot] = fd
				kept[slot] = true
				continue
			}
			// Server gone (crash, StopLang): fall through and reopen below.
			delete(old, slot)
		}

		fd := m.openFragment(hostPath, slot, fr, text, spec)
		if fd != nil {
			next[slot] = fd
		}
	}

	// Whatever is left in old has no matching fragment anymore.
	for _, fd := range old {
		m.closeFragment(fd)
	}

	m.mu.Lock()
	if len(next) == 0 {
		delete(m.frags, hostPath)
	} else {
		m.frags[hostPath] = next
	}
	// Drop diagnostics of slots that did not continue in place (#415); the
	// surviving ones may have shifted with the host edit, so re-emit the
	// merged host diagnostics either way.
	republish := false
	if fds := m.fragDiags[hostPath]; fds != nil {
		for slot := range fds {
			if !kept[slot] {
				delete(fds, slot)
				republish = true
			}
		}
		if len(fds) == 0 {
			delete(m.fragDiags, hostPath)
		} else {
			republish = true
		}
	}
	m.mu.Unlock()
	if republish {
		m.publishHostDiagnostics(hostPath)
	}
}

// openFragment spawns/reuses the fragment language's server and sends didOpen.
// A failed spawn degrades silently (nil): the fragment simply stays plain text.
func (m *Manager) openFragment(hostPath string, slot int, fr highlight.Fragment, text string, spec lsp.ServerSpec) *fragmentDoc {
	root := detectRoot(hostPath, spec.RootMarkers)
	srv, err := m.ensureServer(fr.Lang, root, spec)
	if err != nil {
		return nil
	}
	fd := &fragmentDoc{
		hostPath: hostPath,
		slot:     slot,
		uri:      fragmentURI(hostPath, slot),
		lang:     fr.Lang,
		version:  1,
		frag:     fr,
		srvKey:   srv.key(),
	}
	_ = srv.cl.DidOpen(protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        fd.uri,
			LanguageID: fr.Lang,
			Version:    fd.version,
			Text:       text,
		},
	})
	return fd
}

// closeFragment sends didClose when the fragment's server is still alive.
func (m *Manager) closeFragment(fd *fragmentDoc) {
	m.mu.Lock()
	srv := m.servers[fd.srvKey]
	m.mu.Unlock()
	if srv == nil {
		return
	}
	_ = srv.cl.DidClose(protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: fd.uri},
	})
}

// closeFragmentsFor closes and forgets every fragment document of a host,
// including its published fragment diagnostics (the host is going away, so
// nothing is re-emitted).
func (m *Manager) closeFragmentsFor(hostPath string) {
	m.mu.Lock()
	fds := m.frags[hostPath]
	delete(m.frags, hostPath)
	delete(m.fragGen, hostPath)
	delete(m.fragDiags, hostPath)
	m.mu.Unlock()
	for _, fd := range fds {
		m.closeFragment(fd)
	}
}

// fragmentAt returns the fragment document and its server covering an editor
// position of the host buffer, when one exists. The end boundary is inclusive
// so completion right before a closing quote still routes into the fragment.
func (m *Manager) fragmentAt(hostPath string, pos buffer.Position) (*server, *fragmentDoc, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, fd := range m.frags[hostPath] {
		if !fragContains(fd.frag, pos) {
			continue
		}
		if srv := m.servers[fd.srvKey]; srv != nil {
			return srv, fd, true
		}
	}
	return nil, nil, false
}

// fragmentCompletion routes a completion request into the fragment covering
// pos, when one exists. handled=false means "not a fragment position; use the
// host server". A fragment whose server lacks the capability handles the
// request with an empty result — the host server has nothing useful to say
// about a position inside an embedded string either.
func (m *Manager) fragmentCompletion(ctx context.Context, hostPath string, pos buffer.Position, triggerChar string) (items []protocol.CompletionItem, incomplete bool, handled bool, err error) {
	srv, fd, ok := m.fragmentAt(hostPath, pos)
	if !ok {
		return nil, false, false, nil
	}
	if !srv.cl.Caps().Completion {
		return nil, false, true, nil
	}
	m.mu.Lock()
	frag, uri := fd.frag, fd.uri
	m.mu.Unlock()
	enc := srv.cl.Encoding()
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	items, incomplete, err = srv.cl.Completion(cctx, protocol.CompletionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     protocol.ToLSPPosition(frag.Lines, hostToFrag(frag, pos), enc),
		Context:      completionContext(triggerChar, srv.cl.Caps().CompletionTriggers),
	})
	if err != nil {
		return nil, false, true, err
	}
	hostLines, _ := m.DocLines(hostPath)
	for i := range items {
		if items[i].TextEdit != nil {
			items[i].TextEdit.Range = fragRangeToHost(frag, hostLines, items[i].TextEdit.Range, enc)
		}
	}
	return items, incomplete, true, nil
}

// fragmentHover routes a hover request into the fragment covering pos, when
// one exists; the result range maps back to host coordinates.
func (m *Manager) fragmentHover(ctx context.Context, hostPath string, pos buffer.Position) (h *protocol.Hover, handled bool, err error) {
	srv, fd, ok := m.fragmentAt(hostPath, pos)
	if !ok {
		return nil, false, nil
	}
	if !srv.cl.Caps().Hover {
		return nil, true, nil
	}
	m.mu.Lock()
	frag, uri := fd.frag, fd.uri
	m.mu.Unlock()
	enc := srv.cl.Encoding()
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	h, err = srv.cl.Hover(cctx, protocol.HoverParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     protocol.ToLSPPosition(frag.Lines, hostToFrag(frag, pos), enc),
	})
	if err != nil || h == nil {
		return h, true, err
	}
	if h.Range != nil {
		hostLines, _ := m.DocLines(hostPath)
		r := fragRangeToHost(frag, hostLines, *h.Range, enc)
		h.Range = &r
	}
	return h, true, nil
}

// fragmentDefinition routes a definition request into the fragment covering
// pos, when one exists (#416). Result locations inside fragment documents are
// rewritten to host-file locations; locations in real files pass through.
func (m *Manager) fragmentDefinition(ctx context.Context, hostPath string, pos buffer.Position) (locs []protocol.Location, handled bool, err error) {
	srv, fd, ok := m.fragmentAt(hostPath, pos)
	if !ok {
		return nil, false, nil
	}
	if !srv.cl.Caps().Definition {
		return nil, true, nil
	}
	m.mu.Lock()
	frag, uri := fd.frag, fd.uri
	m.mu.Unlock()
	enc := srv.cl.Encoding()
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	locs, err = srv.cl.Definition(cctx, protocol.DefinitionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     protocol.ToLSPPosition(frag.Lines, hostToFrag(frag, pos), enc),
	})
	if err != nil {
		return nil, true, err
	}
	return m.fragLocationsToHost(locs, enc), true, nil
}

// fragmentReferences routes a references request into the fragment covering
// pos, when one exists (#416), mapping result locations like
// fragmentDefinition.
func (m *Manager) fragmentReferences(ctx context.Context, hostPath string, pos buffer.Position, includeDecl bool) (locs []protocol.Location, handled bool, err error) {
	srv, fd, ok := m.fragmentAt(hostPath, pos)
	if !ok {
		return nil, false, nil
	}
	if !srv.cl.Caps().References {
		return nil, true, nil
	}
	m.mu.Lock()
	frag, uri := fd.frag, fd.uri
	m.mu.Unlock()
	enc := srv.cl.Encoding()
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	locs, err = srv.cl.References(cctx, protocol.ReferenceParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     protocol.ToLSPPosition(frag.Lines, hostToFrag(frag, pos), enc),
		Context:      protocol.ReferenceContext{IncludeDeclaration: includeDecl},
	})
	if err != nil {
		return nil, true, err
	}
	return m.fragLocationsToHost(locs, enc), true, nil
}

// fragmentDocumentHighlight routes a document-highlight request into the
// fragment covering pos, when one exists (#172). Result ranges are fragment
// coordinates and map back onto the host buffer directly — occurrences of a
// fragment symbol always live inside its own document.
func (m *Manager) fragmentDocumentHighlight(ctx context.Context, hostPath string, pos buffer.Position) (hs []lsp.DocumentHighlight, handled bool, err error) {
	srv, fd, ok := m.fragmentAt(hostPath, pos)
	if !ok {
		return nil, false, nil
	}
	if !srv.cl.Caps().DocumentHighlight {
		return nil, true, nil
	}
	m.mu.Lock()
	frag, uri := fd.frag, fd.uri
	m.mu.Unlock()
	enc := srv.cl.Encoding()
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	raw, err := srv.cl.DocumentHighlight(cctx, protocol.DocumentHighlightParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     protocol.ToLSPPosition(frag.Lines, hostToFrag(frag, pos), enc),
	})
	if err != nil {
		return nil, true, err
	}
	out := make([]lsp.DocumentHighlight, len(raw))
	for i, h := range raw {
		er := protocol.FromLSPRange(frag.Lines, h.Range, enc)
		out[i] = lsp.DocumentHighlight{
			Range: buffer.Range{Start: fragToHost(frag, er.Start), End: fragToHost(frag, er.End)},
			Kind:  h.Kind,
		}
	}
	return out, true, nil
}

// fragmentInlayHints collects the inlay hints of every embedded fragment of a
// host document (#171), mapped onto host coordinates. A fragment whose server
// lacks the capability or fails is skipped silently — hints are a passive
// decoration and the host document's own hints should still show.
func (m *Manager) fragmentInlayHints(ctx context.Context, hostPath string) []lsp.InlayHint {
	m.mu.Lock()
	fds := make([]*fragmentDoc, 0, len(m.frags[hostPath]))
	for _, fd := range m.frags[hostPath] {
		fds = append(fds, fd)
	}
	m.mu.Unlock()

	var out []lsp.InlayHint
	for _, fd := range fds {
		m.mu.Lock()
		srv := m.servers[fd.srvKey]
		frag, uri := fd.frag, fd.uri
		m.mu.Unlock()
		if srv == nil || !srv.cl.Caps().InlayHint || len(frag.Lines) == 0 {
			continue
		}
		enc := srv.cl.Encoding()
		cctx, cancel := context.WithTimeout(ctx, requestTimeout)
		hints, err := srv.cl.InlayHints(cctx, protocol.InlayHintParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Range:        wholeDocRange(frag.Lines, enc),
		})
		cancel()
		if err != nil {
			continue
		}
		for _, h := range hints {
			out = append(out, convertInlayHint(frag.Lines, h, enc, func(p buffer.Position) buffer.Position {
				return fragToHost(frag, p)
			}))
		}
	}
	return out
}

// fragLocationsToHost rewrites every fragment-URI location to the equivalent
// host-file location (host file URI, host coordinates). Locations in real
// files pass through untouched; a fragment location whose document is no
// longer tracked (stale, host closed) is dropped rather than surfaced as an
// unopenable synthetic URI.
func (m *Manager) fragLocationsToHost(locs []protocol.Location, enc string) []protocol.Location {
	out := make([]protocol.Location, 0, len(locs))
	for _, l := range locs {
		if !isFragmentURI(l.URI) {
			out = append(out, l)
			continue
		}
		fd, ok := m.fragmentByURI(l.URI)
		if !ok {
			continue
		}
		m.mu.Lock()
		frag := fd.frag
		m.mu.Unlock()
		hostLines, ok := m.DocLines(fd.hostPath)
		if !ok {
			continue
		}
		l.URI = protocol.PathToURI(fd.hostPath)
		l.Range = fragRangeToHost(frag, hostLines, l.Range, enc)
		out = append(out, l)
	}
	return out
}

// fragmentByURI returns the tracked fragment document behind a fragment URI,
// across all hosts (a fragment server may answer with locations in another
// host's fragments).
func (m *Manager) fragmentByURI(uri string) (*fragmentDoc, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, fds := range m.frags {
		for _, fd := range fds {
			if fd.uri == uri {
				return fd, true
			}
		}
	}
	return nil, false
}

// fragRangeToHost converts a fragment-document LSP range into the equivalent
// host-document LSP range, staying in the fragment server's encoding (today's
// consumers only read the edit text, not the range).
func fragRangeToHost(frag highlight.Fragment, hostLines []string, r protocol.Range, enc string) protocol.Range {
	er := protocol.FromLSPRange(frag.Lines, r, enc)
	hr := buffer.Range{Start: fragToHost(frag, er.Start), End: fragToHost(frag, er.End)}
	return protocol.ToLSPRange(hostLines, hr, enc)
}

// fragContains reports whether a host position lies inside the fragment,
// inclusive of the end boundary.
func fragContains(fr highlight.Fragment, pos buffer.Position) bool {
	if pos.Line < fr.StartLine || pos.Line > fr.EndLine {
		return false
	}
	if pos.Line == fr.StartLine && pos.Col < fr.StartCol {
		return false
	}
	if pos.Line == fr.EndLine && pos.Col > fr.EndCol {
		return false
	}
	return true
}

// hostToFrag maps a host-buffer position into fragment-document coordinates.
// Fragment content is exactly the host text of its range, so the mapping is a
// pure offset shift: only the first fragment line has a column offset.
func hostToFrag(fr highlight.Fragment, pos buffer.Position) buffer.Position {
	if pos.Line == fr.StartLine {
		return buffer.Position{Line: 0, Col: pos.Col - fr.StartCol}
	}
	return buffer.Position{Line: pos.Line - fr.StartLine, Col: pos.Col}
}

// fragToHost is the inverse of hostToFrag.
func fragToHost(fr highlight.Fragment, pos buffer.Position) buffer.Position {
	if pos.Line == 0 {
		return buffer.Position{Line: fr.StartLine, Col: pos.Col + fr.StartCol}
	}
	return buffer.Position{Line: pos.Line + fr.StartLine, Col: pos.Col}
}
