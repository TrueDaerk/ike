package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/registry"
)

// symScopeFile writes a 40-line file so a scrolled viewport has content below
// the pinned headers.
func symScopeFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.rs")
	if err := os.WriteFile(path, []byte(strings.Repeat("line body text\n", 40)), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// symScopeTree is a two-level tree: Outer spans 0-based lines 0..30 with a
// child spanning 5..20, plus a single-line constant that must be dropped.
func symScopeTree() []ilsp.SymbolNode {
	return []ilsp.SymbolNode{
		{Name: "Limit", Kind: 14, Line: 2, EndLine: 2},
		{
			Name: "Outer", Kind: 5, Line: 0, EndLine: 30,
			Children: []ilsp.SymbolNode{{Name: "inner", Kind: 6, Line: 5, EndLine: 20}},
		},
	}
}

func TestSymbolScopesConversion(t *testing.T) {
	got := symbolScopes(symScopeTree())
	want := []struct{ header, end int }{{0, 30}, {5, 20}}
	if len(got) != len(want) {
		t.Fatalf("symbolScopes = %v, want %d multi-line scopes (single-line symbols dropped)", got, len(want))
	}
	for i, w := range want {
		if got[i].HeaderLine != w.header || got[i].EndLine != w.end {
			t.Errorf("scope %d = %+v, want header %d end %d", i, got[i], w.header, w.end)
		}
	}
}

func TestSymbolScopesPreOrder(t *testing.T) {
	// enclosingHeaders walks the list once and expects outer before inner.
	scopes := symbolScopes([]ilsp.SymbolNode{{
		Name: "A", Line: 0, EndLine: 40,
		Children: []ilsp.SymbolNode{
			{Name: "B", Line: 1, EndLine: 10, Children: []ilsp.SymbolNode{{Name: "C", Line: 2, EndLine: 5}}},
			{Name: "D", Line: 12, EndLine: 20},
		},
	}})
	var headers []int
	for _, s := range scopes {
		headers = append(headers, s.HeaderLine)
	}
	if len(headers) != 4 || headers[0] != 0 || headers[1] != 1 || headers[2] != 2 || headers[3] != 12 {
		t.Fatalf("headers = %v, want [0 1 2 12]", headers)
	}
}

func TestSymbolScopesReachEditorOnDelivery(t *testing.T) {
	m := sized(t, 100, 40)
	path := symScopeFile(t)
	out, _ := m.openPath(path, false)
	m = out.(Model)
	canon := canonicalPath(path)
	if got := m.activeEditor().SymbolScopePath(); got != "" {
		t.Fatalf("no delivery yet, SymbolScopePath = %q", got)
	}
	m = step(m, ilsp.DocumentSymbolsMsg{Path: canon, Symbols: symScopeTree()})
	if got := m.activeEditor().SymbolScopePath(); got != canon {
		t.Fatalf("SymbolScopePath = %q, want %q", got, canon)
	}
}

func TestSymbolScopesFillLaterViewFromCache(t *testing.T) {
	// A view that missed the reply — a pane that opened the file after its
	// tree was cached, or a tab switched back to one — is filled by the
	// settled-pass sync from the cache alone; the request dedup would never
	// ask for that path again.
	m := sized(t, 100, 40)
	path := symScopeFile(t)
	out, _ := m.openPath(path, false)
	m = out.(Model)
	canon := canonicalPath(path)
	m = step(m, ilsp.DocumentSymbolsMsg{Path: canon, Symbols: symScopeTree()})
	m.activeEditor().SetSymbolScopes("", nil) // a view that never saw the reply
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if got := m.activeEditor().SymbolScopePath(); got != canon {
		t.Fatalf("the settled pass must fill the view from the cache, SymbolScopePath = %q", got)
	}
	// No extra LSP traffic: the cached tree answers the path, so the shared
	// sync issues nothing further for it.
	if cmd := m.structureSyncCmd(); cmd != nil {
		t.Fatal("an already-cached path must not issue another documentSymbol request")
	}
}

func TestSymbolScopesToggleGatesDelivery(t *testing.T) {
	cfg := host.MapConfig{"editor.sticky_scroll_symbols": "false"}
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := NewWith(registry.New(), cfg)
	out, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = out.(Model)
	path := symScopeFile(t)
	out, _ = m.openPath(path, false)
	m = out.(Model)
	canon := canonicalPath(path)
	m = step(m, ilsp.DocumentSymbolsMsg{Path: canon, Symbols: symScopeTree()})
	if got := m.activeEditor().SymbolScopePath(); got != "" {
		t.Fatalf("toggle off must not install scopes, SymbolScopePath = %q", got)
	}
	// Turning it back on fills the view on the next settled pass — the cached
	// tree is reused, no new request.
	cfg["editor.sticky_scroll_symbols"] = "true"
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if got := m.activeEditor().SymbolScopePath(); got != canon {
		t.Fatalf("toggle back on must fill from the cache, SymbolScopePath = %q", got)
	}
}
