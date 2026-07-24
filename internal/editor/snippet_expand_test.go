package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	ilsp "ike/internal/lsp"
	"ike/internal/snippets"
)

// withSnippets installs entries as the process config for the test (#1152).
func withSnippets(t *testing.T, entries []config.SnippetEntry) {
	t.Helper()
	prev := config.Get()
	c := *prev
	c.Snippets = entries
	config.Set(&c)
	t.Cleanup(func() { config.Set(prev) })
}

func TestSnippetTabExpansionAndCycling(t *testing.T) {
	withSnippets(t, []config.SnippetEntry{
		{Trigger: "pair", Body: "left($1) right(${2:val})"},
	})
	m, _ := loaded(t, "\n")
	m = send(m, key('i'))
	m = typeKeys(m, "pair")
	m = send(m, tab())
	if got := line(m, 0); got != "left() right(val)" {
		t.Fatalf("expansion = %q", got)
	}
	// Cursor sits at $1; typing fills the first placeholder.
	if m.cursor.Col != 5 {
		t.Fatalf("cursor at col %d, want 5 ($1)", m.cursor.Col)
	}
	m = typeKeys(m, "ab")
	m = send(m, tab()) // to $2 (end of its default)
	if got := line(m, 0); got != "left(ab) right(val)" {
		t.Fatalf("after fill = %q", got)
	}
	if m.cursor.Col != 18 {
		t.Fatalf("second stop col = %d, want 18", m.cursor.Col)
	}
	m = send(m, shiftTab()) // back to $1's start
	if m.cursor.Col != 5 {
		t.Fatalf("shift+tab col = %d, want 5", m.cursor.Col)
	}
	if m.snippet == nil {
		t.Fatal("session must still be live while cycling")
	}
}

func TestSnippetTabNoTriggerFallsThroughToIndent(t *testing.T) {
	withSnippets(t, []config.SnippetEntry{{Trigger: "hit", Body: "X"}})
	m, _ := loaded(t, "\n")
	m = send(m, key('i'))
	m = typeKeys(m, "miss")
	m = send(m, tab())
	if got := line(m, 0); got != "miss\t" {
		t.Fatalf("non-trigger Tab must insert the tab text, got %q", got)
	}
	// And with no word at all (cursor after whitespace) Tab still indents.
	m = send(m, tab())
	if got := line(m, 0); got != "miss\t\t" {
		t.Fatalf("plain Tab = %q", got)
	}
}

func TestSnippetLanguageScopingInEditor(t *testing.T) {
	withSnippets(t, []config.SnippetEntry{
		{Trigger: "only", Language: "go", Body: "GO"},
	})
	// loaded() opens f.txt — not go — so the go-scoped entry must not fire.
	m, _ := loaded(t, "\n")
	m = send(m, key('i'))
	m = typeKeys(m, "only")
	m = send(m, tab())
	if got := line(m, 0); got != "only\t" {
		t.Fatalf("go-scoped trigger fired in a txt buffer: %q", got)
	}
}

func TestSnippetMultilineReindent(t *testing.T) {
	withSnippets(t, []config.SnippetEntry{
		{Trigger: "blk", Body: "if x {\n\t$1\n}"},
	})
	m, _ := loaded(t, "    \n")
	m.useSpaces = true
	m.tabWidth = 2
	m.SetCursor(0, 4)
	m = send(m, key('a')) // append after the indent
	m = typeKeys(m, "blk")
	m = send(m, tab())
	want := []string{
		"    if x {",
		"      ", // 4 (line indent) + 2 (\t as spaces)
		"    }",
	}
	for i, w := range want {
		if got := line(m, i); got != w {
			t.Fatalf("line %d = %q, want %q\nall: %q", i, got, w, m.buf.Lines())
		}
	}
	if m.cursor.Line != 1 || m.cursor.Col != 6 {
		t.Fatalf("cursor at %v, want line 1 col 6 ($1)", m.cursor)
	}
}

func TestSnippetConfigReloadAppliesToTab(t *testing.T) {
	withSnippets(t, nil)
	m, _ := loaded(t, "\n")
	m = send(m, key('i'))
	m = typeKeys(m, "brb")
	m = send(m, tab())
	if got := line(m, 0); got != "brb\t" {
		t.Fatalf("no entry yet, Tab must indent: %q", got)
	}
	// Simulate a config reload publishing new snippets.
	c := *config.Get()
	c.Snippets = []config.SnippetEntry{{Trigger: "brb", Body: "be right back"}}
	config.Set(&c)
	m = send(m, special(tea.KeyBackspace)) // drop the tab, cursor back after "brb"
	m = send(m, tab())
	if got := line(m, 0); got != "be right back" {
		t.Fatalf("reloaded trigger must expand: %q", got)
	}
}

func TestSnippetPopupItemAcceptExpandsAndReindents(t *testing.T) {
	withSnippets(t, nil)
	m, _ := loaded(t, "  \n")
	m.SetCursor(0, 2)
	m = send(m, key('a'))
	m = typeKeys(m, "blk")
	// A merged popup batch from the snippets source (#1152): accepting the
	// template expands through the snippet engine and re-indents like the
	// Tab-trigger path.
	m, _ = m.Update(ilsp.CompletionMsg{
		Path: m.path, Line: 0, Col: 5,
		Items: []ilsp.CompletionItem{{
			Label: "blk", FilterText: "blk", InsertText: "if x {\n\t$1\n}",
			IsSnippet: true, Source: snippets.SourceName,
		}},
		Source: snippets.SourceName, SourcePriority: ilsp.PrioritySnippets,
	})
	if !m.CompletionOpen() {
		t.Fatal("popup must open from the snippets batch alone (no LSP)")
	}
	m = send(m, tab()) // accept
	if got := line(m, 0); got != "  if x {" {
		t.Fatalf("line 0 = %q", got)
	}
	if got := line(m, 1); !strings.HasPrefix(got, "  \t") {
		t.Fatalf("continuation must inherit the line indent: %q", got)
	}
	if got := line(m, 2); got != "  }" {
		t.Fatalf("line 2 = %q", got)
	}
	if m.snippet == nil {
		t.Fatal("accepting the template must start the placeholder session")
	}
}
