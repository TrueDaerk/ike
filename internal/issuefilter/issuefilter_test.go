package issuefilter

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseQualifiers(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want Spec
	}{
		{"empty", "", Spec{}},
		{"blank", "   ", Spec{}},
		{"is", "is:closed", Spec{State: "closed"}},
		{"state is an alias of is", "state:closed", Spec{State: "closed"}},
		{"sort", "sort:oldest", Spec{Sort: "oldest"}},
		{"labels are repeated, not comma separated", "label:bug label:triage",
			Spec{Labels: []string{"bug", "triage"}}},
		{"a duplicate label is kept once", "label:bug label:bug", Spec{Labels: []string{"bug"}}},
		{"quoted label value", `label:"good first issue"`, Spec{Labels: []string{"good first issue"}}},
		{"every dimension", "is:all label:bug sort:number crash",
			Spec{State: "all", Labels: []string{"bug"}, Sort: "number", Match: "crash"}},
		{"a bare word is match text", "crash", Spec{Match: "crash"}},
		{"bare words join", "panic crash", Spec{Match: "panic crash"}},
		{"a quoted word is never a qualifier", `"fix:tests"`, Spec{Match: "fix:tests"}},
		{"an upper-case key still reads as its qualifier", "Label:bug", Spec{Labels: []string{"bug"}}},
		{"the last state wins", "is:open is:closed", Spec{State: "closed"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.expr)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.expr, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Parse(%q) = %+v, want %+v", tt.expr, got, tt.want)
			}
		})
	}
}

// A config value has no reader to correct it, so — unlike the live match
// input, which leaves an unknown token as literal fuzzy text — a broken
// qualifier here is an error naming what is valid.
func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want string // a substring the message must name
	}{
		{"unknown qualifier", "author:me", `unknown qualifier "author:"`},
		{"unknown state", "is:merged", "is: wants open, closed, all"},
		{"unknown sort", "sort:size", "sort: wants"},
		{"empty label", "label:", "label: wants a label name"},
		{"unterminated quote", `label:"bug`, "unterminated quote"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.expr)
			if err == nil {
				t.Fatalf("Parse(%q) accepted an invalid expression", tt.expr)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Parse(%q) error = %q, want it to name %q", tt.expr, err, tt.want)
			}
		})
	}
}

func TestParseSaved(t *testing.T) {
	name, spec, err := ParseSaved("triage=is:open label:bug crash")
	if err != nil {
		t.Fatalf("ParseSaved: %v", err)
	}
	if name != "triage" {
		t.Fatalf("name = %q, want triage", name)
	}
	want := Spec{State: "open", Labels: []string{"bug"}, Match: "crash"}
	if !reflect.DeepEqual(spec, want) {
		t.Fatalf("spec = %+v, want %+v", spec, want)
	}
}

func TestParseSavedErrors(t *testing.T) {
	for _, tt := range []struct{ name, entry, want string }{
		{"no equals", "triage", `missing "="`},
		{"no name", "=is:open", "missing name"},
		{"empty expression", "triage=", "narrows nothing"},
		{"bad expression", "triage=is:merged", "is: wants"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := ParseSaved(tt.entry); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseSaved(%q) error = %v, want it to name %q", tt.entry, err, tt.want)
			}
		})
	}
}

// Format has to produce something Parse reads back as the same Spec —
// otherwise a filter shown in the UI could not be re-typed.
func TestFormatRoundTrips(t *testing.T) {
	for _, want := range []Spec{
		{State: "open"},
		{Labels: []string{"bug", "good first issue"}},
		{State: "all", Labels: []string{"bug"}, Sort: "updated", Match: "two words"},
		{Match: "crash"},
	} {
		expr := Format(want)
		got, err := Parse(expr)
		if err != nil {
			t.Fatalf("Parse(Format(%+v)) = %q: %v", want, expr, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("round trip of %+v through %q gave %+v", want, expr, got)
		}
	}
}
