package app

import (
	"ike/internal/highlight"
	ilsp "ike/internal/lsp"
	"ike/internal/pane"
)

// symbolscopes.go is the LSP half of sticky scroll (#2167): the same cached
// documentSymbol tree the Structure view (#1025) and the breadcrumbs bar
// (#1153) consume, converted into editor scopes so a language with no
// Tree-sitter grammar — Rust, Java, C#, or any language in a CGo-less build —
// still pins its enclosing declarations at the top of the viewport. No
// request is issued for it: the scopes are derived from docSymbols, which the
// existing settled-pass sync fills, and the editor prefers Tree-sitter scopes
// wherever the parse produces them.

// symbolScopes converts a documentSymbol tree into sticky-scroll scopes in
// pre-order (outer before inner), which is the order the editor's
// enclosingHeaders needs. Single-line symbols — constants, fields, one-line
// methods — are dropped: their header can never scroll out while the body is
// visible, so they would only crowd the depth cap.
func symbolScopes(syms []ilsp.SymbolNode) []highlight.Scope {
	var out []highlight.Scope
	var walk func(nodes []ilsp.SymbolNode)
	walk = func(nodes []ilsp.SymbolNode) {
		for _, n := range nodes {
			if n.EndLine > n.Line {
				out = append(out, highlight.Scope{HeaderLine: n.Line, EndLine: n.EndLine})
			}
			walk(n.Children)
		}
	}
	walk(syms)
	return out
}

// stickySymbolsOn reports whether the symbol fallback may feed sticky scroll:
// both editor.sticky_scroll and editor.sticky_scroll_symbols default on, so
// only an explicit "false" disables either. Read live from the config like
// breadcrumbsOn, so the settings toggle applies without a restart.
func (m Model) stickySymbolsOn() bool {
	cfg := m.host.Config()
	if v, ok := cfg.Get("editor.sticky_scroll"); ok && v == "false" {
		return false
	}
	v, ok := cfg.Get("editor.sticky_scroll_symbols")
	return !ok || v != "false"
}

// pushSymbolScopes hands a freshly cached tree to every editor pane showing
// path, so a documentSymbol reply — the first delivery or a post-save refresh
// — updates the pinned headers in the pass it lands in.
func (m *Model) pushSymbolScopes(path string, syms []ilsp.SymbolNode) {
	if path == "" || !m.stickySymbolsOn() {
		return
	}
	scopes := symbolScopes(syms)
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		if ed := inst.Editor(); ed != nil && ed.HasFile() && ed.Path() == path {
			ed.SetSymbolScopes(path, scopes)
		}
	}
}

// syncSymbolScopes runs once per settled Update pass and covers the views a
// reply could not reach: a pane that opened the file after its tree was
// cached, or a tab switch back to one. Only an editor whose installed scopes
// belong to another file is refilled, so the steady state costs one string
// compare per editor pane; with no tree cached at all — no LSP, or the toggle
// off — the pass returns immediately.
func (m *Model) syncSymbolScopes() {
	if len(m.docSymbols) == 0 || !m.stickySymbolsOn() {
		return
	}
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		ed := inst.Editor()
		if ed == nil || !ed.HasFile() || ed.SymbolScopePath() == ed.Path() {
			continue
		}
		syms, ok := m.docSymbols[ed.Path()]
		if !ok {
			continue
		}
		ed.SetSymbolScopes(ed.Path(), symbolScopes(syms))
	}
}
