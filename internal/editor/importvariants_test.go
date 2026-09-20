package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	ilsp "ike/internal/lsp"
)

// importvariants_test.go covers #2653: auto-import variants of one symbol
// (same source, label, insert text and kind, different import module) merge
// into the canonical item plus one folded "+N modules" entry; accepting the
// folded entry opens the picker over the variants, and picking one applies
// that variant exactly like a direct accept.

// loggingVariants is pyright's answer to `loggi` in a project where several
// modules re-export `logging`, in server order with the stdlib module not
// first.
func loggingVariants() []ilsp.CompletionItem {
	mk := func(id int, module string) ilsp.CompletionItem {
		return ilsp.CompletionItem{Label: "logging", InsertText: "logging", Kind: 9, ID: id, Source: ilsp.SourceLSP,
			Detail: module + " Auto-import", ImportModule: module,
			AdditionalEdits: []ilsp.FormatEdit{{Text: "import " + module + "\n"}}}
	}
	return []ilsp.CompletionItem{mk(0, "app.util"), mk(1, "logging"), mk(2, "app.cli"), mk(3, "app.a.b")}
}

func openLogging(t *testing.T, items []ilsp.CompletionItem) Model {
	t.Helper()
	m, _ := loaded(t, "loggi\n")
	m = insertModeAt(m, 0, 5)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 5, Seq: 1, Source: ilsp.SourceLSP, SourcePriority: ilsp.PriorityLSP, Items: items})
	return m
}

func rowsOf(items []ilsp.CompletionItem) string {
	var got []string
	for _, it := range items {
		got = append(got, completionLabel(it))
	}
	return strings.Join(got, " | ")
}

// TestCompletionFoldsImportVariants: four `logging` items merge into the
// stdlib one (module equals the label) plus one folded entry, in that order.
func TestCompletionFoldsImportVariants(t *testing.T) {
	m := openLogging(t, loggingVariants())
	items := m.filteredCompletion()
	if got, want := rowsOf(items), "logging logging Auto-import | logging   +3 modules"; got != want {
		t.Fatalf("rows = %q, want %q", got, want)
	}
	if items[0].ID != 1 || items[0].ImportModule != "logging" {
		t.Fatalf("canonical = %+v, want the stdlib module (ID 1)", items[0])
	}
	folded := items[1]
	if len(folded.Variants) != 3 || folded.ImportModule != "" || folded.AdditionalEdits != nil {
		t.Fatalf("folded entry = %+v, want three variants and no import of its own", folded)
	}
	if got, want := rowsOf(folded.Variants), "logging app.cli Auto-import | logging app.util Auto-import | logging app.a.b Auto-import"; got != want {
		t.Fatalf("variants = %q, want shortest module path first, then server order", got)
	}
	if completionItemKey(folded) == completionItemKey(items[0]) {
		t.Fatal("the folded entry needs its own selection identity")
	}
	if v := m.CompletionView(); !strings.Contains(v, "+3 modules") {
		t.Fatalf("popup must render the folded count, got:\n%s", v)
	}
}

// TestCompletionCanonicalFallsBackToShortestModule: with no module equal to
// the label the fewest-segment module wins, then the shorter string.
func TestCompletionCanonicalFallsBackToShortestModule(t *testing.T) {
	mk := func(id int, module string) ilsp.CompletionItem {
		return ilsp.CompletionItem{Label: "Path", InsertText: "Path", Kind: 7, ID: id, Source: ilsp.SourceLSP, Detail: module, ImportModule: module}
	}
	m, _ := loaded(t, "Pa\n")
	m = insertModeAt(m, 0, 2)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 2, Seq: 1, Source: ilsp.SourceLSP, SourcePriority: ilsp.PriorityLSP, Items: []ilsp.CompletionItem{
		mk(0, "app.fs.paths"), mk(1, "pathlib2"), mk(2, "pathlib"), mk(3, "app/fs"),
	}})
	items := m.filteredCompletion()
	if got, want := rowsOf(items), "Path pathlib | Path   +3 modules"; got != want {
		t.Fatalf("rows = %q, want %q", got, want)
	}
	if got, want := rowsOf(items[1].Variants), "Path pathlib2 | Path app/fs | Path app.fs.paths"; got != want {
		t.Fatalf("variants = %q, want %q", got, want)
	}
}

// TestCompletionTwoImportVariantsShowBoth: a two-way group skips the folded
// entry and lists both, canonical first.
func TestCompletionTwoImportVariantsShowBoth(t *testing.T) {
	all := loggingVariants()
	m := openLogging(t, all[:2])
	items := m.filteredCompletion()
	if got, want := rowsOf(items), "logging logging Auto-import | logging app.util Auto-import"; got != want {
		t.Fatalf("rows = %q, want %q", got, want)
	}
	for _, it := range items {
		if len(it.Variants) != 0 {
			t.Fatalf("no folded entry for a two-way group, got %+v", it)
		}
	}
}

// TestCompletionDetailOnlyVariantsNotFolded: servers naming the module only
// in free-text detail keep the #2610 behaviour — every variant stays listed.
func TestCompletionDetailOnlyVariantsNotFolded(t *testing.T) {
	var items []ilsp.CompletionItem
	for i, d := range []string{"pathlib", "mypkg.fs", "other.mod"} {
		items = append(items, ilsp.CompletionItem{Label: "Path", InsertText: "Path", Detail: d, ID: i, Source: ilsp.SourceLSP})
	}
	m, _ := loaded(t, "Pa\n")
	m = insertModeAt(m, 0, 2)
	m, _ = m.Update(ilsp.CompletionMsg{Path: m.path, Line: 0, Col: 2, Seq: 1, Source: ilsp.SourceLSP, SourcePriority: ilsp.PriorityLSP, Items: items})
	if got, want := rowsOf(m.filteredCompletion()), "Path pathlib | Path mypkg.fs | Path other.mod"; got != want {
		t.Fatalf("rows = %q, want %q", got, want)
	}
}

// TestCompletionFoldedEntryRanksBehindCanonical: the folded entry shares the
// canonical item's label, so the tier ranking keeps it directly behind —
// never above — its canonical item, and an unrelated exact match still leads.
func TestCompletionFoldedEntryRanksBehindCanonical(t *testing.T) {
	items := loggingVariants()
	items = append(items, ilsp.CompletionItem{Label: "loggi", InsertText: "loggi", Kind: 6, ID: 9, Source: ilsp.SourceLSP, SortText: "zzz"})
	m := openLogging(t, items)
	if got, want := rowsOf(m.filteredCompletion()), "loggi | logging logging Auto-import | logging   +3 modules"; got != want {
		t.Fatalf("rows = %q, want %q", got, want)
	}
	// Selecting the folded entry requests no resolve — it inserts nothing.
	evs := resolveEvents(&m)
	m = send(m, special(tea.KeyDown))
	m = send(m, special(tea.KeyDown))
	if len(*evs) != 1 || (*evs)[0].CompletionID != 1 {
		t.Fatalf("events = %+v, want one select for the canonical item only", *evs)
	}
}

// TestCompletionPickerAppliesVariant: accepting the folded entry opens the
// picker; picking a variant applies its additional edits and inserts the
// symbol like a direct accept.
func TestCompletionPickerAppliesVariant(t *testing.T) {
	m := openLogging(t, loggingVariants())
	m = send(m, special(tea.KeyDown)) // the folded entry
	m = send(m, special(tea.KeyEnter))
	if m.comp == nil || m.comp.variants == nil {
		t.Fatal("accepting the folded entry must open the picker")
	}
	if got, want := rowsOf(m.filteredCompletion()), "logging app.cli Auto-import | logging app.util Auto-import | logging app.a.b Auto-import"; got != want {
		t.Fatalf("picker rows = %q, want %q", got, want)
	}
	if v := m.CompletionView(); !strings.Contains(v, completionPickHint) {
		t.Fatalf("picker must show its own hint, got:\n%s", v)
	}
	if got := line(m, 0); got != "loggi" {
		t.Fatalf("opening the picker inserted %q", got)
	}
	m = send(m, special(tea.KeyDown)) // app.util
	m = send(m, special(tea.KeyEnter))
	if m.comp != nil {
		t.Fatal("picking a variant closes the popup")
	}
	if got := line(m, 0); got != "import app.util" || line(m, 1) != "logging" {
		t.Fatalf("doc = %q, want the picked variant's import above the accepted symbol", m.buf.Lines())
	}
}

// TestCompletionPickerLateImport: a picked variant without inline edits
// resolves on accept like a direct accept (#2610): the accept event names
// the variant's ID and the late reply's import applies.
func TestCompletionPickerLateImport(t *testing.T) {
	items := loggingVariants()
	for i := range items {
		items[i].AdditionalEdits = nil
	}
	m := openLogging(t, items)
	evs := resolveEvents(&m)
	m = send(m, special(tea.KeyDown))
	m = send(m, special(tea.KeyEnter)) // picker opens, first variant (app.cli, ID 2) selected
	m = send(m, special(tea.KeyEnter))
	if got := line(m, 0); got != "logging" {
		t.Fatalf("line 0 = %q, want the accepted symbol", got)
	}
	last := (*evs)[len(*evs)-1]
	if last.Kind != EventCompletionAccept || last.CompletionID != 2 || last.CompletionSeq != 1 {
		t.Fatalf("events = %+v, want an accept for variant 2 of reply 1 last", *evs)
	}
	m, _ = m.Update(ilsp.CompletionResolveMsg{Path: m.path, ID: 2, Seq: 1, AdditionalEdits: []ilsp.FormatEdit{
		{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 0, Text: "from app.cli import logging\n"},
	}})
	if got := line(m, 0); got != "from app.cli import logging" || line(m, 1) != "logging" {
		t.Fatalf("doc = %q, want the late import of the picked variant", m.buf.Lines())
	}
}

// TestCompletionPickerEscReturnsToList: Esc inside the picker goes back to
// the list with the folded entry selected; a second Esc closes the popup.
func TestCompletionPickerEscReturnsToList(t *testing.T) {
	m := openLogging(t, loggingVariants())
	m = send(m, special(tea.KeyDown))
	m = send(m, special(tea.KeyEnter))
	m = send(m, special(tea.KeyEscape))
	if m.comp == nil || m.comp.variants != nil {
		t.Fatal("Esc must leave the picker and keep the list open")
	}
	if m.comp.sel != 1 {
		t.Fatalf("sel = %d, want the folded entry re-selected", m.comp.sel)
	}
	m = send(m, special(tea.KeyEscape))
	if m.comp != nil {
		t.Fatal("a second Esc closes the popup")
	}
}
