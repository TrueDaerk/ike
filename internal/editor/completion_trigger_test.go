package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	ilsp "ike/internal/lsp"
)

// collectTriggers installs an emitter recording every completion-trigger
// event's Char payload.
func collectTriggers(m *Model) *[]string {
	var got []string
	m.SetEmitter(EmitterFunc(func(e Event) {
		if e.Kind == EventCompletionTrigger {
			got = append(got, e.Char)
		}
	}))
	return &got
}

// TestTypedCharEmitsCompletionTrigger guards #527: every typed rune emits a
// completion trigger carrying the character, so the LSP bridge can match it
// against the server's trigger characters; ctrl+space stays the char-less
// manual request.
func TestTypedCharEmitsCompletionTrigger(t *testing.T) {
	m, _ := loaded(t, "fmt\n")
	got := collectTriggers(&m)
	m = insertModeAt(m, 0, 3)

	m = send(m, key('.'), key('P'), tea.KeyPressMsg{Code: ' ', Mod: tea.ModCtrl})
	want := []string{".", "P", ""}
	if len(*got) != len(want) {
		t.Fatalf("triggers = %v, want %v", *got, want)
	}
	for i, w := range want {
		if (*got)[i] != w {
			t.Fatalf("trigger %d = %q, want %q; all %v", i, (*got)[i], w, *got)
		}
	}
}

// TestReplaceModeDoesNotTrigger keeps replace-mode overtyping silent, matching
// the old "."-only behavior which also skipped replace mode.
func TestReplaceModeDoesNotTrigger(t *testing.T) {
	m, _ := loaded(t, "abc\n")
	got := collectTriggers(&m)
	m = insertModeAt(m, 0, 0)
	m.mode = Replace

	m = send(m, key('.'))
	if len(*got) != 0 {
		t.Fatalf("replace mode must not emit completion triggers, got %v", *got)
	}
}

// TestAutoCloseStillTriggers guards #527's auto-close criterion: a typed
// character the auto-close feature handled (pair insert, quote pairing) still
// emits its completion trigger.
func TestAutoCloseStillTriggers(t *testing.T) {
	m := autoCloseModel(t, "x\n")
	got := collectTriggers(&m)
	m = insertModeAt(m, 0, 1)

	m = send(m, key('('), key('"'))
	if len(*got) != 2 || (*got)[0] != "(" || (*got)[1] != `"` {
		t.Fatalf("auto-closed characters must still trigger, got %v", *got)
	}
}

// TestIdentTypingWithPopupOpenDoesNotRetrigger: while the popup is showing,
// identifier runes narrow the client-side prefix filter and must not re-query
// the server.
func TestIdentTypingWithPopupOpenDoesNotRetrigger(t *testing.T) {
	m, _ := loaded(t, "fmt.\n")
	m = insertModeAt(m, 0, 4)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 4, Items: []ilsp.CompletionItem{
		{Label: "Println", InsertText: "Println"},
	}})
	if !m.CompletionOpen() {
		t.Fatal("completion popup should be open")
	}
	got := collectTriggers(&m)
	m = send(m, key('P'), key('r'))
	if len(*got) != 0 {
		t.Fatalf("identifier typing with the popup open must not re-trigger, got %v", *got)
	}
	if !m.CompletionOpen() {
		t.Fatal("popup should stay open, filtered by the typed prefix")
	}
}

// TestCompletionAnchorAtIdentifierStart: a reply to an identifier-rune
// auto-trigger (#527) anchors at the identifier start, so the partial word
// typed before the request counts into the prefix filter.
func TestCompletionAnchorAtIdentifierStart(t *testing.T) {
	m, _ := loaded(t, "Pr\n")
	m = insertModeAt(m, 0, 2)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 2, Items: []ilsp.CompletionItem{
		{Label: "Println", InsertText: "Println"},
		{Label: "Sprintf", InsertText: "Sprintf"},
	}})
	if !m.CompletionOpen() {
		t.Fatal("completion popup should be open")
	}
	if col, _ := m.CompletionAnchor(); col != 0 {
		t.Fatalf("anchor col = %d, want 0 (identifier start)", col)
	}
	items := m.filteredCompletion()
	if len(items) != 1 || items[0].Label != "Println" {
		t.Fatalf("prefix 'Pr' should filter to Println, got %+v", items)
	}
}

// TestPasteDoesNotTrigger: multi-rune input (paste) never auto-triggers.
func TestPasteDoesNotTrigger(t *testing.T) {
	m, _ := loaded(t, "\n")
	got := collectTriggers(&m)
	m = insertModeAt(m, 0, 0)
	m.writeRunes("foo.bar")
	if len(*got) != 0 {
		t.Fatalf("multi-rune insert must not trigger, got %v", *got)
	}
}
