package manager

import (
	"context"
	"sort"
	"sync"
	"time"

	"ike/internal/editor/buffer"
	"ike/internal/lsp"
	"ike/internal/lsp/client"
	"ike/internal/lsp/protocol"
)

// inheritance.go adds the implementation / type-hierarchy wrappers and the
// per-document inheritance-mark batch (#1450). Everything is capability-gated
// the usual way: a missing provider is a graceful empty answer, never an error.

// Implementation requests the implementations of the symbol at an editor
// position, gated on capability.
func (m *Manager) Implementation(ctx context.Context, path string, pos buffer.Position) ([]protocol.Location, error) {
	return call(m, ctx, path, capImplementation, func(ctx context.Context, srv *server, doc *document) ([]protocol.Location, error) {
		return srv.cl.Implementation(ctx, protocol.ImplementationParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.PathToURI(path)},
			Position:     protocol.ToLSPPosition(doc.lines, pos, srv.cl.Encoding()),
		})
	})
}

// capImplementation gates textDocument/implementation.
func capImplementation(c client.Capabilities) bool { return c.Implementation }

// capTypeHierarchy gates the three textDocument/typeHierarchy forwarders.
func capTypeHierarchy(c client.Capabilities) bool { return c.TypeHierarchy }

// ImplementationSupported reports whether path currently has a ready server
// advertising the implementation capability (#858-style probe, so the caller
// can word an empty answer).
func (m *Manager) ImplementationSupported(path string) bool {
	srv, _, ok := m.docServer(path)
	return ok && srv.cl.Caps().Implementation
}

// PrepareTypeHierarchy resolves the symbol at an editor position into
// type-hierarchy items, gated on the server capability.
func (m *Manager) PrepareTypeHierarchy(ctx context.Context, path string, pos buffer.Position) ([]protocol.TypeHierarchyItem, error) {
	return call(m, ctx, path, capTypeHierarchy, func(ctx context.Context, srv *server, doc *document) ([]protocol.TypeHierarchyItem, error) {
		return srv.cl.PrepareTypeHierarchy(ctx, protocol.TypeHierarchyPrepareParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.PathToURI(path)},
			Position:     protocol.ToLSPPosition(doc.lines, pos, srv.cl.Encoding()),
		})
	})
}

// Supertypes requests the parents of a prepared item. Path names the document
// the hierarchy was prepared from and selects the server.
func (m *Manager) Supertypes(ctx context.Context, path string, item protocol.TypeHierarchyItem) ([]protocol.TypeHierarchyItem, error) {
	return call(m, ctx, path, capTypeHierarchy, func(ctx context.Context, srv *server, _ *document) ([]protocol.TypeHierarchyItem, error) {
		return srv.cl.Supertypes(ctx, protocol.TypeHierarchyItemParams{Item: item})
	})
}

// Subtypes requests the children of a prepared item.
func (m *Manager) Subtypes(ctx context.Context, path string, item protocol.TypeHierarchyItem) ([]protocol.TypeHierarchyItem, error) {
	return call(m, ctx, path, capTypeHierarchy, func(ctx context.Context, srv *server, _ *document) ([]protocol.TypeHierarchyItem, error) {
		return srv.cl.Subtypes(ctx, protocol.TypeHierarchyItemParams{Item: item})
	})
}

// TypeHierarchySupported reports whether path currently has a ready server
// advertising the typeHierarchy capability.
func (m *Manager) TypeHierarchySupported(path string) bool {
	srv, _, ok := m.docServer(path)
	return ok && srv.cl.Caps().TypeHierarchy
}

// DocVersion reports the manager-owned sync version of path's document — the
// inheritance-mark batch stamps its reply with the version it ran against, so
// a reply that raced an edit is recognisably stale (#1453).
func (m *Manager) DocVersion(path string) (int, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if doc, ok := m.docs[path]; ok {
		return doc.version, true
	}
	return 0, false
}

// maxInheritanceSymbols caps the per-file mark batch so one giant file never
// floods the server with implementation requests (cf. maxWorkspaceSymbols).
const maxInheritanceSymbols = 150

// inheritanceWorkers bounds the concurrent implementation requests of one batch.
const inheritanceWorkers = 4

// inheritanceBatchTimeout bounds the whole batch: a slow server degrades to no
// marks, never a hang.
const inheritanceBatchTimeout = 5 * time.Second

// inheritCandidate is one symbol worth probing: its name position (LSP
// coordinates, sent verbatim) and the arrow a non-empty answer earns.
type inheritCandidate struct {
	pos  protocol.Position
	kind int // lsp.InheritanceImplements / lsp.InheritanceImplemented
}

// InheritanceMarks computes the gutter inheritance marks of one document
// (#1450): a documentSymbol pass filtered to classes/methods/interfaces/
// structs, then one textDocument/implementation probe per symbol. Direction
// heuristic: an interface (or a method inside one) with implementations gets ↓,
// a concrete type or method whose probe answers gets ↑ — gopls's implementation
// is bidirectional, so one probe per symbol covers both arrows. Gated on both
// documentSymbol and implementation capabilities; missing either yields nil.
func (m *Manager) InheritanceMarks(ctx context.Context, path string) ([]lsp.InheritanceMark, error) {
	srv, doc, ok := m.docServer(path)
	if !ok || !srv.cl.Caps().Implementation || !srv.cl.Caps().DocumentSymbol {
		return nil, nil
	}
	enc := srv.cl.Encoding()
	lines := doc.lines
	uri := protocol.PathToURI(path)

	sctx, cancelSyms := context.WithTimeout(ctx, requestTimeout)
	syms, err := srv.cl.DocumentSymbols(sctx, protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	})
	cancelSyms()
	if err != nil {
		return nil, err
	}

	var candidates []inheritCandidate
	collectInheritCandidates(syms, 0, &candidates)
	if len(candidates) == 0 {
		return nil, nil
	}
	if len(candidates) > maxInheritanceSymbols {
		candidates = candidates[:maxInheritanceSymbols]
	}

	bctx, cancel := context.WithTimeout(ctx, inheritanceBatchTimeout)
	defer cancel()

	marks := make([]lsp.InheritanceMark, len(candidates))
	var wg sync.WaitGroup
	sem := make(chan struct{}, inheritanceWorkers)
	for i, cand := range candidates {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, cand inheritCandidate) {
			defer wg.Done()
			defer func() { <-sem }()
			locs, err := srv.cl.Implementation(bctx, protocol.ImplementationParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: uri},
				Position:     cand.pos,
			})
			if err != nil || len(locs) == 0 {
				return // an erroring probe only loses its own mark
			}
			line := protocol.FromLSPPosition(lines, cand.pos, enc).Line
			marks[i] = lsp.InheritanceMark{Line: line, Kind: cand.kind}
		}(i, cand)
	}
	wg.Wait()

	seen := map[int]bool{}
	out := marks[:0]
	for _, mk := range marks {
		if mk.Kind == 0 || seen[mk.Line] {
			continue // empty slot, or a second symbol on the same line
		}
		seen[mk.Line] = true
		out = append(out, mk)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// collectInheritCandidates walks the symbol tree keeping the kinds inheritance
// applies to. parentKind is the enclosing symbol's kind (0 at the root); a
// method inside an interface inherits the interface's ↓ direction.
func collectInheritCandidates(syms []protocol.DocumentSymbol, parentKind int, out *[]inheritCandidate) {
	for _, s := range syms {
		switch s.Kind {
		case protocol.SymKindInterface:
			*out = append(*out, inheritCandidate{pos: s.SelectionRange.Start, kind: lsp.InheritanceImplemented})
		case protocol.SymKindClass, protocol.SymKindStruct:
			*out = append(*out, inheritCandidate{pos: s.SelectionRange.Start, kind: lsp.InheritanceImplements})
		case protocol.SymKindMethod:
			kind := lsp.InheritanceImplements
			if parentKind == protocol.SymKindInterface {
				kind = lsp.InheritanceImplemented
			}
			*out = append(*out, inheritCandidate{pos: s.SelectionRange.Start, kind: kind})
		}
		collectInheritCandidates(s.Children, s.Kind, out)
	}
}
