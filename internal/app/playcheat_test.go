package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/jqplay"
	"ike/internal/palette"
)

// playcheat_test.go covers the language cheatsheet's wiring (#2382): the key
// that opens it, that opening and closing it leaves the playground untouched,
// the dialect split, the searching, and the two shapes of insertion. The
// sheet's *content* is jqplay's — every example there is evaluated against a
// sample document by jqplay's own test.

// ctrlG is the cheatsheet chord, from either playground focus. It is fed
// through the mode's own routing (updatePlayground) rather than through
// Model.Update: the app coalesces key input above the modal dispatch, so a
// single synthetic keypress there never reaches the playground — the same
// reason the filter-picker tests drive their command message directly.
var ctrlG = tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl}

// TestPlayCheatsheetOpensAndLeavesThePlaygroundIntact is the issue's first
// acceptance case: one key from the open playground, and closing it again
// costs neither the query, the result nor the history.
func TestPlayCheatsheetOpensAndLeavesThePlaygroundIntact(t *testing.T) {
	m := openJQ(t, playApp(t, `{"foo":[{"bar":1},{"bar":4}]}`))
	m = setProgram(m, ".foo[] | select(.bar > 3)")
	m.play.hist.Add(".foo")
	wantOutputs := len(m.play.result.Outputs)

	tm, cmd := m.updatePlayground(ctrlG)
	m = drainCmd(asModel(tm), cmd)
	if !m.palette.IsOpen() {
		t.Fatal("ctrl+g must open the cheatsheet")
	}
	if !m.playOpen() {
		t.Fatal("the playground must stay mounted under the cheatsheet")
	}
	m.palette.Close()
	if got := m.play.program; got != ".foo[] | select(.bar > 3)" {
		t.Fatalf("the query line changed to %q", got)
	}
	if len(m.play.result.Outputs) != wantOutputs {
		t.Fatalf("the result changed: %d outputs, want %d", len(m.play.result.Outputs), wantOutputs)
	}
	if got, ok := m.play.hist.At(0); !ok || got != ".foo" {
		t.Fatalf("the history changed: %q (ok=%v)", got, ok)
	}
}

// TestPlayCheatsheetOpensFromTheResultBuffer: looking the language up must
// not require tabbing back to the query line first.
func TestPlayCheatsheetOpensFromTheResultBuffer(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	m.play.setBufFocus(true)
	tm, cmd := m.updatePlayground(ctrlG)
	m = drainCmd(asModel(tm), cmd)
	if !m.palette.IsOpen() {
		t.Fatal("ctrl+g must open the cheatsheet from the result buffer too")
	}
}

// cheatItems opens the sheet over a dialect and returns its rows for a query.
func cheatItems(m Model, d jqplay.Dialect, query string) []palette.Item {
	m.playCheat.dialect = d
	m.playCheat.Refresh()
	return m.playCheat.Results(query, palette.Context{})
}

// TestPlayCheatsheetListsEverySection: the sheet is the syntax, the everyday
// programs and the whole builtin list, generated from the engine's own.
func TestPlayCheatsheetListsEverySection(t *testing.T) {
	m := newSized()
	items := cheatItems(m, jqplay.DialectJQ, "")
	counts := map[string]int{}
	for _, it := range items {
		counts[it.Badge]++
	}
	for _, badge := range []string{"syntax", "example", "builtin"} {
		if counts[badge] == 0 {
			t.Errorf("no %s rows in the cheatsheet", badge)
		}
	}
	if counts["builtin"] != len(jqplay.Builtins()) {
		t.Errorf("builtin rows: got %d, want the engine's %d", counts["builtin"], len(jqplay.Builtins()))
	}
}

// TestPlayCheatsheetSearchesTitleAndProgram: the sheet is browsed by what one
// wants to do, and still found by the function name one half-remembers.
func TestPlayCheatsheetSearchesTitleAndProgram(t *testing.T) {
	m := newSized()
	byTitle := cheatItems(m, jqplay.DialectJQ, "group by a field")
	if len(byTitle) == 0 {
		t.Fatal("searching for the operation found nothing")
	}
	if !strings.Contains(byTitle[0].Title, "group") {
		t.Fatalf("the best row for a title query is %q", byTitle[0].Title)
	}
	// A program-only query still finds the example using it.
	found := false
	for _, it := range cheatItems(m, jqplay.DialectJQ, "from_entries") {
		if it.Badge == "example" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("searching for a function name found no example using it")
	}
}

// TestPlayCheatsheetFollowsTheDialect: a yq session is never shown the jq
// document rules, and the placeholder names the live dialect.
func TestPlayCheatsheetFollowsTheDialect(t *testing.T) {
	m := newSized()
	m.playCheat.dialect = jqplay.DialectYQ
	m.playCheat.Refresh()
	if !strings.HasPrefix(m.playCheat.Placeholder(), "yq ") {
		t.Fatalf("the yq placeholder is %q", m.playCheat.Placeholder())
	}
	for _, it := range m.playCheat.Results("", palette.Context{}) {
		if strings.Contains(it.Title, ".jsonl") {
			t.Fatalf("a jq-only row leaked into the yq sheet: %q", it.Title)
		}
	}
	m.playCheat.dialect = jqplay.DialectJQ
	m.playCheat.Refresh()
	for _, it := range m.playCheat.Results("", palette.Context{}) {
		if strings.Contains(it.Title, "merge key") {
			t.Fatalf("a yq-only row leaked into the jq sheet: %q", it.Title)
		}
	}
}

// TestPlayCheatsheetInsertReplacesAndKeepsTheOldProgram: an example is a
// whole program, so it replaces the query line — and the program it replaced
// is in the history, one `↑` away.
func TestPlayCheatsheetInsertReplacesAndKeepsTheOldProgram(t *testing.T) {
	m := openJQ(t, playApp(t, `{"users":[{"name":"ada","age":36}]}`))
	m = setProgram(m, ".users")

	tm, cmd := m.Update(InsertCheatMsg{Program: ".users | map(.name)"})
	m = drainCmd(asModel(tm), cmd)
	if m.play.program != ".users | map(.name)" {
		t.Fatalf("the query line is %q", m.play.program)
	}
	if m.play.pos != len(".users | map(.name)") {
		t.Fatalf("the caret is at %d", m.play.pos)
	}
	if got, ok := m.play.hist.At(0); !ok || got != ".users" {
		t.Fatalf("the replaced program is not in the history: %q (ok=%v)", got, ok)
	}
	if m.play.status == "" {
		t.Fatal("the insert must say the example uses the sheet's own document")
	}
	if len(m.play.result.Outputs) == 0 {
		t.Fatal("the inserted program must have been run")
	}
}

// TestPlayCheatsheetInsertsABuiltinAtTheCaret: a function name is half a
// program, so it lands where the caret is instead of replacing the line.
func TestPlayCheatsheetInsertsABuiltinAtTheCaret(t *testing.T) {
	m := openJQ(t, playApp(t, `{"users":[{"name":"ada"}]}`))
	m = setProgram(m, ".users | ")
	m.play.pos = len(".users | ")

	tm, cmd := m.Update(InsertCheatMsg{Program: "length", AtCaret: true})
	m = drainCmd(asModel(tm), cmd)
	if m.play.program != ".users | length" {
		t.Fatalf("the query line is %q", m.play.program)
	}
	if m.play.pos != len(".users | length") {
		t.Fatalf("the caret is at %d", m.play.pos)
	}
}

// TestPlayCheatsheetInsertNeedsThePlayground: the sheet is readable from the
// palette with nothing open, but it never opens a playground over an
// unrelated buffer just to run an example written against another document.
func TestPlayCheatsheetInsertNeedsThePlayground(t *testing.T) {
	m := newSized()
	tm, cmd := m.Update(InsertCheatMsg{Program: ".users[]"})
	m = drainCmd(asModel(tm), cmd)
	if m.playOpen() {
		t.Fatal("inserting a cheatsheet row must not open a playground")
	}
}

// TestPlayCheatsheetInsertRefusesTheOtherDialect: a yq sheet's row has no
// business rewriting an open jq playground's query line.
func TestPlayCheatsheetInsertRefusesTheOtherDialect(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	m = setProgram(m, ".a")
	tm, cmd := m.Update(InsertCheatMsg{Dialect: jqplay.DialectYQ, Program: ".users[]"})
	m = drainCmd(asModel(tm), cmd)
	if m.play.program != ".a" {
		t.Fatalf("the jq query line was rewritten to %q", m.play.program)
	}
}

// TestPlayCheatsheetIsInTheKeyboardSheet: the language sheet has to be
// findable from the keyboard one, or nobody learns it exists (#2237).
func TestPlayCheatsheetIsInTheKeyboardSheet(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	var found int
	for _, g := range m.playgroundHelpGroups() {
		for _, e := range g.Entries {
			if e.Shortcut == "ctrl+g" && strings.Contains(e.Title, "cheatsheet") {
				found++
			}
		}
	}
	if found < 2 {
		t.Fatalf("ctrl+g is documented in %d of the two key tables", found)
	}
}
