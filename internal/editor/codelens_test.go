package editor

import (
	"testing"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
)

func lensesMsg(path string, lenses ...ilsp.CodeLens) ilsp.CodeLensesMsg {
	return ilsp.CodeLensesMsg{Path: path, Lenses: lenses}
}

// TestCodeLensesRenderTrailing renders a line's lenses as one dimmed trailing
// annotation after the buffer text, joined with a middle dot, without
// touching the buffer (#1912).
func TestCodeLensesRenderTrailing(t *testing.T) {
	m, path := loaded(t, "func TestFoo(t *testing.T) {\n}\n")
	m, _ = m.Update(lensesMsg(path,
		ilsp.CodeLens{Line: 0, Title: "run test"},
		ilsp.CodeLens{Line: 0, Title: "debug test"},
	))
	if got, want := firstLine(m), "func TestFoo(t *testing.T) {  run test · debug test"; got != want {
		t.Errorf("rendered = %q, want %q", got, want)
	}
	if m.buf.Line(0) != "func TestFoo(t *testing.T) {" {
		t.Error("lenses are virtual text and must not change the buffer")
	}

	// An empty reply clears the lenses.
	m, _ = m.Update(lensesMsg(path))
	if got := firstLine(m); got != "func TestFoo(t *testing.T) {" {
		t.Errorf("after clear rendered = %q", got)
	}
}

// TestCodeLensesConfigToggle hides the annotations while lsp.code_lens is
// off and resumes from the cached set when it comes back — rendering-only,
// like the inlay-hint toggle.
func TestCodeLensesConfigToggle(t *testing.T) {
	m, path := loaded(t, "func TestFoo() {\n}\n")
	m, _ = m.Update(lensesMsg(path, ilsp.CodeLens{Line: 0, Title: "run test"}))
	m.Configure(host.MapConfig{"lsp.code_lens": "false"})
	if got := firstLine(m); got != "func TestFoo() {" {
		t.Errorf("rendered = %q, lenses must hide while toggled off", got)
	}
	m.Configure(host.MapConfig{"lsp.code_lens": "true"})
	if got, want := firstLine(m), "func TestFoo() {  run test"; got != want {
		t.Errorf("rendered = %q, want %q (cached lenses resume)", got, want)
	}
}

// TestCodeLensesOtherPathIgnored keeps another document's lenses out.
func TestCodeLensesOtherPathIgnored(t *testing.T) {
	m, _ := loaded(t, "func TestFoo() {\n}\n")
	m, _ = m.Update(lensesMsg("/other.go", ilsp.CodeLens{Line: 0, Title: "run test"}))
	if got := firstLine(m); got != "func TestFoo() {" {
		t.Errorf("rendered = %q, other-path lenses must be ignored", got)
	}
}

// TestCodeLensesCoexistWithInlayHints keeps the lens annotation after the
// line-end inlay hints on the same line.
func TestCodeLensesCoexistWithInlayHints(t *testing.T) {
	m, path := loaded(t, "x := foo()\n")
	m.Configure(host.MapConfig{"lsp.inlay_hints": "true"}) // default off (#523)
	m, _ = m.Update(hintsMsg(path, ilsp.InlayHint{Line: 0, Col: 10, Label: "int", PadLeft: true}))
	m, _ = m.Update(lensesMsg(path, ilsp.CodeLens{Line: 0, Title: "1 reference"}))
	if got, want := firstLine(m), "x := foo() int  1 reference"; got != want {
		t.Errorf("rendered = %q, want %q", got, want)
	}
}
