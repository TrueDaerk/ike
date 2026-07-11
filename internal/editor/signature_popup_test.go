package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

// TestSignaturePopupClampsWidthAndHeight guards #306: an over-long signature
// wraps at the popup cap instead of widening past the pane, and a wrapped
// monster is cut at popupMaxRows with an ellipsis row.
func TestSignaturePopupClampsWidthAndHeight(t *testing.T) {
	m, path := loaded(t, "f(\n")
	m.SetSize(60, 20)
	long := "print(" + strings.Repeat("value string, ", 60) + ")"
	m, _ = m.Update(ilsp.SignatureHelpMsg{Path: path, Label: long, ParamStart: 6, ParamEnd: 11})
	v := m.SignatureView()
	maxW := m.popupMaxWidth() + 2 // + the box padding
	for i, line := range strings.Split(v, "\n") {
		if w := lipgloss.Width(line); w > maxW {
			t.Fatalf("popup line %d width %d exceeds cap %d", i, w, maxW)
		}
	}
	if rows := len(strings.Split(v, "\n")); rows > popupMaxRows+1 {
		t.Fatalf("popup is %d rows, want at most %d + ellipsis", rows, popupMaxRows+1)
	}
	if !strings.Contains(ansi.Strip(v), "…") {
		t.Fatal("the truncated popup should end in an ellipsis row")
	}
}

// TestMouseClickDismissesPopups guards #307: a click moves the cursor the
// popup anchors to, so it must dismiss signature and hover like a key does.
func TestMouseClickDismissesPopups(t *testing.T) {
	m, path := loaded(t, "Greet(\nsecond\n")
	m.SetSize(60, 10)
	m, _ = m.Update(ilsp.SignatureHelpMsg{Path: path, Label: "Greet(name string)", ParamStart: 6, ParamEnd: 17})
	if !m.SignatureOpen() {
		t.Fatal("setup: popup should be open")
	}
	m.MouseClick(2, 1)
	if m.SignatureOpen() {
		t.Fatal("a mouse click must dismiss the signature popup")
	}
}

// TestPopupAffordances guards #308: the completion popup shows its accept
// keys, the signature popup carries the informational marker.
func TestPopupAffordances(t *testing.T) {
	m, path := loaded(t, "Greet(\n")
	m, _ = m.Update(ilsp.SignatureHelpMsg{Path: path, Label: "Greet(name string)", ParamStart: 6, ParamEnd: 17})
	if v := ansi.Strip(m.SignatureView()); !strings.Contains(v, "ƒ ") {
		t.Fatalf("signature popup missing the ƒ marker:\n%s", v)
	}
	m.dismissSignature()
	m = insertModeAt(m, 0, 6)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 6, Items: []ilsp.CompletionItem{{Label: "Greet", InsertText: "Greet"}}})
	if !m.CompletionOpen() {
		t.Fatal("setup: completion should open")
	}
	if v := ansi.Strip(m.CompletionView()); !strings.Contains(v, "accept") {
		t.Fatalf("completion popup missing the accept hint:\n%s", v)
	}
}
