package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	ilsp "ike/internal/lsp"
)

func TestSignaturePopupLifecycle(t *testing.T) {
	m, path := loaded(t, "Greet(\n")
	msg := ilsp.SignatureHelpMsg{Path: path, Label: "Greet(name string) string", ParamStart: 6, ParamEnd: 17, Doc: "Greets.", More: 1}
	m, _ = m.Update(msg)
	if !m.SignatureOpen() {
		t.Fatal("msg should open the popup")
	}
	v := ansi.Strip(m.SignatureView())
	for _, want := range []string{"Greet(", "name string", "Greets.", "+1 overloads"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view missing %q:\n%s", want, v)
		}
	}

	// Typing keeps it open (retrigger is server-driven), esc dismisses.
	m = send(m, key('i'), key('x'))
	if !m.SignatureOpen() {
		t.Fatal("typing must not dismiss the popup")
	}
	m = send(m, special(tea.KeyEscape))
	if m.SignatureOpen() {
		t.Fatal("esc should dismiss the popup")
	}

	// Server null (empty label) clears it too.
	m, _ = m.Update(msg)
	m, _ = m.Update(ilsp.SignatureHelpMsg{Path: path})
	if m.SignatureOpen() {
		t.Fatal("empty msg should clear the popup")
	}
}

func TestSignaturePopupIgnoresOtherFiles(t *testing.T) {
	m, _ := loaded(t, "x\n")
	m, _ = m.Update(ilsp.SignatureHelpMsg{Path: "/other.go", Label: "f()"})
	if m.SignatureOpen() {
		t.Fatal("msg for another path must be ignored")
	}
}
