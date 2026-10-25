package complexity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// baseline_test.go covers the accepted-debt baseline (#2958): the file
// round trip and its deterministic order, the malformed-file errors, the
// classification table and what Summarize makes of a stamped population.

func TestBaselineRoundTripSorted(t *testing.T) {
	dir := t.TempDir()
	path := BaselinePath(dir)
	b := NewBaseline(15, 15)
	b.Accept("z/last.go", Function{Name: "f", CCN: 20, Cognitive: 3}, 0)
	b.Accept("a/first.go", Function{Name: "m", CCN: 16, Cognitive: 1}, 1)
	b.Accept("a/first.go", Function{Name: "m", CCN: 18, Cognitive: 2}, 0)
	b.Accept("a/first.go", Function{Name: "b", CCN: 30, Cognitive: 40}, 0)
	if err := b.Save(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "version": 2,
  "ccn_warn": 15,
  "cognitive_warn": 15,
  "accepted": [
    {
      "file": "a/first.go",
      "function": "b",
      "ordinal": 0,
      "ccn": 30,
      "cognitive": 40
    },
    {
      "file": "a/first.go",
      "function": "m",
      "ordinal": 0,
      "ccn": 18,
      "cognitive": 2
    },
    {
      "file": "a/first.go",
      "function": "m",
      "ordinal": 1,
      "ccn": 16,
      "cognitive": 1
    },
    {
      "file": "z/last.go",
      "function": "f",
      "ordinal": 0,
      "ccn": 20,
      "cognitive": 3
    }
  ]
}
`
	if string(data) != want {
		t.Fatalf("written file:\n%s\nwant:\n%s", data, want)
	}
	got, err := LoadBaseline(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != BaselineVersion || got.CCNWarn != 15 || got.CognitiveWarn != 15 || got.Len() != 4 || got.Accepted[3].File != "z/last.go" {
		t.Fatalf("loaded %+v", got)
	}
	// Re-accepting replaces the metrics instead of adding a second entry.
	got.Accept("z/last.go", Function{Name: "f", CCN: 25, Cognitive: 9}, 0)
	if e, _ := got.Lookup(EntryKey{"z/last.go", "f", 0}); got.Len() != 4 || e.CCN != 25 || e.Cognitive != 9 {
		t.Fatalf("re-accept must replace: %+v", got.Accepted)
	}
	if !got.Remove(EntryKey{"z/last.go", "f", 0}) || got.Remove(EntryKey{"z/last.go", "f", 0}) || got.Len() != 3 {
		t.Fatalf("remove must drop the entry once: %+v", got.Accepted)
	}
	// An empty baseline still writes the accepted array, not null.
	empty := NewBaseline(10, 12)
	if err := empty.Save(path); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), `"accepted": []`) {
		t.Fatalf("empty baseline must write []: %s", data)
	}
}

func TestLoadBaselineMissingAndMalformed(t *testing.T) {
	dir := t.TempDir()
	if b, err := LoadBaseline(BaselinePath(dir)); b != nil || err != nil {
		t.Fatalf("missing file = %v, %v; want nil, nil", b, err)
	}
	cases := map[string]string{
		"bad json":    `{"version": 1, "accepted": [`,
		"bad version": `{"version": 3, "accepted": []}`,
		"no file":     `{"version": 1, "accepted": [{"function": "f", "ordinal": 0}]}`,
		"no function": `{"version": 1, "accepted": [{"file": "a.go", "ordinal": 0}]}`,
		"bad ordinal": `{"version": 1, "accepted": [{"file": "a.go", "function": "f", "ordinal": -1}]}`,
	}
	for name, text := range cases {
		path := filepath.Join(dir, strings.ReplaceAll(name, " ", "_")+".json")
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		if b, err := LoadBaseline(path); err == nil || b != nil {
			t.Errorf("%s: LoadBaseline = %v, %v; want an error", name, b, err)
		} else if !strings.Contains(err.Error(), filepath.Base(path)) {
			t.Errorf("%s: the error must name the file: %v", name, err)
		}
	}
	// A file without the accepted key is an empty baseline.
	path := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(path, []byte(`{"version": 1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := LoadBaseline(path); err != nil || b == nil || b.Accepted == nil || b.Len() != 0 {
		t.Fatalf("empty baseline = %+v, %v", b, err)
	}
}

func TestBaselineClassify(t *testing.T) {
	b := NewBaseline(15, 15)
	b.Accept("a.go", Function{Name: "f", CCN: 20, Cognitive: 30}, 0)
	b.Accept("a.go", Function{Name: "f", CCN: 16, Cognitive: 16}, 1)
	cases := []struct {
		name    string
		file    string
		fn      Function
		ordinal int
		want    Status
	}{
		{"accepted at the recorded metrics", "a.go", Function{Name: "f", CCN: 20, Cognitive: 30}, 0, BaselineAccepted},
		{"accepted below them", "a.go", Function{Name: "f", CCN: 3, Cognitive: 2}, 0, BaselineAccepted},
		{"worsened: CCN grew", "a.go", Function{Name: "f", CCN: 21, Cognitive: 30}, 0, BaselineWorsened},
		{"worsened: cognitive grew", "a.go", Function{Name: "f", CCN: 20, Cognitive: 31}, 0, BaselineWorsened},
		{"second ordinal has its own metrics", "a.go", Function{Name: "f", CCN: 17, Cognitive: 16}, 1, BaselineWorsened},
		{"new: no entry", "a.go", Function{Name: "g", CCN: 20, Cognitive: 30}, 0, BaselineNew},
		{"new: wrong ordinal", "a.go", Function{Name: "f", CCN: 20, Cognitive: 30}, 2, BaselineNew},
		{"new: renamed", "a.go", Function{Name: "fRenamed", CCN: 20, Cognitive: 30}, 0, BaselineNew},
		{"new: other file", "b.go", Function{Name: "f", CCN: 20, Cognitive: 30}, 0, BaselineNew},
	}
	for _, c := range cases {
		if got := b.Classify(c.file, c.fn, c.ordinal); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	var none *Baseline
	if got := none.Classify("a.go", Function{Name: "f", CCN: 20, Cognitive: 30}, 0); got != BaselineNew {
		t.Errorf("nil baseline classifies %v, want new", got)
	}
	if none.Len() != 0 || none.Remove(EntryKey{"a.go", "f", 0}) {
		t.Error("a nil baseline is empty")
	}
	for i, s := range []Status{BaselineNew, BaselineAccepted, BaselineWorsened} {
		if p, ok := ParseStatus(s.String()); !ok || p != s {
			t.Errorf("status %d: %q does not round-trip", i, s)
		}
	}
	if _, ok := ParseStatus("fine"); ok {
		t.Error("ParseStatus must refuse an unknown word")
	}
}

func TestOrdinalsAndStamp(t *testing.T) {
	fns := []Function{
		{Name: "f", CCN: 20, Cognitive: 1},
		{Name: "g", CCN: 1},
		{Name: "f", CCN: 16, Cognitive: 1},
		{Name: "f", CCN: 2},
	}
	if got := Ordinals(fns); got[0] != 0 || got[1] != 0 || got[2] != 1 || got[3] != 2 {
		t.Fatalf("ordinals = %v", got)
	}
	b := NewBaseline(15, 15)
	b.Accept("a.go", fns[0], 0)
	b.Accept("a.go", fns[2], 1)
	fns[2].CCN = 17 // worsened
	if n := b.Stamp("a.go", fns); n != 1 {
		t.Fatalf("Stamp accepted %d, want 1", n)
	}
	want := []Status{BaselineAccepted, BaselineNew, BaselineWorsened, BaselineNew}
	for i, f := range fns {
		if f.Baseline != want[i] {
			t.Errorf("fns[%d].Baseline = %v, want %v", i, f.Baseline, want[i])
		}
	}
	// Summarize: only new and worsened over-threshold units count, the
	// accepted one moves to Accepted and out of the excess.
	fns = append(fns, Function{Name: "h", CCN: 40, Cognitive: 50})
	s := Summarize(fns, Thresholds{CCN: 15, Cog: 15, Cmt: 0})
	if s.OverThreshold != 2 || s.Accepted != 1 || s.Worsened != 1 || s.Excess != (17-15)+(40-15)+(50-15) {
		t.Fatalf("summary over=%d accepted=%d worsened=%d excess=%d", s.OverThreshold, s.Accepted, s.Worsened, s.Excess)
	}
	// Without a stamp (no baseline) everything over counts, as before.
	for i := range fns {
		fns[i].Baseline = BaselineNew
	}
	if s := Summarize(fns, Thresholds{CCN: 15, Cog: 15, Cmt: 0}); s.OverThreshold != 3 || s.Accepted != 0 || s.Excess != 5+2+25+35 {
		t.Fatalf("unstamped summary over=%d accepted=%d excess=%d", s.OverThreshold, s.Accepted, s.Excess)
	}
	// A nil baseline's Stamp leaves everything new.
	var none *Baseline
	fns[0].Baseline = BaselineAccepted
	if n := none.Stamp("a.go", fns); n != 0 || fns[0].Baseline != BaselineNew {
		t.Fatalf("nil Stamp: n=%d status=%v", n, fns[0].Baseline)
	}
}
