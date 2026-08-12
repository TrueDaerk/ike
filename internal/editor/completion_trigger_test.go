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
	// Fuzzy matching (#845) also keeps the scattered Sprintf, but the anchored
	// prefix must count into the filter and rank Println first.
	items := m.filteredCompletion()
	if len(items) == 0 || items[0].Label != "Println" {
		t.Fatalf("prefix 'Pr' should rank Println first, got %+v", items)
	}
}

// TestIncompleteListRequeriesOnTyping guards #849: with an isIncomplete
// reply, identifier runes typed while the popup shows re-emit the completion
// trigger (the bridge re-queries); a complete reply keeps the old
// filter-only behavior.
func TestIncompleteListRequeriesOnTyping(t *testing.T) {
	for _, tc := range []struct {
		incomplete bool
		want       int
	}{{true, 2}, {false, 0}} {
		m, _ := loaded(t, "\n")
		m = insertModeAt(m, 0, 0)
		m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 0, IsIncomplete: tc.incomplete, Items: []ilsp.CompletionItem{
			{Label: "alpha", InsertText: "alpha"},
			{Label: "aleph", InsertText: "aleph"},
		}})
		got := collectTriggers(&m)
		m = send(m, key('a'), key('l'))
		if len(*got) != tc.want {
			t.Fatalf("incomplete=%v: triggers = %v, want %d re-queries", tc.incomplete, *got, tc.want)
		}
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

// TestStaleEmptyPopupDoesNotSwallowArrows guards #1810: a completion reply
// that lands after the typed prefix has moved past it filters down to nothing
// and therefore draws no popup — the arrows must still move the caret instead
// of being eaten by the invisible list.
func TestStaleEmptyPopupDoesNotSwallowArrows(t *testing.T) {
	m, _ := loaded(t, "first\nself.x = 500\nthird\n")
	m = insertModeAt(m, 1, 12) // end of line, right after typing "500"
	// The reply answers the trigger from the first digit; by now the prefix
	// is "500", which matches none of the items.
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 1, Col: 10, Items: []ilsp.CompletionItem{
		{Label: "self", InsertText: "self"},
	}})
	if m.CompletionOpen() {
		t.Fatal("a list filtered down to nothing must not count as an open popup")
	}

	m = send(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.cursor.Line != 0 {
		t.Fatalf("up arrow after typing at line end: cursor = %+v, want line 0", m.cursor)
	}
	m = send(m, tea.KeyPressMsg{Code: tea.KeyDown}, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor.Line != 2 {
		t.Fatalf("down arrow: cursor = %+v, want line 2", m.cursor)
	}
}

// TestOpenPopupStillOwnsArrows keeps the popup's list navigation intact: with
// matching items showing, up/down move the selection, not the caret.
func TestOpenPopupStillOwnsArrows(t *testing.T) {
	m, _ := loaded(t, "first\nfmt.\nthird\n")
	m = insertModeAt(m, 1, 4)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 1, Col: 4, Items: []ilsp.CompletionItem{
		{Label: "Println", InsertText: "Println"},
		{Label: "Printf", InsertText: "Printf"},
	}})
	if !m.CompletionOpen() {
		t.Fatal("completion popup should be open")
	}
	m = send(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor.Line != 1 {
		t.Fatalf("the open popup owns down: cursor = %+v, want line 1", m.cursor)
	}
	if m.comp == nil || m.comp.sel != 1 {
		t.Fatalf("down should move the popup selection, got %+v", m.comp)
	}
}
