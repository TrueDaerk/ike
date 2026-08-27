package editor

import (
	"strings"
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/highlight"
	"ike/internal/host"
)

// symStickyModel builds the same 40-line editor stickyModel uses, but without
// a Tree-sitter parse: the language has no grammar, so only the LSP fallback
// (#2167) can pin headers.
func symStickyModel(t *testing.T) Model {
	t.Helper()
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = "line" + itoa(i)
	}
	m := New()
	m.buf = buffer.FromString(strings.Join(lines, "\n"))
	m.path = "main.rs"
	m.SetSize(40, 10)
	return m
}

// symScopes mirrors the scopes the app derives from a two-level symbol tree.
func symScopes() []highlight.Scope {
	return []highlight.Scope{
		{HeaderLine: 0, EndLine: 30},
		{HeaderLine: 5, EndLine: 20},
	}
}

func TestStickyFromSymbolsWithoutGrammar(t *testing.T) {
	m := symStickyModel(t)
	if got := m.stickyLines(); got != nil {
		t.Fatalf("no scopes at all must pin nothing, got %v", got)
	}
	m.SetSymbolScopes("main.rs", symScopes())
	m.view.Top = 10
	got := m.stickyLines()
	if len(got) != 2 || got[0] != 0 || got[1] != 5 {
		t.Fatalf("stickyLines from symbols = %v, want [0 5]", got)
	}
}

func TestStickySymbolsInvalidatedByPath(t *testing.T) {
	// Scopes carry the file they were derived for: a buffer showing another
	// document must not pin headers from a stale delivery.
	m := symStickyModel(t)
	m.SetSymbolScopes("other.rs", symScopes())
	m.view.Top = 10
	if got := m.stickyLines(); got != nil {
		t.Fatalf("scopes of another file must not pin, got %v", got)
	}
}

func TestStickyTreeSitterScopesWinOverSymbols(t *testing.T) {
	// A language with a grammar keeps the parse's scopes: they need no server
	// and follow every keystroke. The symbol delivery is ignored while they
	// exist.
	m := symStickyModel(t)
	m.path = "main.go"
	m = feedSpans(t, m, highlight.SpansMsg{
		Path:   "main.go",
		Scopes: []highlight.Scope{{HeaderLine: 2, EndLine: 30}},
	})
	m.SetSymbolScopes("main.go", symScopes())
	m.view.Top = 10
	got := m.stickyLines()
	if len(got) != 1 || got[0] != 2 {
		t.Fatalf("stickyLines = %v, want the Tree-sitter header [2]", got)
	}
}

func TestStickySymbolsToggleOff(t *testing.T) {
	m := symStickyModel(t)
	m.SetSymbolScopes("main.rs", symScopes())
	m.view.Top = 10
	if got := m.stickyLines(); len(got) != 2 {
		t.Fatalf("setup: want two pinned headers, got %v", got)
	}
	m.stickySymbols = false
	if got := m.stickyLines(); got != nil {
		t.Fatalf("editor.sticky_scroll_symbols off must pin nothing, got %v", got)
	}
	// The plain sticky-scroll toggle still governs both sources.
	m.stickySymbols = true
	m.stickyScroll = false
	if got := m.stickyLines(); got != nil {
		t.Fatalf("editor.sticky_scroll off must pin nothing, got %v", got)
	}
}

func TestStickySymbolsRefreshInvalidatesMemo(t *testing.T) {
	// The header memo is keyed on the symbol epoch too (#2187): a second
	// delivery for the same document — a post-save refresh — must be seen.
	m := symStickyModel(t)
	m.SetSymbolScopes("main.rs", symScopes())
	m.view.Top = 10
	if got := m.stickyLines(); len(got) != 2 {
		t.Fatalf("setup: want two pinned headers, got %v", got)
	}
	m.SetSymbolScopes("main.rs", []highlight.Scope{{HeaderLine: 4, EndLine: 25}})
	got := m.stickyLines()
	if len(got) != 1 || got[0] != 4 {
		t.Fatalf("stickyLines after refresh = %v, want [4]", got)
	}
}

func TestStickySymbolsConfigToggle(t *testing.T) {
	cfg := host.MapConfig{}
	m := symStickyModel(t)
	m.Configure(cfg)
	m.SetSymbolScopes("main.rs", symScopes())
	m.view.Top = 10
	m.applyConfig()
	if got := m.stickyLines(); len(got) != 2 {
		t.Fatalf("default-on symbol fallback must pin, got %v", got)
	}
	cfg["editor.sticky_scroll_symbols"] = "false"
	m.applyConfig()
	if got := m.stickyLines(); got != nil {
		t.Fatalf("config toggle off must pin nothing, got %v", got)
	}
	cfg["editor.sticky_scroll_symbols"] = "true"
	m.applyConfig()
	if got := m.stickyLines(); len(got) != 2 {
		t.Fatalf("config toggle back on must restore the headers, got %v", got)
	}
}
