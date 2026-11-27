package config

import (
	"reflect"
	"strings"
	"testing"
)

// search_matchorder_test.go covers search.match_order: the ranking order of
// the match classes is a defaulted list key holding every class exactly once.

var defaultMatchOrder = []string{"exact", "prefix", "contains", "initials", "directory", "fuzzy"}

func TestSearchMatchOrderDefault(t *testing.T) {
	c, diags := Load(Options{})
	if !reflect.DeepEqual(c.Search.MatchOrder, defaultMatchOrder) {
		t.Errorf("match_order default = %v, want %v", c.Search.MatchOrder, defaultMatchOrder)
	}
	if len(diags) != 0 {
		t.Errorf("the default must validate, got %v", diags)
	}
	if v := c.Flat()["search.match_order"]; v != strings.Join(defaultMatchOrder, ",") {
		t.Errorf("Flat search.match_order = %q", v)
	}
}

func TestSearchMatchOrderAcceptsPermutation(t *testing.T) {
	proj := writeProject(t, "[search]\nmatch_order = [\"directory\", \"exact\", \"initials\", \"prefix\", \"contains\", \"fuzzy\"]\n")
	c, diags := Load(Options{ProjectRoot: proj})
	want := []string{"directory", "exact", "initials", "prefix", "contains", "fuzzy"}
	if !reflect.DeepEqual(c.Search.MatchOrder, want) || len(diags) != 0 {
		t.Errorf("a permutation must load unchanged, got %v %v", c.Search.MatchOrder, diags)
	}
}

func TestSearchMatchOrderRejectsBadLists(t *testing.T) {
	cases := []struct {
		name, list, message string
	}{
		{"unknown name", `["exact", "prefix", "contains", "initials", "directory", "scattered"]`, `unknown match class "scattered"`},
		{"duplicate", `["exact", "prefix", "prefix", "initials", "directory", "fuzzy"]`, `match class "prefix" is listed twice`},
		{"missing", `["exact", "prefix", "contains"]`, "missing match class initials, directory, fuzzy"},
		{"empty", `[]`, "missing match class"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proj := writeProject(t, "[search]\nmatch_order = "+tc.list+"\n")
			c, diags := Load(Options{ProjectRoot: proj})
			if !reflect.DeepEqual(c.Search.MatchOrder, defaultMatchOrder) {
				t.Errorf("a rejected list must fall back to the default, got %v", c.Search.MatchOrder)
			}
			if len(diags) != 1 || diags[0].Field != "search.match_order" {
				t.Fatalf("expected one diagnostic on the key, got %v", diags)
			}
			if !strings.Contains(diags[0].Message, tc.message) {
				t.Errorf("diagnostic %q does not say %q", diags[0].Message, tc.message)
			}
		})
	}
}
