//go:build cgo

package editor

import (
	"os"
	"path/filepath"
	"testing"

	ts "github.com/tree-sitter/go-tree-sitter"
	tsgo "github.com/tree-sitter/tree-sitter-go/bindings/go"

	"ike/internal/editor/buffer"
	"ike/internal/highlight"
	"ike/internal/lang"
)

// structvalue_cgo_test.go drives gy / gY through the key table against a real
// grammar. Like the selection-ladder tests it registers the Go grammar under
// its own extension — the language plugins are not linked into this package's
// tests, and a private id keeps every other test's association intact.

// yankValLoaded loads content under a .yankval path whose language carries the
// Go grammar.
func yankValLoaded(t *testing.T, content string) Model {
	t.Helper()
	lang.Register(lang.Language{
		ID:         "yankval",
		Extensions: []string{"yankval"},
		Grammar:    highlight.NewGrammar(ts.NewLanguage(tsgo.Language()), "(package_clause) @keyword"),
	})
	path := filepath.Join(t.TempDir(), "main.yankval")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(80, 10)
	m.SetFocused(true)
	return m
}

// TestYankValueChords (#2499): gy copies the decoded literal under the caret,
// gY the literal as the buffer holds it — and both record the copy in the
// clipboard history like every other yank.
func TestYankValueChords(t *testing.T) {
	const src = "package main\n\nvar s = \"a\\nb\"\n"
	for _, tc := range []struct {
		chord string
		want  string
	}{
		{"gy", "a\nb"},
		{"gY", `"a\nb"`},
	} {
		t.Run(tc.chord, func(t *testing.T) {
			m := yankValLoaded(t, src)
			m.cursor = buffer.Position{Line: 2, Col: 10} // inside the literal
			m = typeKeys(m, tc.chord)
			if got := m.regs.Get('"').Text; got != tc.want {
				t.Fatalf("%s yanked %q, want %q", tc.chord, got, tc.want)
			}
			hist := m.regs.History()
			if len(hist) == 0 || hist[0].Text != tc.want {
				t.Fatalf("clipboard history = %+v, want %q first", hist, tc.want)
			}
		})
	}
}

// TestYankValueOutsideAValue (#2499): off any literal both chords decline —
// nothing is copied and the registers stay as they were.
func TestYankValueOutsideAValue(t *testing.T) {
	m := yankValLoaded(t, "package main\n\nvar s = \"a\"\n")
	m.cursor = buffer.Position{Line: 0, Col: 9} // in the package name
	if got := noticeIn(t, m.yankStructuralValue(false)); got != "no structural value under the cursor" {
		t.Fatalf("notice = %q", got)
	}
	if got := m.regs.Get('"').Text; got != "" {
		t.Fatalf("yanked %q, want nothing", got)
	}
}

// TestYankValuePendingOperatorCancels (#2499): gy is a copy, not a motion, so
// `dgy` must not delete anything — the operator simply cancels.
func TestYankValuePendingOperatorCancels(t *testing.T) {
	m := yankValLoaded(t, "package main\n\nvar s = \"a\"\n")
	m.cursor = buffer.Position{Line: 2, Col: 10}
	m = typeKeys(m, "dgy")
	if got := m.buf.Line(2); got != `var s = "a"` {
		t.Fatalf("line after dgy = %q, want it untouched", got)
	}
	if got := m.regs.Get('"').Text; got != "" {
		t.Fatalf("dgy yanked %q, want nothing", got)
	}
}
