package palette

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/plugin"
	"ike/internal/registry"
)

// openPalette returns an open command palette for the paste tests.
func openPalette(t *testing.T) *Palette {
	t.Helper()
	src := fakeSource{cmds: []registry.OwnedCommand{
		owned("example.hello", "Say Hello", plugin.GlobalScope()),
	}}
	p := New(Config{DefaultPrefix: ':'}, NewCommandMode(src, nil, false), fileMode())
	p.SetSize(80, 24)
	p.Open(Context{ContextID: "editor"})
	return p
}

// TestPasteIntoQuery guards #1273: a paste lands in the query at the cursor.
// The overlay used to swallow pastes outright, so search-everywhere could not
// be pasted into at all.
func TestPasteIntoQuery(t *testing.T) {
	p := openPalette(t)
	p.Update(runes("hel"))
	if _, ok := p.Paste("lo world"); !ok {
		t.Fatal("Paste reported not handled")
	}
	if p.query.Text != "hello world" {
		t.Fatalf("query = %q, want %q", p.query.Text, "hello world")
	}
	if p.query.Cur != len([]rune("hello world")) {
		t.Fatalf("cursor = %d, want it after the pasted text", p.query.Cur)
	}
}

// TestPasteAtCursor: the block goes where the cursor is, not at the end.
func TestPasteAtCursor(t *testing.T) {
	p := openPalette(t)
	p.Update(runes("ho"))
	p.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if _, ok := p.Paste("ell"); !ok {
		t.Fatal("Paste reported not handled")
	}
	if p.query.Text != "hello" {
		t.Fatalf("query = %q, want %q", p.query.Text, "hello")
	}
}

// TestPasteKeepsPrefix: pasting into a prefixed query leaves the mode prefix
// intact, so a paste never switches modes out from under the user.
func TestPasteKeepsPrefix(t *testing.T) {
	p := openPalette(t)
	p.Update(runes("@main"))
	if _, ok := p.Paste(".go"); !ok {
		t.Fatal("Paste reported not handled")
	}
	if p.query.Text != "@main.go" {
		t.Fatalf("query = %q, want the prefix kept", p.query.Text)
	}
}

// TestPasteFlattensMultiline: the query is a single line, so a multi-line
// block is joined rather than dropped — and a trailing newline (a path copied
// from a shell) does not leave a trailing space.
func TestPasteFlattensMultiline(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"one\ntwo", "one two"},
		{"path/to/file.go\n", "path/to/file.go"},
		{"  a  \r\n\r\n  b  ", "a b"},
	} {
		p := openPalette(t)
		if _, ok := p.Paste(tc.in); !ok {
			t.Fatalf("Paste(%q) reported not handled", tc.in)
		}
		if p.query.Text != tc.want {
			t.Fatalf("Paste(%q): query = %q, want %q", tc.in, p.query.Text, tc.want)
		}
	}
}

// TestPasteEmptyIsNoOp: a block that flattens to nothing leaves the query and
// the cursor alone and reports unhandled.
func TestPasteEmptyIsNoOp(t *testing.T) {
	p := openPalette(t)
	p.Update(runes("keep"))
	if _, ok := p.Paste(" \n\t\n "); ok {
		t.Fatal("Paste of a blank block should report not handled")
	}
	if p.query.Text != "keep" {
		t.Fatalf("query = %q, want it unchanged", p.query.Text)
	}
}
