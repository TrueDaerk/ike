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
		{"state", "state:closed", Spec{State: "closed"}},
		{"labels are repeated, not comma separated", "label:bug label:triage",
			Spec{Labels: []string{"bug", "triage"}}},
		{"a duplicate label is kept once", "label:bug label:bug", Spec{Labels: []string{"bug"}}},
		{"quoted label value", `label:"good first issue"`, Spec{Labels: []string{"good first issue"}}},
		{"all three dimensions", "state:all label:bug match:crash",
			Spec{State: "all", Labels: []string{"bug"}, Match: "crash"}},
		{"a bare word is match text", "crash", Spec{Match: "crash"}},
		{"bare words join the match qualifier", "match:panic crash", Spec{Match: "panic crash"}},
		{"a quoted word is never a qualifier", `"fix:tests"`, Spec{Match: "fix:tests"}},
		{"an upper-case key is not a qualifier", "Fix:tests", Spec{Match: "Fix:tests"}},
		{"a quoted match value keeps its spaces", `match:"two words"`, Spec{Match: "two words"}},
		{"the last state wins", "state:open state:closed", Spec{State: "closed"}},
		{"an escaped quote is literal", `match:"a\"b"`, Spec{Match: `a"b`}},
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

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want string // a substring the message must name
	}{
		{"unknown qualifier", "author:me", `unknown qualifier "author:"`},
		{"unknown state", "state:merged", `unknown state "merged"`},
		{"empty label", "label:", "label: needs a label name"},
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
	name, spec, err := ParseSaved("triage=state:open label:bug match:crash")
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
		{"no name", "=state:open", "missing name"},
		{"empty expression", "triage=", "narrows nothing"},
		{"bad expression", "triage=state:merged", `unknown state "merged"`},
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
		{State: "all", Labels: []string{"bug"}, Match: "two words"},
		{Match: `a"b`},
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
