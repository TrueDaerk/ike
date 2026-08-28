// Package issuefilter is the Issues pane's dialect of the shared filter
// syntax (internal/filterexpr): the expression the pane's default filter and
// its named saved filters are written in (#2115).
//
// The syntax is the one the filter overlay's match input already accepts
// (#2110, internal/ghissues/qualifier.go), so an expression can be typed in
// the pane and pasted into the config unchanged:
//
//	is:open label:bug label:"good first issue" sort:oldest crash
//
// `is:` (alias `state:`) takes open, closed or all; `label:` is repeated once
// per label and OR'd; `sort:` names a list order; everything else is the
// fuzzy match pattern, so a bare word means "match this". A value with spaces
// is double-quoted.
//
// Since #2156 the tokenizer, the quoting rules and the error wording live in
// internal/filterexpr, shared with the Problems, Usages and TODO index panes;
// this package is the Issues Schema over it plus the Spec shape its readers
// want. The one deliberate difference from the live input is strictness. In
// the overlay an unknown qualifier stays literal fuzzy text — you are
// mid-typing and can see it — while a config value has no reader to correct
// it, so here it is an error the settings form and the config diagnostics can
// name.
//
// It is a leaf package on purpose: the config validator, the settings form
// and the pane all read the same expression, so none of them owns the syntax.
// ghissues/qualifier_conformance_test.go pins the two parsers to each other.
package issuefilter

import (
	"fmt"
	"strings"

	"ike/internal/filterexpr"
)

// Spec is one parsed expression. Every field is optional — an empty Spec
// narrows nothing, and a consumer seeding itself from it leaves the
// dimensions the expression did not name alone.
type Spec struct {
	// State is the state gate: "open", "closed", "all", or "" when the
	// expression did not name one.
	State string
	// Labels are the selected label names, in the order they were written.
	Labels []string
	// Sort is the list order, "" when the expression did not name one.
	Sort string
	// Match is the fuzzy pattern.
	Match string
}

// Empty reports whether the expression narrows nothing.
func (s Spec) Empty() bool {
	return s.State == "" && len(s.Labels) == 0 && s.Sort == "" && s.Match == ""
}

// States is the state qualifier's vocabulary, in the order error messages
// list it. Exported so the pane's conformance test can walk it.
var States = []string{"open", "closed", "all"}

// SortOrders is the sort qualifier's vocabulary. It mirrors the pane's own
// list; the conformance test fails if the two ever drift.
var SortOrders = []string{"relevance", "newest", "oldest", "updated", "number"}

// Schema is the Issues pane's filter language, the single definition every
// reader of the syntax resolves a qualifier against (#2156).
var Schema = filterexpr.Schema{Fields: []filterexpr.Field{
	{Name: "is", Aliases: []string{"state"}, Values: States, Doc: "state gate: open, closed or all"},
	{Name: "label", ValueDoc: "a label name", Doc: "issue label, repeatable (OR)"},
	{Name: "sort", Values: SortOrders, Doc: "list order"},
}}

// Parse reads one filter expression. It rejects an unknown qualifier, an
// unknown value and an unterminated quote with a message naming what is
// valid — the settings form shows it verbatim, so it has to read as advice.
func Parse(expr string) (Spec, error) {
	q, err := Schema.Parse(expr)
	if err != nil {
		return Spec{}, err
	}
	return Spec{
		State:  q.Value("is"),
		Labels: q.Values("label"),
		Sort:   q.Value("sort"),
		Match:  q.Match,
	}, nil
}

// ParseSaved reads one "name=expression" saved-filter entry (the
// issues.saved_filters list). The name is everything before the first "=";
// it may not be empty, and the expression must parse and narrow something.
func ParseSaved(entry string) (string, Spec, error) {
	name, expr, ok := strings.Cut(entry, "=")
	name = strings.TrimSpace(name)
	if !ok {
		return "", Spec{}, fmt.Errorf("missing \"=\": write a saved filter as name=is:open label:bug")
	}
	if name == "" {
		return "", Spec{}, fmt.Errorf("missing name: write a saved filter as name=is:open label:bug")
	}
	spec, err := Parse(expr)
	if err != nil {
		return "", Spec{}, err
	}
	if spec.Empty() {
		return "", Spec{}, fmt.Errorf("saved filter %q narrows nothing, name at least one of is:, label:, sort: or some match text", name)
	}
	return name, spec, nil
}

// Format writes a Spec back as an expression. It is the inverse of Parse for
// every expression Parse accepts, up to qualifier order and quoting.
func Format(s Spec) string {
	var q filterexpr.Query
	if s.State != "" {
		q.Terms = append(q.Terms, filterexpr.Term{Field: "is", Value: s.State})
	}
	for _, l := range s.Labels {
		q.Terms = append(q.Terms, filterexpr.Term{Field: "label", Value: l})
	}
	if s.Sort != "" {
		q.Terms = append(q.Terms, filterexpr.Term{Field: "sort", Value: s.Sort})
	}
	q.Match = s.Match
	return filterexpr.Format(q)
}
