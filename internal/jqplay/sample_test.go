package jqplay

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sample_test.go covers a saved filter's self-test (#2792): the budgeted
// capture, the check's three outcomes, and that the store stays readable in
// both directions across the new fields.

func TestNewSampleCapturesRunAndFlags(t *testing.T) {
	in, err := Parse(`{"a":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	res := RunWith(context.Background(), ".a", in, Options{Raw: true})
	s, err := NewSample(`{"a":"x"}`, res, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if s.Input != `{"a":"x"}` || s.Expect != "x" || s.Flags != "-r" {
		t.Fatalf("captured %+v", s)
	}
	if st := CheckFilter(context.Background(), DialectJQ, Filter{Name: "a", Program: ".a", SelfTest: s}); st != CheckPass {
		t.Fatalf("the raw expectation must check under -r, got %v", st)
	}
}

func TestNewSampleBudget(t *testing.T) {
	big := strings.Repeat("1\n", MaxSampleBytes)
	if _, err := NewSample(big, Result{}, false, 0); !errors.Is(err, ErrSampleTooLarge) {
		t.Fatalf("an over-budget input returned %v", err)
	}
	res := Result{Outputs: []string{strings.Repeat("x", MaxSampleBytes+1)}}
	if _, err := NewSample("1", res, false, 0); !errors.Is(err, ErrSampleTooLarge) {
		t.Fatalf("an over-budget result returned %v", err)
	}
	if _, err := NewSample("1", Result{Truncated: true}, false, 0); !errors.Is(err, ErrSampleTooLarge) {
		t.Fatalf("a truncated result returned %v", err)
	}
}

func TestCheckFilterStates(t *testing.T) {
	ctx := context.Background()
	sample := SelfTest{Input: `{"a":{"b":3}}`, Expect: "3\n"}
	cases := []struct {
		name string
		f    Filter
		want CheckState
	}{
		{"no sample", Filter{Program: ".a.b"}, CheckNone},
		{"pass", Filter{Program: ".a.b", SelfTest: sample}, CheckPass},
		{"other output", Filter{Program: ".a", SelfTest: sample}, CheckFail},
		{"compile error", Filter{Program: ".a.[", SelfTest: sample}, CheckFail},
		{"runtime error", Filter{Program: ".a.b | keys", SelfTest: sample}, CheckFail},
		{"broken sample", Filter{Program: ".a.b", SelfTest: SelfTest{Input: "{", Expect: "3"}}, CheckFail},
		{"empty expectation", Filter{Program: ".a | select(.b > 5)", SelfTest: SelfTest{Input: sample.Input}}, CheckPass},
	}
	for _, c := range cases {
		if got := CheckFilter(ctx, DialectJQ, c.f); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	yq := Filter{Program: ".a", SelfTest: SelfTest{Input: "a: 1\n", Expect: "1"}}
	if got := CheckFilter(ctx, DialectYQ, yq); got != CheckPass {
		t.Errorf("yq: got %v", got)
	}
	csv := Filter{Program: ".[0].n", SelfTest: SelfTest{Input: "n;m\nx;y\n", Expect: `"x"`, CSV: true, Sep: ";"}}
	if got := CheckFilter(ctx, DialectJQ, csv); got != CheckPass {
		t.Errorf("csv: got %v", got)
	}
}

func TestLibrarySampleRoundTripAndCompat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "filters.json")
	if err := os.WriteFile(path, []byte(`{"filters":[{"name":"old","program":".a"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	lib := LoadLibrary(path)
	if f, ok := lib.Get("old"); !ok || f.SelfTest.Has() {
		t.Fatalf("the pre-#2792 entry loaded as %+v", f)
	}
	if err := lib.Set("new", ".b"); err != nil {
		t.Fatal(err)
	}
	st := SelfTest{Input: `{"b":2}`, Expect: "2", Flags: "-c"}
	if err := lib.SetSample("new", st); err != nil {
		t.Fatal(err)
	}
	if err := lib.SetSample("missing", st); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a sample for an unknown name returned %v", err)
	}
	// An overwrite without a new sample keeps the one the filter carries.
	if err := lib.Set("new", ".b + 0"); err != nil {
		t.Fatal(err)
	}
	if err := lib.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), `"old","program":".a","sample"`) || strings.Count(string(data), `"sample"`) != 1 {
		t.Fatalf("a filter without a sample must keep the old shape:\n%s", data)
	}
	back := LoadLibrary(path)
	f, _ := back.Get("new")
	if f.Program != ".b + 0" || f.SelfTest != st {
		t.Fatalf("round trip gave %+v", f)
	}
}
