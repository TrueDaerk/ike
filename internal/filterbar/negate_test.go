package filterbar

import (
	"strings"
	"testing"
)

// The bar's view of negation (#2951): a negated term parses, completes like
// its positive spelling, is not reported by HasTerm, and the hint names "-".
func TestNegatedTerms(t *testing.T) {
	m := New(demo)
	m.Focus()
	typeIn(&m, "-sco")
	if got := m.Completion(); got != "pe:" {
		t.Fatalf("negated name ghost = %q, want pe:", got)
	}
	m.SetText("-scope:file crash")
	if m.Err() != "" {
		t.Fatalf("negated term must parse: %s", m.Err())
	}
	if m.HasTerm("scope", "file") {
		t.Fatal("HasTerm must not report a negated term")
	}
	if q := m.Query(); len(q.Terms) != 1 || !q.Terms[0].Negated {
		t.Fatalf("query = %+v, want one negated term", q)
	}
	// The quick-key seam replaces the field, negated term included.
	m.SetTerm("scope", "file")
	if m.Text() != "scope:file crash" {
		t.Fatalf("SetTerm text = %q", m.Text())
	}
	if !strings.Contains(m.Hint(), "- negates") {
		t.Fatalf("hint %q must mention negation", m.Hint())
	}
}
