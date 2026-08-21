package palette

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// digitMode is a DigitPicker stub: it lists its titles filtered by substring
// so a typed query really narrows the list, like the intention popup's fuzzy
// match does.
type digitMode struct {
	prefix  rune
	titles  []string
	enabled bool
}

func (d digitMode) Prefix() rune        { return d.prefix }
func (d digitMode) Placeholder() string { return "" }
func (d digitMode) DigitShortcuts() bool {
	return d.enabled
}

func (d digitMode) Results(query string, _ Context) []Item {
	var out []Item
	for _, t := range d.titles {
		if query != "" && !strings.Contains(strings.ToLower(t), strings.ToLower(query)) {
			continue
		}
		out = append(out, Item{Title: t, Msg: RunCommandMsg{ID: t}})
	}
	return out
}

func digitKey(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

// ranCommand runs cmd and reports the RunCommandMsg id it produced.
func ranCommand(t *testing.T, cmd tea.Cmd) string {
	t.Helper()
	if cmd == nil {
		return ""
	}
	rc, ok := cmd().(RunCommandMsg)
	if !ok {
		t.Fatalf("cmd emitted %#v, want RunCommandMsg", cmd())
	}
	return rc.ID
}

// TestDigitPickerRunsRowOnEmptyQuery guards the #2023 fast path: with an empty
// query a digit activates the matching row and closes the palette.
func TestDigitPickerRunsRowOnEmptyQuery(t *testing.T) {
	mode := digitMode{prefix: '!', titles: []string{"Alpha", "Beta", "Gamma"}, enabled: true}
	p := New(Config{}, mode)
	p.SetSize(80, 24)
	p.OpenLocked(Context{}, '!')

	cmd := p.Update(digitKey('2'))
	if got := ranCommand(t, cmd); got != "Beta" {
		t.Fatalf("digit 2 ran %q, want Beta", got)
	}
	if p.IsOpen() {
		t.Fatal("running a row must close the palette")
	}
	if p.Query() != "" {
		t.Fatalf("the digit must not land in the query, got %q", p.Query())
	}
}

// TestDigitPickerPastEndSwallowed: a digit with no matching row keeps the
// popup open and does not type into the query — the hints and the accepted
// keys stay in sync.
func TestDigitPickerPastEndSwallowed(t *testing.T) {
	p := New(Config{}, digitMode{prefix: '!', titles: []string{"Alpha"}, enabled: true})
	p.SetSize(80, 24)
	p.OpenLocked(Context{}, '!')

	if cmd := p.Update(digitKey('5')); cmd != nil {
		t.Fatal("a digit past the last row must not activate anything")
	}
	if !p.IsOpen() || p.Query() != "" {
		t.Fatalf("open = %v, query = %q", p.IsOpen(), p.Query())
	}
}

// TestDigitPickerFiltersOnceQueryTyped: with a query present the digits are
// ordinary text again, so "Item 2"-style titles stay reachable by typing.
func TestDigitPickerFiltersOnceQueryTyped(t *testing.T) {
	p := New(Config{}, digitMode{prefix: '!', titles: []string{"Run test 1", "Run test 2"}, enabled: true})
	p.SetSize(80, 24)
	p.OpenLocked(Context{}, '!')

	p.Update(runes("test "))
	if cmd := p.Update(digitKey('2')); cmd != nil {
		t.Fatal("with a query typed, a digit must filter, not run")
	}
	if p.Query() != "test 2" {
		t.Fatalf("query = %q, want %q", p.Query(), "test 2")
	}
	if len(p.items) != 1 || p.items[0].Title != "Run test 2" {
		t.Fatalf("digit did not narrow the list: %+v", p.items)
	}
	if got := ranCommand(t, p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})); got != "Run test 2" {
		t.Fatalf("enter ran %q", got)
	}
}

// TestDigitPickerScopedToOptingModes: a mode that does not implement
// DigitPicker (or opts out) keeps the generic palette behavior — digits type.
func TestDigitPickerScopedToOptingModes(t *testing.T) {
	for name, mode := range map[string]Mode{
		"plain":   stubMode{prefix: '!', items: []Item{{Title: "Alpha", Msg: RunCommandMsg{ID: "Alpha"}}}},
		"opt-out": digitMode{prefix: '!', titles: []string{"Alpha"}, enabled: false},
	} {
		p := New(Config{}, mode)
		p.SetSize(80, 24)
		p.OpenLocked(Context{}, '!')
		if cmd := p.Update(digitKey('1')); cmd != nil {
			t.Fatalf("%s: digit activated a row", name)
		}
		if p.Query() != "1" {
			t.Fatalf("%s: query = %q, want the typed digit", name, p.Query())
		}
	}
}

// TestDigitHintRendered: an Item.Hint shows in front of the row title.
func TestDigitHintRendered(t *testing.T) {
	p := New(Config{}, stubMode{prefix: '!'})
	p.SetSize(80, 24)
	row := p.row(Item{Title: "Organize Imports", Hint: "3", Detail: "quick fix"}, false, true, 40)
	plain := ansi.Strip(row)
	if !strings.Contains(plain, "3 Organize Imports") {
		t.Fatalf("hint missing from row %q", plain)
	}
}
