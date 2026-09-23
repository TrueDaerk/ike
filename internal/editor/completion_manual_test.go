package editor

import (
	"testing"

	ilsp "ike/internal/lsp"
)

// TestManualTriggerRequestsAtCaret guards #2695: completion.trigger's editor
// side emits the char-less request at the caret, which every consumer honours
// without the identifier-rune delay or a trigger-character match.
func TestManualTriggerRequestsAtCaret(t *testing.T) {
	m, _ := loaded(t, "fmt.Pri\n")
	m = insertModeAt(m, 0, 7)
	var got []Event
	m.SetEmitter(EmitterFunc(func(e Event) {
		if e.Kind == EventCompletionTrigger {
			got = append(got, e)
		}
	}))

	if !m.TriggerCompletion() {
		t.Fatal("manual trigger in insert mode must report that it fired")
	}
	if len(got) != 1 {
		t.Fatalf("triggers = %v, want exactly one", got)
	}
	if got[0].Char != "" {
		t.Errorf("Char = %q, want the empty manual-request marker", got[0].Char)
	}
	if got[0].Line != 0 || got[0].Col != 7 {
		t.Errorf("request at %d:%d, want the caret at 0:7", got[0].Line, got[0].Col)
	}
}

// TestManualTriggerFiltersByWordPrefix: the reply to a manual request anchors
// at the start of the word under the caret, so the popup opens filtered by
// what is already typed rather than listing everything the server sent.
func TestManualTriggerFiltersByWordPrefix(t *testing.T) {
	m, _ := loaded(t, "fmt.Pri\n")
	m = insertModeAt(m, 0, 7)
	m.TriggerCompletion()

	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 7, Items: []ilsp.CompletionItem{
		{Label: "Println", InsertText: "Println"},
		{Label: "Sprintf", InsertText: "Sprintf"},
	}})
	if !m.CompletionOpen() {
		t.Fatal("completion popup should be open")
	}
	if col, _ := m.CompletionAnchor(); col != 4 {
		t.Errorf("anchor col = %d, want the identifier start 4", col)
	}
	items := m.filteredCompletion()
	if len(items) != 1 || items[0].Label != "Println" {
		t.Fatalf("filtered = %v, want only Println for the prefix %q", items, "Pri")
	}
}

// TestManualTriggerRequeriesOpenPopup: unlike a typed identifier rune, which
// only narrows the client-side filter, a second ctrl+space re-asks — the way
// out of a server answer that came back incomplete.
func TestManualTriggerRequeriesOpenPopup(t *testing.T) {
	m, _ := loaded(t, "fmt.\n")
	m = insertModeAt(m, 0, 4)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 4, Items: []ilsp.CompletionItem{
		{Label: "Println", InsertText: "Println"},
	}})
	if !m.CompletionOpen() {
		t.Fatal("completion popup should be open")
	}
	got := collectTriggers(&m)

	if !m.TriggerCompletion() {
		t.Fatal("manual trigger must fire with the popup already open")
	}
	if len(*got) != 1 || (*got)[0] != "" {
		t.Fatalf("triggers = %v, want one char-less re-request", *got)
	}
}

// TestManualTriggerNormalModeIsNoOp: in normal mode there is nothing to
// complete, so the chord stays silent instead of erroring.
func TestManualTriggerNormalModeIsNoOp(t *testing.T) {
	m, _ := loaded(t, "fmt.Pri\n")
	got := collectTriggers(&m)

	if m.TriggerCompletion() {
		t.Error("manual trigger in normal mode must report that it did nothing")
	}
	if len(*got) != 0 {
		t.Fatalf("normal mode must emit no trigger, got %v", *got)
	}
}
