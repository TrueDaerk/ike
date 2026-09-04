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

// ctrlG is the cheatsheet chord, from either playground focus. Most cases feed
// it through the mode's own routing (updatePlayground) rather than through
// Model.Update, because a fresh test model still has the first-start LSP
// dialog (#301) up and that dialog owns the keyboard — the same reason the
// filter-picker tests drive their command message directly. #2482 added one
// case that does take the whole Model.Update path, with the dialog dismissed
// first: the report was that the chord does nothing on the query line, and a
// test that never leaves the mode's own switch cannot see the difference.
var ctrlG = tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl}

// TestPlayCheatsheetOpensThroughTheWholeKeyPath drives ctrl+g through
// Model.Update, the way the terminal delivers it: past the overlay chain, the
// modal dispatch and the playground's own routing.
func TestPlayCheatsheetOpensThroughTheWholeKeyPath(t *testing.T) {
	m := openJQ(t, playApp(t, `{"users":[{"name":"ada"}]}`))
	m = setProgram(m, ".users")
	m.onboarding = nil // the first-start dialog would own the keyboard
	if !m.playFocused() {
		t.Fatal("the playground must hold the focus for this test to mean anything")
	}
	tm, cmd := m.Update(ctrlG)
	m = drainCmd(asModel(tm), cmd)
	if !m.palette.IsOpen() {
		t.Fatal("ctrl+g must reach the playground and open the cheatsheet")
	}
	m.palette.Close()
	if m.play.program != ".users" {
		t.Fatalf("the query line is %q", m.play.program)
	}
}

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

// TestPlayCheatsheetOpensWhileWritingAQuery is #2482's first acceptance case:
// with the completion popup up — the state the query line is in for most of
// the time a query is being written — ctrl+g still opens the sheet, the popup
// is gone from under it, and the half-typed program and its caret survive the
// trip.
func TestPlayCheatsheetOpensWhileWritingAQuery(t *testing.T) {
	m := openJQ(t, playApp(t, `{"users":[{"name":"ada","age":36}]}`))
	m = setProgram(m, ".us")
	m.refreshPlayCompletion("", true)
	if m.play.comp == nil {
		t.Fatal("the completion popup must be open for this test to mean anything")
	}
	pos := m.play.pos

	tm, cmd := m.updatePlayground(ctrlG)
	m = drainCmd(asModel(tm), cmd)
	if !m.palette.IsOpen() {
		t.Fatal("ctrl+g must open the cheatsheet with the completion popup open")
	}
	if m.play.comp != nil {
		t.Fatal("the completion popup must not stay drawn on top of the sheet")
	}
	m.palette.Close()
	if m.play.program != ".us" || m.play.pos != pos {
		t.Fatalf("the query line is %q at %d, want %q at %d", m.play.program, m.play.pos, ".us", pos)
	}
}

// TestPlayCheatsheetOpensFromThePaletteWhileFocused: the palette and Tools
// menu entry (json.jqCheatsheet) has to work from the query line too — it is
// the delivered fallback for a terminal that eats ctrl+g, and it was worth
// nothing if the playground's modal routing swallowed the command message.
func TestPlayCheatsheetOpensFromThePaletteWhileFocused(t *testing.T) {
	m := openJQ(t, playApp(t, `{"users":[{"name":"ada"}]}`))
	m = setProgram(m, ".users | ")
	m.play.pos = len(".users | ")
	if !m.playFocused() {
		t.Fatal("the playground must hold the focus for this test to mean anything")
	}

	tm, cmd := m.Update(ShowCheatsheetMsg{})
	m = drainCmd(asModel(tm), cmd)
	if !m.palette.IsOpen() {
		t.Fatal("json.jqCheatsheet must open the sheet while the playground is focused")
	}
	if !m.playOpen() {
		t.Fatal("the playground must stay mounted under the sheet")
	}
	m.palette.Close()
	if m.play.program != ".users | " || m.play.pos != len(".users | ") {
		t.Fatalf("the query line is %q at %d", m.play.program, m.play.pos)
	}
}

// TestPlayCheatsheetHintIsOnTheQueryRow: the chord is only usable if it is
// said somewhere. It was listed seventh in the hint tail, which the info row
// cuts at the widths a playground is actually opened at — so the one key that
// answers "I cannot write this query" was never on screen.
func TestPlayCheatsheetHintIsOnTheQueryRow(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	// 100 cells is a playground pane beside an explorer on a normal terminal
	// — the width the report came from, and the width the hint used to be cut
	// at. Narrower than that the row is down to its first two hints and no
	// ordering saves anything.
	for _, width := range []int{100, 120, 200} {
		if row := m.playInfoRow(width); !strings.Contains(row, "ctrl+g cheatsheet") {
			t.Errorf("the info row at width %d does not name the cheatsheet: %q", width, row)
		}
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

// TestPlayCheatsheetSaysHowToApplyARow is #2482's second acceptance case: the
// sheet's first row states what enter does with the row under it, so nobody
// has to guess whether a builtin replaces the program or lands at the caret.
func TestPlayCheatsheetSaysHowToApplyARow(t *testing.T) {
	m := newSized()
	items := cheatItems(m, jqplay.DialectJQ, "")
	if len(items) < 2 || items[0].Badge != "guide" {
		t.Fatalf("the sheet does not lead with a guide row (%d rows)", len(items))
	}
	var head []string
	for _, it := range items {
		if it.Badge != "guide" {
			break
		}
		head = append(head, it.Title)
		// A guide row carries no language, so enter must not write into the
		// query line — it re-opens the sheet instead.
		if _, ok := it.Msg.(InsertCheatMsg); ok {
			t.Errorf("guide row %q would insert itself into the query line", it.Title)
		}
	}
	joined := strings.Join(head, "\n")
	for _, want := range []string{"⏎", "replaces the program; ↑ restores it", "inserts the name at the caret", "esc"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the guide rows do not mention %q:\n%s", want, joined)
		}
	}
	// Each guide row has to survive the width a palette row actually has —
	// the sentence about builtins used to sit past the truncation point.
	for _, line := range head {
		if n := len([]rune(line)); n > 65 {
			t.Errorf("guide row is %d cells wide, too wide to read: %q", n, line)
		}
	}
}

// TestPlayCheatsheetShowsTheSampleDocument: the examples are written against a
// document the reader never saw, which made `.users[]` a field out of nowhere.
// The second row lists it, and its rows are the document itself.
func TestPlayCheatsheetShowsTheSampleDocument(t *testing.T) {
	for _, d := range []jqplay.Dialect{jqplay.DialectJQ, jqplay.DialectYQ, jqplay.DialectXMQ} {
		m := newSized()
		items := cheatItems(m, d, "")
		var found bool
		for _, it := range items {
			if it.Badge != "guide" {
				break
			}
			if !strings.HasPrefix(it.Title, jqplay.CheatSampleTag) {
				continue
			}
			show, ok := it.Msg.(ShowCheatsheetMsg)
			if !ok || show.Query != jqplay.CheatSampleTag {
				t.Fatalf("%s: the sample row emits %#v, want the seeded re-open", d.Name(), it.Msg)
			}
			found = true
		}
		if !found {
			t.Fatalf("%s: no %q guide row", d.Name(), jqplay.CheatSampleTag)
		}
		// The seeded query is what actually lists the document.
		seeded := cheatItems(m, d, jqplay.CheatSampleTag)
		if len(seeded) == 0 {
			t.Fatalf("%s: %q lists nothing", d.Name(), jqplay.CheatSampleTag)
		}
		var body []string
		for _, it := range seeded {
			if it.Badge == "sample" {
				body = append(body, it.Title)
			}
		}
		if len(body) == 0 {
			t.Fatalf("%s: no sample rows under the seeded query", d.Name())
		}
		joined := strings.Join(body, "\n")
		for _, line := range strings.Split(jqplay.Sample(d), "\n") {
			if line = strings.TrimRight(line, " \t"); line == "" {
				continue
			}
			if !strings.Contains(joined, line) {
				t.Errorf("%s: sample line %q is not listed", d.Name(), line)
			}
		}
	}
}

// TestPlayCheatsheetSampleRowsSitAtTheEnd: the document is reachable, but it
// does not stand between the reader and the language on a cold open — a yq
// sample is twenty rows of pure noise there.
func TestPlayCheatsheetSampleRowsSitAtTheEnd(t *testing.T) {
	m := newSized()
	items := cheatItems(m, jqplay.DialectYQ, "")
	var firstSample, lastOther int
	firstSample = -1
	for i, it := range items {
		switch it.Badge {
		case "sample":
			if firstSample < 0 {
				firstSample = i
			}
		case "guide":
		default:
			lastOther = i
		}
	}
	if firstSample < 0 {
		t.Fatal("no sample rows at all")
	}
	if firstSample < lastOther {
		t.Errorf("sample rows start at %d, before the last language row at %d", firstSample, lastOther)
	}
}

// TestPlayCheatsheetShowsExampleOutputAndBuiltinUsage: an example row says
// what it prints, a builtin row the shape it is called in — the whole point of
// #2482's content half is that the sheet stops listing existence alone.
func TestPlayCheatsheetShowsExampleOutputAndBuiltinUsage(t *testing.T) {
	m := newSized()
	var example, builtin palette.Item
	for _, it := range cheatItems(m, jqplay.DialectJQ, "map one field out of every element") {
		if it.Badge == "example" {
			example = it
			break
		}
	}
	if example.Title == "" {
		t.Fatal("the map-a-field example is not findable")
	}
	if !strings.Contains(example.Detail, "→") || !strings.Contains(example.Detail, "ada") {
		t.Errorf("the example's chip is %q, want the program and what it prints", example.Detail)
	}
	for _, it := range cheatItems(m, jqplay.DialectJQ, "select") {
		if it.Badge == "builtin" && strings.HasPrefix(it.Title, "select ") {
			builtin = it
			break
		}
	}
	if builtin.Title == "" {
		t.Fatal("the select builtin is not findable")
	}
	if !strings.Contains(builtin.Detail, "select(cond)") || !strings.Contains(builtin.Detail, "/1") {
		t.Errorf("the builtin's chip is %q, want the usage form beside the arity", builtin.Detail)
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
