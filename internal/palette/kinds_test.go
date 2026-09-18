package palette

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// kinds_test.go covers the kind summary the pick and dismissal records carry
// (#2635): a closed, sorted, counted vocabulary — never a row's title.

// kindedPalette lists three rows carrying kinds plus one chrome row, so the
// summary provably skips what the palette renders but never activates.
func kindedPalette(t *testing.T) *Palette {
	t.Helper()
	p := New(Config{DefaultPrefix: '&'}, stubMode{prefix: '&', items: []Item{
		{Title: "one", Kind: "quickfix", Msg: primaryMsg{"one"}},
		{Title: "two", Kind: "source.organizeImports", Msg: primaryMsg{"two"}},
		{Title: "three", Kind: "quickfix", Msg: primaryMsg{"three"}},
		{Title: "did you mean", Inert: true},
		{Title: "four", Msg: primaryMsg{"four"}},
	}})
	p.SetSize(100, 40)
	p.OpenLocked(Context{}, '&')
	return p
}

// The summary is sorted, counts repeats and skips chrome and kindless rows.
func TestKindSummarySortsAndCounts(t *testing.T) {
	p := kindedPalette(t)
	if got, want := p.kindSummary(), "quickfix*2,source.organizeImports"; got != want {
		t.Fatalf("kindSummary = %q, want %q", got, want)
	}
}

// Both records carry it: the dismissal the offer, the pick the offer plus the
// chosen row's own kind.
func TestPickAndDismissCarryKinds(t *testing.T) {
	p := kindedPalette(t)
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	pick, ok := p.TakePick()
	if !ok {
		t.Fatal("an activation must leave a pick to take")
	}
	if pick.Kinds != "quickfix*2,source.organizeImports" || pick.PickedKind != "source.organizeImports" {
		t.Fatalf("pick = %+v, want the offer summary and the second row's kind", pick)
	}

	p = kindedPalette(t)
	p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	d, ok := p.TakeDismissal()
	if !ok {
		t.Fatal("an esc must leave a dismissal to take")
	}
	if d.Kinds != "quickfix*2,source.organizeImports" {
		t.Fatalf("dismissal = %+v, want the offer summary", d)
	}
}

// A mode whose rows carry no Kind summarizes to "", which is what keeps the
// field out of every other mode's events.
func TestKindlessModeSummarizesEmpty(t *testing.T) {
	p := pickPalette(t)
	if got := p.kindSummary(); got != "" {
		t.Fatalf("kindSummary = %q, want empty for a mode without kinds", got)
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if pick, _ := p.TakePick(); pick.Kinds != "" || pick.PickedKind != "" {
		t.Fatalf("pick = %+v, want no kinds at all", pick)
	}
}

// sanitizeKind is the leak guard: only identifier characters within the length
// cap travel; anything else collapses to "other".
func TestSanitizeKind(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"quickfix", "quickfix"},
		{"source.organizeImports", "source.organizeImports"},
		{"refactor.rewrite-all_now", "refactor.rewrite-all_now"},
		{"fix userSecret in a.go", "other"},
		{"quickfix: rename Foo", "other"},
		{strings.Repeat("x", kindTokenMax), strings.Repeat("x", kindTokenMax)},
		{strings.Repeat("x", kindTokenMax+1), "other"},
	} {
		if got := sanitizeKind(tc.in); got != tc.want {
			t.Errorf("sanitizeKind(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A pathological offer cannot blow up an event line: the summary names at most
// kindSummaryMax kinds and marks the truncation.
func TestKindSummaryTruncates(t *testing.T) {
	var items []Item
	for i := 0; i < kindSummaryMax+5; i++ {
		items = append(items, Item{Title: "row", Kind: "kind" + string(rune('a'+i)), Msg: primaryMsg{"row"}})
	}
	p := New(Config{DefaultPrefix: '&'}, stubMode{prefix: '&', items: items})
	p.SetSize(100, 40)
	p.OpenLocked(Context{}, '&')

	got := p.kindSummary()
	parts := strings.Split(got, ",")
	if len(parts) != kindSummaryMax+1 || parts[len(parts)-1] != "…" {
		t.Fatalf("kindSummary = %q, want %d kinds plus the … marker", got, kindSummaryMax)
	}
}
