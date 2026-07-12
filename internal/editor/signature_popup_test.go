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
	m = insertModeAt(m, 0, 6) // signatures only show while typing the call (#315)
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
	m = send(m, key('x'))
	if !m.SignatureOpen() {
		t.Fatal("typing must not dismiss the popup")
	}
	m = send(m, special(tea.KeyEscape))
	if m.SignatureOpen() {
		t.Fatal("esc should dismiss the popup")
	}

	// Server null (empty label) clears it too.
	m = insertModeAt(m, 0, 6)
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
	m = insertModeAt(m, 0, 2)
	long := "print(" + strings.Repeat("value string, ", 60) + ")"
	m, _ = m.Update(ilsp.SignatureHelpMsg{Path: path, Label: long, ParamStart: 6, ParamEnd: 11})
	v := m.SignatureView()
	maxW := m.popupMaxWidth() + 4 // + the box padding and the frame (#316)
	for i, line := range strings.Split(v, "\n") {
		if w := lipgloss.Width(line); w > maxW {
			t.Fatalf("popup line %d width %d exceeds cap %d", i, w, maxW)
		}
	}
	// Content rows cap at popupMaxRows plus the ellipsis row; the frame adds
	// its two border rows (#316).
	if rows := len(strings.Split(v, "\n")); rows > popupMaxRows+3 {
		t.Fatalf("popup is %d rows, want at most %d + ellipsis + frame", rows, popupMaxRows+3)
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
	m = insertModeAt(m, 0, 6)
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
	m = insertModeAt(m, 0, 6)
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

// TestPopupsCarryRoundedFrame guards #316: signature and completion popups
// ship the rounded overlay frame so they read as overlays, not buffer text.
func TestPopupsCarryRoundedFrame(t *testing.T) {
	m, path := loaded(t, "Greet(\n")
	m = insertModeAt(m, 0, 6)
	m, _ = m.Update(ilsp.SignatureHelpMsg{Path: path, Label: "Greet(name string)", ParamStart: 6, ParamEnd: 17})
	for _, corner := range []string{"╭", "╮", "╰", "╯"} {
		if v := ansi.Strip(m.SignatureView()); !strings.Contains(v, corner) {
			t.Fatalf("signature popup misses frame corner %q:\n%s", corner, v)
		}
	}
	m.dismissSignature()
	m = insertModeAt(m, 0, 6)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 6, Items: []ilsp.CompletionItem{{Label: "Greet", InsertText: "Greet"}}})
	for _, corner := range []string{"╭", "╮", "╰", "╯"} {
		if v := ansi.Strip(m.CompletionView()); !strings.Contains(v, corner) {
			t.Fatalf("completion popup misses frame corner %q:\n%s", corner, v)
		}
	}
}

// TestSignatureDismissalPaths guards #315: leaving insert dismisses the
// popup, and a reply landing after insert mode ended is dropped instead of
// re-anchoring at the normal-mode cursor. Insert-mode arrow motion no longer
// dismisses — the bridge retriggers at the new cursor and the server decides
// (#523).
func TestSignatureDismissalPaths(t *testing.T) {
	m, path := loaded(t, "Greet(x, y)\n")
	msg := ilsp.SignatureHelpMsg{Path: path, Label: "Greet(a int, b int)", ParamStart: 6, ParamEnd: 11}

	// Arrow motion in insert mode keeps the popup; the retrigger updates it.
	m = insertModeAt(m, 0, 6)
	m, _ = m.Update(msg)
	if !m.SignatureOpen() {
		t.Fatal("setup: popup open")
	}
	m = send(m, special(tea.KeyRight))
	if !m.SignatureOpen() {
		t.Fatal("insert-mode arrow motion must keep the popup (#523)")
	}

	// Leaving insert mode dismisses.
	m = send(m, special(tea.KeyEscape))
	if m.SignatureOpen() {
		t.Fatal("leaving insert mode must dismiss the popup")
	}

	// A stale reply after esc must not re-open it in normal mode.
	m, _ = m.Update(msg)
	if m.SignatureOpen() {
		t.Fatal("a signature reply in normal mode must be dropped")
	}
}

// TestSignatureManualAndFollow guards #523: a Manual reply opens the popup in
// normal mode, follow-up replies update it in any mode, and an empty follow-up
// dismisses it.
func TestSignatureManualAndFollow(t *testing.T) {
	m, path := loaded(t, "Greet(x, y)\n")
	manual := ilsp.SignatureHelpMsg{Path: path, Label: "Greet(a int, b int)", Manual: true,
		Params:      []ilsp.SignatureParam{{Label: "a int", Start: 6, End: 11}, {Label: "b int", Start: 13, End: 18}},
		ActiveParam: 0, ParamStart: 6, ParamEnd: 11}

	m, _ = m.Update(manual)
	if !m.SignatureOpen() {
		t.Fatal("a manual reply must open the popup in normal mode")
	}

	// A non-manual follow-up (cursor-follow retrigger) updates the open popup.
	follow := manual
	follow.Manual = false
	follow.ActiveParam = 1
	m, _ = m.Update(follow)
	if !m.SignatureOpen() || m.signature.activeParam != 1 {
		t.Fatal("a follow-up reply must update the open popup in normal mode")
	}

	// The server answering null dismisses it.
	m, _ = m.Update(ilsp.SignatureHelpMsg{Path: path})
	if m.SignatureOpen() {
		t.Fatal("an empty follow-up must dismiss the popup")
	}
}

// TestSignatureParamListRendering guards the parameter-list layout (#523):
// one row per parameter, the active one marked, its doc below.
func TestSignatureParamListRendering(t *testing.T) {
	m, path := loaded(t, "Greet(\n")
	m = insertModeAt(m, 0, 6)
	m, _ = m.Update(ilsp.SignatureHelpMsg{Path: path, Label: "Greet(a int, b int)", Doc: "Greets.",
		Params: []ilsp.SignatureParam{
			{Label: "a int", Start: 6, End: 11, Doc: "first"},
			{Label: "b int", Start: 13, End: 18, Doc: "second"},
		},
		ActiveParam: 1, ParamStart: 13, ParamEnd: 18})
	v := ansi.Strip(m.SignatureView())
	for _, want := range []string{"▶ b int", "  a int", "second", "Greets."} {
		if !strings.Contains(v, want) {
			t.Fatalf("view missing %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "▶ a int") {
		t.Fatalf("inactive parameter must not carry the marker:\n%s", v)
	}
}
