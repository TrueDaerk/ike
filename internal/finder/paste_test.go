package finder

import (
	"testing"

	"ike/internal/search"
)

// TestPasteIntoQuery guards #1273: a paste lands in the focused field at the
// cursor. The overlay used to swallow pastes outright.
func TestPasteIntoQuery(t *testing.T) {
	m := opened(t)
	typeText(m, "need")
	if !m.Paste("le") {
		t.Fatal("Paste reported not handled")
	}
	if m.query != "needle" {
		t.Fatalf("query = %q, want %q", m.query, "needle")
	}
	if m.cur != len([]rune("needle")) {
		t.Fatalf("cursor = %d, want it after the pasted text", m.cur)
	}
}

// TestPasteIntoReplacement: the paste follows the focused field, so replace
// mode's replacement input receives it once focus moves there.
func TestPasteIntoReplacement(t *testing.T) {
	m := New(search.New(nil))
	m.SetSize(100, 30)
	m.OpenReplace(t.TempDir())
	m.setFocus(fieldReplace)
	if !m.Paste("thread") {
		t.Fatal("Paste reported not handled")
	}
	if m.replace != "thread" {
		t.Fatalf("replace = %q, want %q", m.replace, "thread")
	}
	if m.query != "" {
		t.Fatalf("query = %q, want the unfocused field untouched", m.query)
	}
}

// TestPasteReplacesPreselectedQuery: a paste over the remembered, selected
// query replaces it wholesale — the same as typing over it (#277).
func TestPasteReplacesPreselectedQuery(t *testing.T) {
	m := opened(t)
	typeText(m, "needle")
	m.Update(key("esc"))

	m.Open(t.TempDir())
	if !m.preselect {
		t.Fatal("re-open should preselect the remembered query")
	}
	if !m.Paste("haystack") {
		t.Fatal("Paste reported not handled")
	}
	if m.query != "haystack" {
		t.Fatalf("query = %q, want the prefill replaced", m.query)
	}
	if m.preselect {
		t.Fatal("the prefill selection should be dropped after a paste")
	}
}

// TestPasteFlattensMultiline: single-line fields join a multi-line block.
func TestPasteFlattensMultiline(t *testing.T) {
	m := opened(t)
	if !m.Paste("alpha\nbravo\n") {
		t.Fatal("Paste reported not handled")
	}
	if m.query != "alpha bravo" {
		t.Fatalf("query = %q, want %q", m.query, "alpha bravo")
	}
}

// TestPasteEmptyIsNoOp: a block that flattens to nothing changes nothing.
func TestPasteEmptyIsNoOp(t *testing.T) {
	m := opened(t)
	typeText(m, "keep")
	if m.Paste("  \n\t\n ") {
		t.Fatal("Paste of a blank block should report not handled")
	}
	if m.query != "keep" {
		t.Fatalf("query = %q, want it unchanged", m.query)
	}
}
