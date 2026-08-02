package lsp

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/manager"
	"ike/internal/lsp/protocol"
)

// inheritance.go carries the bridge cores of the inheritance navigation
// commands (#1451): go to super, go to implementations, and the type-hierarchy
// overlay feed. Empty answers surface as toasts per #858 — never silently.

// goToSuper resolves the super declaration of the symbol under the cursor.
// TypeHierarchy-capable servers answer via prepare + supertypes; without the
// capability (or when it yields nothing on a method) the bidirectional
// textDocument/implementation answer is the fallback — on a concrete method
// gopls returns the interface method, which is exactly "super" in Go.
func (b *bridge) goToSuper(h host.API) tea.Cmd {
	b.ensure(h)
	path, line, col := b.cur()
	mgr := b.manager()
	if path == "" || mgr == nil {
		return nil
	}
	go func() {
		pos := buffer.Position{Line: line, Col: col}
		if mgr.TypeHierarchySupported(path) {
			items, err := mgr.PrepareTypeHierarchy(context.Background(), path, pos)
			if requestFailed(h, "go to super", err) {
				return
			}
			if len(items) > 0 {
				supers, err := mgr.Supertypes(context.Background(), path, items[0])
				if requestFailed(h, "go to super", err) {
					return
				}
				if len(supers) > 0 {
					h.Send(ilsp.ImplementationsMsg{Refs: typeItemsToRefs(mgr, path, supers), Super: true})
					return
				}
			}
		}
		locs, err := mgr.Implementation(context.Background(), path, pos)
		if requestFailed(h, "go to super", err) {
			return
		}
		if len(locs) == 0 {
			supported := mgr.ImplementationSupported(path) || mgr.TypeHierarchySupported(path)
			h.Send(ilsp.ServerStatusMsg{Text: inheritanceNotice("go to super", "no super declaration found", supported), Kind: ilsp.ServerEventInfo})
			return
		}
		h.Send(ilsp.ImplementationsMsg{Refs: locationsToRefs(mgr, path, locs), Super: true})
	}()
	return nil
}

// implementations requests the implementations of the symbol under the cursor
// and delivers them like definition candidates: one target jumps, several
// open the picker.
func (b *bridge) implementations(h host.API) tea.Cmd {
	b.ensure(h)
	path, line, col := b.cur()
	mgr := b.manager()
	if path == "" || mgr == nil {
		return nil
	}
	go func() {
		locs, err := mgr.Implementation(context.Background(), path, buffer.Position{Line: line, Col: col})
		if requestFailed(h, "go to implementations", err) {
			return
		}
		if len(locs) == 0 {
			h.Send(ilsp.ServerStatusMsg{Text: inheritanceNotice("go to implementations", "no implementations found", mgr.ImplementationSupported(path)), Kind: ilsp.ServerEventInfo})
			return
		}
		h.Send(ilsp.ImplementationsMsg{Refs: locationsToRefs(mgr, path, locs)})
	}()
	return nil
}

// inheritanceNotice words an empty answer (#858): whether nothing was found
// or nobody could be asked.
func inheritanceNotice(op, empty string, supported bool) string {
	if !supported {
		return op + " unavailable: the language server does not support it"
	}
	return empty
}

// typeHierarchy prepares the type hierarchy for the symbol under the cursor
// (#1451) and sends a TypeHierarchyMsg carrying the root items plus the Fetch
// continuation the overlay expands nodes with. Nothing prepared (position not
// on a type, or the server lacks the capability) surfaces as a toast.
func (b *bridge) typeHierarchy(h host.API) tea.Cmd {
	b.ensure(h)
	path, line, col := b.cur()
	mgr := b.manager()
	if path == "" || mgr == nil {
		return nil
	}
	go func() {
		items, err := mgr.PrepareTypeHierarchy(context.Background(), path, buffer.Position{Line: line, Col: col})
		if requestFailed(h, "type hierarchy", err) {
			return
		}
		if len(items) == 0 {
			h.Send(ilsp.ServerStatusMsg{Text: inheritanceNotice("type hierarchy", "no type hierarchy here", mgr.TypeHierarchySupported(path)), Kind: ilsp.ServerEventInfo})
			return
		}
		roots := make([]ilsp.TypeHierarchyEntry, len(items))
		for i, it := range items {
			roots[i] = typeHierEntry(mgr, path, it)
		}
		h.Send(ilsp.TypeHierarchyMsg{
			Path:  path,
			Roots: roots,
			Fetch: func(reqID int, item protocol.TypeHierarchyItem, supertypes bool) tea.Cmd {
				return b.fetchTypes(h, path, reqID, item, supertypes)
			},
		})
	}()
	return nil
}

// fetchTypes expands one hierarchy node into its supertypes or subtypes.
func (b *bridge) fetchTypes(h host.API, path string, reqID int, item protocol.TypeHierarchyItem, supertypes bool) tea.Cmd {
	mgr := b.manager()
	if mgr == nil {
		return nil
	}
	go func() {
		var items []protocol.TypeHierarchyItem
		var err error
		if supertypes {
			items, err = mgr.Supertypes(context.Background(), path, item)
		} else {
			items, err = mgr.Subtypes(context.Background(), path, item)
		}
		if requestFailed(h, "type hierarchy", err) {
			return
		}
		entries := make([]ilsp.TypeHierarchyEntry, len(items))
		for i, it := range items {
			entries[i] = typeHierEntry(mgr, path, it)
		}
		h.Send(ilsp.TypeHierarchyItemsMsg{ReqID: reqID, Supertypes: supertypes, Items: entries})
	}()
	return nil
}

// typeHierEntry converts one TypeHierarchyItem to its editor-coordinate entry
// (like hierEntry; the navigation target is the item's own declaration name).
func typeHierEntry(mgr *manager.Manager, path string, item protocol.TypeHierarchyItem) ilsp.TypeHierarchyEntry {
	ref := locationsToRefs(mgr, path, []protocol.Location{{URI: item.URI, Range: item.SelectionRange}})[0]
	return ilsp.TypeHierarchyEntry{
		Item:   item,
		Name:   item.Name,
		Detail: item.Detail,
		Path:   ref.Path,
		Line:   ref.Line,
		Col:    ref.Col,
	}
}

// typeItemsToRefs converts hierarchy items to picker references — their
// declaration positions, converted like any other location list.
func typeItemsToRefs(mgr *manager.Manager, path string, items []protocol.TypeHierarchyItem) []ilsp.Reference {
	locs := make([]protocol.Location, len(items))
	for i, it := range items {
		locs[i] = protocol.Location{URI: it.URI, Range: it.SelectionRange}
	}
	return locationsToRefs(mgr, path, locs)
}
