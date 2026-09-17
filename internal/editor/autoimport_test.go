package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	ilsp "ike/internal/lsp"
)

// autoimport_test.go covers the resolve-before-accept ordering of #2610: the
// auto-import a server delivers only through completionItem/resolve must land
// whether the reply arrives before the accept (cached on the popup) or after
// it (applied to the accepted text), and a resolve is requested for every
// selected server item, documentation or not.

// resolveEvents installs an emitter recording the select/accept events.
func resolveEvents(m *Model) *[]Event {
	var evs []Event
	m.SetEmitter(EmitterFunc(func(e Event) {
		if e.Kind == EventCompletionSelect || e.Kind == EventCompletionAccept {
			evs = append(evs, e)
		}
	}))
	return &evs
}

// TestCompletionResolveRequestedDespiteInlineDoc: an item that ships
// documentation still resolves (#2610) — the import edit only comes by
// resolve — and the inline doc renders meanwhile.
func TestCompletionResolveRequestedDespiteInlineDoc(t *testing.T) {
	m, _ := loaded(t, "ab\n")
	evs := resolveEvents(&m)
	m = insertModeAt(m, 0, 2)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 2, Seq: 4, Source: ilsp.SourceLSP, SourcePriority: ilsp.PriorityLSP, Items: []ilsp.CompletionItem{
		{Label: "abc", InsertText: "abc", ID: 1, Doc: "inline docs", Source: ilsp.SourceLSP},
	}})
	if len(*evs) != 1 || (*evs)[0].Kind != EventCompletionSelect || (*evs)[0].CompletionID != 1 || (*evs)[0].CompletionSeq != 4 {
		t.Fatalf("events = %+v, want one select for item 1 of reply 4", *evs)
	}
	if v := m.CompletionView(); !strings.Contains(v, "inline docs") {
		t.Fatalf("popup must render the inline doc, got:\n%s", v)
	}
}

// TestCompletionAcceptBeforeResolveAppliesLateImport: Enter before the
// resolve answered emits an accept event for the item; the reply's
// additionalTextEdits then apply above the accepted text and shift the cursor.
func TestCompletionAcceptBeforeResolveAppliesLateImport(t *testing.T) {
	m, _ := loaded(t, "x = tld\n")
	evs := resolveEvents(&m)
	m = insertModeAt(m, 0, 7)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 7, Seq: 2, Source: ilsp.SourceLSP, SourcePriority: ilsp.PriorityLSP, Items: []ilsp.CompletionItem{
		{Label: "tldextract", InsertText: "tldextract", ID: 3, Source: ilsp.SourceLSP},
	}})
	m = send(m, special(tea.KeyEnter))
	if got := line(m, 0); got != "x = tldextract" {
		t.Fatalf("line 0 = %q, want the completed symbol", got)
	}
	if n := len(*evs); n != 2 || (*evs)[1].Kind != EventCompletionAccept || (*evs)[1].CompletionID != 3 || (*evs)[1].CompletionSeq != 2 {
		t.Fatalf("events = %+v, want select then accept for item 3 of reply 2", *evs)
	}
	// The reply lands after the accept: the import goes in at (0,0), the
	// accepted line moves down and the cursor with it.
	m, _ = m.Update(ilsp.CompletionResolveMsg{Path: m.path, ID: 3, Seq: 2, AdditionalEdits: []ilsp.FormatEdit{
		{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 0, Text: "import tldextract\n"},
	}})
	if got := line(m, 0); got != "import tldextract" {
		t.Fatalf("line 0 = %q, want the late import", got)
	}
	if got := line(m, 1); got != "x = tldextract" {
		t.Fatalf("line 1 = %q, want the accepted text shifted down", got)
	}
	if m.cursor.Line != 1 || m.cursor.Col != 14 {
		t.Fatalf("cursor = %v, want (1,14) after the import shifted the line", m.cursor)
	}
	// One reply, one application: a duplicate is ignored.
	m, _ = m.Update(ilsp.CompletionResolveMsg{Path: m.path, ID: 3, Seq: 2, AdditionalEdits: []ilsp.FormatEdit{
		{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 0, Text: "import tldextract\n"},
	}})
	if got := line(m, 1); got != "x = tldextract" {
		t.Fatalf("a second reply must not apply again, doc: %q", m.buf.Lines())
	}
}

// TestCompletionLateImportSurvivesTypingAfterAccept: typing on after the
// accept (same line, further right) keeps the late import armed.
func TestCompletionLateImportSurvivesTypingAfterAccept(t *testing.T) {
	m, _ := loaded(t, "r = APIRou\n")
	m = insertModeAt(m, 0, 10)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 10, Seq: 1, Source: ilsp.SourceLSP, SourcePriority: ilsp.PriorityLSP, Items: []ilsp.CompletionItem{
		{Label: "APIRouter", InsertText: "APIRouter", Detail: "fastapi", ID: 0, Source: ilsp.SourceLSP},
	}})
	m = send(m, special(tea.KeyEnter))
	m = typeKeys(m, "()")
	m, _ = m.Update(ilsp.CompletionResolveMsg{Path: m.path, ID: 0, Seq: 1, AdditionalEdits: []ilsp.FormatEdit{
		{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 0, Text: "from fastapi import APIRouter\n\n\n"},
	}})
	want := []string{"from fastapi import APIRouter", "", "", "r = APIRouter()"}
	for i, w := range want {
		if got := line(m, i); got != w {
			t.Fatalf("line %d = %q, want %q (doc %q)", i, got, w, m.buf.Lines())
		}
	}
	if m.cursor.Line != 3 {
		t.Fatalf("cursor line = %d, want 3", m.cursor.Line)
	}
}

// TestCompletionLateImportAfterLeavingInsert: the user pressed Esc before the
// reply — the import still applies, as its own undoable change.
func TestCompletionLateImportAfterLeavingInsert(t *testing.T) {
	m, _ := loaded(t, "x = ToUp\n")
	m = insertModeAt(m, 0, 8)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 8, Seq: 1, Source: ilsp.SourceLSP, SourcePriority: ilsp.PriorityLSP, Items: []ilsp.CompletionItem{
		{Label: "ToUpper", InsertText: "strings.ToUpper", ID: 0, Source: ilsp.SourceLSP},
	}})
	m = send(m, special(tea.KeyEnter))
	m = send(m, special(tea.KeyEsc))
	if m.mode != Normal {
		t.Fatalf("mode = %v, want Normal", m.mode)
	}
	m, _ = m.Update(ilsp.CompletionResolveMsg{Path: m.path, ID: 0, Seq: 1, AdditionalEdits: []ilsp.FormatEdit{
		{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 0, Text: "import \"strings\"\n\n"},
	}})
	if got := line(m, 0); got != "import \"strings\"" || line(m, 2) != "x = strings.ToUpper" {
		t.Fatalf("doc = %q, want the import above the accepted text", m.buf.Lines())
	}
	if m.cursor.Line != 2 {
		t.Fatalf("cursor line = %d, want 2", m.cursor.Line)
	}
	if !m.dirty {
		t.Fatal("a late import dirties the buffer")
	}
	// Undo reverts the import alone, the accept stays.
	m = typeKeys(m, "u")
	if got := line(m, 0); got != "x = strings.ToUpper" {
		t.Fatalf("after undo line 0 = %q, want the accept without its import", got)
	}
}

// TestCompletionLateImportDroppedAfterUndo: undoing the accept before the
// reply lands disarms the import — the symbol it belongs to is gone.
func TestCompletionLateImportDroppedAfterUndo(t *testing.T) {
	m, _ := loaded(t, "x = tld\n")
	m = insertModeAt(m, 0, 7)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 7, Seq: 1, Source: ilsp.SourceLSP, SourcePriority: ilsp.PriorityLSP, Items: []ilsp.CompletionItem{
		{Label: "tldextract", InsertText: "tldextract", ID: 0, Source: ilsp.SourceLSP},
	}})
	m = send(m, special(tea.KeyEnter))
	m = send(m, special(tea.KeyEsc))
	m = typeKeys(m, "u")
	if got := line(m, 0); got != "x = tld" {
		t.Fatalf("after undo line 0 = %q, want the prefix back", got)
	}
	m, _ = m.Update(ilsp.CompletionResolveMsg{Path: m.path, ID: 0, Seq: 1, AdditionalEdits: []ilsp.FormatEdit{
		{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 0, Text: "import tldextract\n"},
	}})
	if m.buf.LineCount() != 1 || line(m, 0) != "x = tld" {
		t.Fatalf("doc = %q, want no import after the accept was undone", m.buf.Lines())
	}
}

// TestCompletionResolveSeqMismatchIgnored: a resolve stamped with another
// reply's sequence belongs to neither the pending accept nor the popup.
func TestCompletionResolveSeqMismatchIgnored(t *testing.T) {
	m, _ := loaded(t, "x = tld\n")
	m = insertModeAt(m, 0, 7)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 7, Seq: 5, Source: ilsp.SourceLSP, SourcePriority: ilsp.PriorityLSP, Items: []ilsp.CompletionItem{
		{Label: "tldextract", InsertText: "tldextract", ID: 0, Source: ilsp.SourceLSP},
	}})
	// Wrong Seq while the popup is open: nothing cached, so the accept
	// records a pending import instead of merging the edit.
	m, _ = m.Update(ilsp.CompletionResolveMsg{Path: m.path, ID: 0, Seq: 4, AdditionalEdits: []ilsp.FormatEdit{
		{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 0, Text: "import wrong\n"},
	}})
	m = send(m, special(tea.KeyEnter))
	if got := line(m, 0); got != "x = tldextract" {
		t.Fatalf("line 0 = %q, want the accept without the stale edit", got)
	}
	// Wrong Seq after the accept: dropped as well.
	m, _ = m.Update(ilsp.CompletionResolveMsg{Path: m.path, ID: 0, Seq: 6, AdditionalEdits: []ilsp.FormatEdit{
		{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 0, Text: "import wrong\n"},
	}})
	if m.buf.LineCount() != 1 {
		t.Fatalf("doc = %q, want the stale reply ignored", m.buf.Lines())
	}
	// The right one lands.
	m, _ = m.Update(ilsp.CompletionResolveMsg{Path: m.path, ID: 0, Seq: 5, AdditionalEdits: []ilsp.FormatEdit{
		{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 0, Text: "import tldextract\n"},
	}})
	if got := line(m, 0); got != "import tldextract" {
		t.Fatalf("line 0 = %q, want the matching reply's import", got)
	}
}

// TestCompletionNoAcceptEventWhenResolved: a cached resolve (reply before
// accept, #847) or inline additionalTextEdits need no accept round-trip.
func TestCompletionNoAcceptEventWhenResolved(t *testing.T) {
	m, _ := loaded(t, "ab\n")
	evs := resolveEvents(&m)
	m = insertModeAt(m, 0, 2)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 2, Seq: 1, Source: ilsp.SourceLSP, SourcePriority: ilsp.PriorityLSP, Items: []ilsp.CompletionItem{
		{Label: "abc", InsertText: "abc", ID: 0, Source: ilsp.SourceLSP},
	}})
	m, _ = m.Update(ilsp.CompletionResolveMsg{Path: m.path, ID: 0, Seq: 1, Doc: "docs"})
	m = send(m, special(tea.KeyEnter))
	for _, e := range *evs {
		if e.Kind == EventCompletionAccept {
			t.Fatalf("no accept event for an already resolved item, got %+v", *evs)
		}
	}
	if m.pendingImport != nil {
		t.Fatal("nothing pending after an accept with the resolve cached")
	}

	m, _ = loaded(t, "ab\n")
	evs = resolveEvents(&m)
	m = insertModeAt(m, 0, 2)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 2, Seq: 1, Source: ilsp.SourceLSP, SourcePriority: ilsp.PriorityLSP, Items: []ilsp.CompletionItem{
		{Label: "abc", InsertText: "abc", ID: 0, Source: ilsp.SourceLSP, AdditionalEdits: []ilsp.FormatEdit{
			{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 0, Text: "import abc\n"},
		}},
	}})
	m = send(m, special(tea.KeyEnter))
	for _, e := range *evs {
		if e.Kind == EventCompletionAccept {
			t.Fatalf("no accept event for an item with inline edits, got %+v", *evs)
		}
	}
	if got := line(m, 0); got != "import abc" {
		t.Fatalf("line 0 = %q, want the inline import", got)
	}
}

// TestCompletionKeepsSameInsertVariantsByDetail: two server items with the
// same insert text but different detail (auto-import candidates from two
// modules) both stay listed; across sources the higher-priority item wins.
func TestCompletionKeepsSameInsertVariantsByDetail(t *testing.T) {
	m, _ := loaded(t, "Pa\n")
	m = insertModeAt(m, 0, 2)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 2, Source: "words", SourcePriority: ilsp.PriorityWords, Items: []ilsp.CompletionItem{
		{Label: "Path", InsertText: "Path", Source: "words"},
	}})
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 2, Seq: 1, Source: ilsp.SourceLSP, SourcePriority: ilsp.PriorityLSP, Items: []ilsp.CompletionItem{
		{Label: "Path", InsertText: "Path", Detail: "pathlib", ID: 0, Source: ilsp.SourceLSP},
		{Label: "Path", InsertText: "Path", Detail: "mypkg.fs", ID: 1, Source: ilsp.SourceLSP},
		{Label: "Path", InsertText: "Path", Detail: "pathlib", ID: 2, Source: ilsp.SourceLSP},
	}})
	items := m.filteredCompletion()
	var got []string
	for _, it := range items {
		got = append(got, it.Source+":"+it.Detail)
	}
	want := []string{ilsp.SourceLSP + ":pathlib", ilsp.SourceLSP + ":mypkg.fs"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("items = %v, want %v", got, want)
	}
	if v := m.CompletionView(); !strings.Contains(v, "mypkg.fs") {
		t.Fatalf("popup must show the module detail, got:\n%s", v)
	}
}
