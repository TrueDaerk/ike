package jqplay

import (
	"strings"
	"testing"
)

// TestCheatsheetProgramsRun is the guard the whole sheet rests on (#2382):
// every authored program is evaluated against its dialect's sample document
// and must compile, run without an error and produce at least one output. A
// typo in an example would otherwise sit in the reference forever, teaching
// the language wrong to exactly the reader who cannot tell.
func TestCheatsheetProgramsRun(t *testing.T) {
	for _, d := range []Dialect{DialectJQ, DialectYQ} {
		sample := Sample(d)
		for _, e := range Cheatsheet(d) {
			if !e.Kind.Complete() {
				continue // a builtin row carries a name, not a program
			}
			res := EvaluateWith(d, e.Program, sample)
			if res.Err != "" {
				t.Errorf("%s cheatsheet %q (%s): %s", d.Name(), e.Title, e.Program, res.Err)
				continue
			}
			if len(res.Outputs) == 0 {
				t.Errorf("%s cheatsheet %q (%s): produced no output", d.Name(), e.Title, e.Program)
			}
		}
	}
}

// TestCheatsheetSampleParses keeps the sample documents themselves valid in
// both languages — a broken sample would fail every program at once and hide
// which one is actually wrong.
func TestCheatsheetSampleParses(t *testing.T) {
	for _, d := range []Dialect{DialectJQ, DialectYQ} {
		in, err := d.Parse(Sample(d))
		if err != nil {
			t.Fatalf("%s sample does not parse: %v", d.Name(), err)
		}
		if in.Len() != 1 {
			t.Errorf("%s sample: got %d values, want 1", d.Name(), in.Len())
		}
	}
}

// TestCheatsheetHasEveryBuiltin ties the reference section to the engine:
// the builtin rows are Builtins() itself, so a gojq version with a new
// function shows it without anyone editing this package.
func TestCheatsheetHasEveryBuiltin(t *testing.T) {
	sheet := Cheatsheet(DialectJQ)
	seen := map[string]CheatEntry{}
	for _, e := range sheet {
		if e.Kind == CheatBuiltin {
			seen[e.Title] = e
		}
	}
	bs := Builtins()
	if len(bs) == 0 {
		t.Fatal("no builtins at all")
	}
	if len(seen) != len(bs) {
		t.Errorf("builtin rows: got %d, want %d", len(seen), len(bs))
	}
	for _, b := range bs {
		e, ok := seen[b.Name]
		if !ok {
			t.Errorf("builtin %s missing from the cheatsheet", b.Name)
			continue
		}
		if e.Arity == "" {
			t.Errorf("builtin %s has no arity note", b.Name)
		}
		if e.Doc != b.Doc {
			t.Errorf("builtin %s doc: got %q, want %q", b.Name, e.Doc, b.Doc)
		}
	}
	// The everyday names must carry a description — the sheet is useless for
	// them without one, and this is what keeps builtinDocs from rotting.
	for _, name := range []string{"map", "select", "sort_by", "group_by", "to_entries", "from_entries", "length", "add", "unique", "walk", "ceil", "objects"} {
		if seen[name].Doc == "" {
			t.Errorf("builtin %s has no description", name)
		}
	}
}

// TestCheatsheetDialectSplit checks the sheet never shows a session the other
// language's document rules: a jq reader is not told about merge keys, a yq
// reader not about `.jsonl` streams.
func TestCheatsheetDialectSplit(t *testing.T) {
	jq := cheatTitles(Cheatsheet(DialectJQ))
	yq := cheatTitles(Cheatsheet(DialectYQ))
	for _, want := range []struct {
		in, out map[string]bool
		title   string
	}{
		{jq, yq, "several values in one buffer"},
		{jq, yq, "a number keeps its exact spelling"},
		{yq, jq, "several documents in one file"},
		{yq, jq, "aliases and merge keys are resolved"},
	} {
		if !want.in[want.title] {
			t.Errorf("%q missing from its own dialect's sheet", want.title)
		}
		if want.out[want.title] {
			t.Errorf("%q leaked into the other dialect's sheet", want.title)
		}
	}
}

// TestCheatsheetCoversTheIssuesOperations pins the everyday-programs section
// to the operations #2382 named: they are the reason the sheet exists, and a
// later cleanup dropping one should fail rather than pass quietly.
func TestCheatsheetCoversTheIssuesOperations(t *testing.T) {
	var programs []string
	for _, e := range Cheatsheet(DialectJQ) {
		if e.Kind == CheatExample {
			programs = append(programs, e.Program)
		}
	}
	joined := strings.Join(programs, "\n")
	for _, want := range []string{".meta.total", ".users[]", "select(", "map(", "sort_by(", "group_by(", "from_entries", "length", "[.users[].tags[]]", "//", `\(`} {
		if !strings.Contains(joined, want) {
			t.Errorf("no everyday example uses %q", want)
		}
	}
}

// TestCheatsheetEntriesAreOneLine keeps the sheet a sheet: a program or a
// description spilling over a line would not fit the picker row it is read in.
func TestCheatsheetEntriesAreOneLine(t *testing.T) {
	for _, d := range []Dialect{DialectJQ, DialectYQ} {
		for _, e := range Cheatsheet(d) {
			if strings.ContainsAny(e.Program+e.Doc+e.Title+e.Output+e.Usage, "\n\r") {
				t.Errorf("%s cheatsheet %q spans more than one line", d.Name(), e.Title)
			}
			if e.Title == "" {
				t.Errorf("%s cheatsheet entry %+v has no title", d.Name(), e)
			}
			if e.Kind.Insertable() && e.Program == "" {
				t.Errorf("%s cheatsheet entry %+v is incomplete", d.Name(), e)
			}
			if e.Kind.Complete() && e.Doc == "" {
				t.Errorf("%s cheatsheet %q has no description", d.Name(), e.Title)
			}
		}
	}
}

// TestCheatsheetExamplesShowTheirOutput is #2482's first content case: a row
// that only names a program leaves the reader to guess what applying it does.
// Every runnable row carries what it actually printed against the sample —
// computed from the same evaluation the sheet's own test checks, so the two
// cannot drift — and it has to be one line.
func TestCheatsheetExamplesShowTheirOutput(t *testing.T) {
	for _, d := range []Dialect{DialectJQ, DialectYQ} {
		for _, e := range Cheatsheet(d) {
			if !e.Kind.Complete() {
				continue
			}
			if e.Output == "" {
				t.Errorf("%s cheatsheet %q (%s) shows no output", d.Name(), e.Title, e.Program)
			}
		}
	}
	// The jq sheet's own arithmetic: the output beside the row is what the
	// program prints, not a hand-written promise.
	var found bool
	for _, e := range Cheatsheet(DialectJQ) {
		if e.Program == ".users | map(.name)" {
			found = true
			if !strings.Contains(e.Output, "ada") || !strings.Contains(e.Output, "grace") {
				t.Errorf(`map(.name) output = %q, want the sample's names`, e.Output)
			}
		}
	}
	if !found {
		t.Fatal("the map(.name) example is gone — the assertion above tests nothing")
	}
}

// TestCheatsheetBuiltinsShowAUsageForm: `/1` says a function takes one
// argument, not what the call looks like. Every builtin row carries the shape
// it is written in, curated where a parameter name says more than a letter.
func TestCheatsheetBuiltinsShowAUsageForm(t *testing.T) {
	usage := map[string]string{}
	for _, e := range Cheatsheet(DialectJQ) {
		if e.Kind != CheatBuiltin {
			continue
		}
		if e.Usage == "" {
			t.Errorf("builtin %q has no usage form", e.Title)
		}
		usage[e.Title] = e.Usage
	}
	for name, want := range map[string]string{
		"map":    "map(f)",
		"select": "select(cond)",
		"length": "length",
		"test":   "test(regex)",
	} {
		if got := usage[name]; got != want {
			t.Errorf("usage of %s = %q, want %q", name, got, want)
		}
	}
}

// TestCheatsheetLeadsWithTheGuideRows: the sheet's first two rows say what
// enter does and where the sample document is — the two things #2482 found a
// reader had to guess. Both are inert to insert: they are text about the
// sheet, not language to pick up.
func TestCheatsheetLeadsWithTheGuideRows(t *testing.T) {
	for _, d := range []Dialect{DialectJQ, DialectYQ, DialectXMQ} {
		sheet := Cheatsheet(d)
		if len(sheet) < 2 || sheet[0].Kind != CheatGuide {
			t.Fatalf("%s sheet does not lead with a guide row", d.Name())
		}
		var guide []string
		var sampleRow bool
		for _, e := range sheet {
			if e.Kind != CheatGuide {
				break // the guide block is contiguous and first
			}
			if e.Kind.Insertable() {
				t.Errorf("%s guide row %q would insert something", d.Name(), e.Title)
			}
			guide = append(guide, e.Title+" "+e.Doc)
			sampleRow = sampleRow || e.Title == CheatSampleTag
		}
		if !sampleRow {
			t.Errorf("%s sheet has no %q guide row", d.Name(), CheatSampleTag)
		}
		// The block must name both insertion shapes, and both come from
		// Complete() rather than being written out twice.
		joined := strings.Join(guide, "\n")
		for _, want := range []string{"⏎", "esc", "replaces the program; ↑ restores it", "inserts the name at the caret"} {
			if !strings.Contains(joined, want) {
				t.Errorf("%s guide block does not mention %q:\n%s", d.Name(), want, joined)
			}
		}
	}
}

// TestCheatsheetListsTheSampleDocument: the sample is viewable from the sheet
// itself, line by line, so `.users[]` means something before it is inserted.
func TestCheatsheetListsTheSampleDocument(t *testing.T) {
	for _, d := range []Dialect{DialectJQ, DialectYQ, DialectXMQ} {
		var got []string
		for _, e := range Cheatsheet(d) {
			if e.Kind == CheatSample {
				got = append(got, e.Title)
			}
		}
		if len(got) == 0 {
			t.Fatalf("%s sheet lists no sample rows", d.Name())
		}
		joined := strings.Join(got, "\n")
		for _, line := range strings.Split(Sample(d), "\n") {
			line = strings.TrimRight(line, " \t")
			if line == "" {
				continue
			}
			if !strings.Contains(joined, line) {
				t.Errorf("%s sample line %q is not listed", d.Name(), line)
			}
		}
	}
}

// TestCheatsheetIsMemoized: building the sheet now runs every example, so it
// must happen once per dialect rather than on every palette open.
func TestCheatsheetIsMemoized(t *testing.T) {
	a, b := Cheatsheet(DialectJQ), Cheatsheet(DialectJQ)
	if len(a) == 0 || len(a) != len(b) {
		t.Fatalf("sheet lengths %d and %d", len(a), len(b))
	}
	if &a[0] != &b[0] {
		t.Error("Cheatsheet rebuilds the sheet on every call")
	}
}

func cheatTitles(entries []CheatEntry) map[string]bool {
	out := make(map[string]bool, len(entries))
	for _, e := range entries {
		out[e.Title] = true
	}
	return out
}
