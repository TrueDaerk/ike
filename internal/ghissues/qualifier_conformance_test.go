package ghissues

// qualifier_conformance_test.go pins the two readers of the pane's filter
// syntax to each other (#2115): the live match input's extractQualifiers
// (#2110) and internal/issuefilter, which the config validator and the
// settings form use for issues.default_filter and issues.saved_filters. They
// are separate implementations — the config layer cannot import the pane —
// so a change to one that the other does not follow has to fail here rather
// than in a user's config file.

import (
	"reflect"
	"sort"
	"testing"

	"ike/internal/issuefilter"
)

// The vocabularies must be the same set of values, or an expression typed in
// the overlay would be rejected by the settings form (or vice versa).
func TestQualifierVocabulariesMatch(t *testing.T) {
	gotStates := append([]string(nil), stateValues...)
	wantStates := append([]string(nil), issuefilter.States...)
	sort.Strings(gotStates)
	sort.Strings(wantStates)
	if !reflect.DeepEqual(gotStates, wantStates) {
		t.Fatalf("state values = %v, issuefilter.States = %v", gotStates, wantStates)
	}
	gotSorts := sortValues() // already sorted
	wantSorts := append([]string(nil), issuefilter.SortOrders...)
	sort.Strings(wantSorts)
	if !reflect.DeepEqual(gotSorts, wantSorts) {
		t.Fatalf("sort values = %v, issuefilter.SortOrders = %v", gotSorts, wantSorts)
	}
}

// Every expression issuefilter accepts must be read the same way by the live
// match input, so a default filter and a hand-typed one produce one filter.
func TestBothParsersAgree(t *testing.T) {
	for _, expr := range []string{
		"",
		"is:open",
		"state:closed",
		"is:all label:bug",
		"label:bug label:triage",
		`label:"good first issue"`,
		"sort:oldest",
		"is:all label:bug sort:number crash",
		"crash",
		"panic crash",
		"label:bug crash panic",
	} {
		spec, err := issuefilter.Parse(expr)
		if err != nil {
			t.Fatalf("issuefilter rejected %q: %v", expr, err)
		}
		// final: a whole config value has no half-typed trailing token.
		rest, quals := extractQualifiers(expr, true)
		got := issuefilter.Spec{Match: rest}
		for _, q := range quals {
			switch q.name {
			case "state":
				got.State = q.value
			case "label":
				if !contains(got.Labels, q.value) {
					got.Labels = append(got.Labels, q.value)
				}
			case "sort":
				got.Sort = q.value
			}
		}
		if !reflect.DeepEqual(got, spec) {
			t.Fatalf("%q: match input read %+v, issuefilter read %+v", expr, got, spec)
		}
	}
}

// contains is the test's own membership check over a small slice.
func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
