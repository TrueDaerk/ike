package palette

import (
	"reflect"
	"testing"
)

// tailFixture mirrors the tree behind #2887's telemetry: three packages of
// 30-odd files each, so a query that is a package name matches 30–45 files.
// Each package holds its namesake file, a run of ordinary names, two "trap"
// files whose name starts with the query's last letter (scored over the whole
// path the matcher used to split the query there, a boundary bonus beating a
// consecutive one) and the short file the user is after.
func tailFixture() *FileMode {
	names := []string{
		"aggregate.go", "aggregate_test.go", "arrowkeys_test.go", "batch.go",
		"batch_test.go", "cached_test.go", "clock.go", "decoder.go",
		"decoder_test.go", "filter.go", "filterov.go", "keycmds.go",
		"keycmds_test.go", "keymap_test.go", "labelmode_test.go",
		"matchstep_test.go", "mdlist_test.go", "mouse.go", "mutations.go",
		"mutations_test.go", "overlays.go", "poll_test.go", "prdetail.go",
		"prdetail_test.go", "qualifier.go", "qualifier_test.go", "textedit.go",
		"textedit_test.go", "timeline.go", "timeline_test.go",
	}
	pkgs := map[string][]string{
		"ghissues":  {"savedfilter.go", "selection.go", "view.go"},
		"httppane":  {"export_test.go", "encode.go", "body.go"},
		"telemetry": {"yank.go", "yield_test.go", "sink.go"},
	}
	var paths []string
	for pkg, extra := range pkgs {
		paths = append(paths, "internal/"+pkg+"/"+pkg+".go", "internal/"+pkg+"/"+pkg+"_test.go")
		for _, n := range names {
			paths = append(paths, "internal/"+pkg+"/"+n)
		}
		for _, n := range extra {
			paths = append(paths, "internal/"+pkg+"/"+n)
		}
	}
	return fileMode(paths...)
}

// TestFileModeDirectoryQueryRanksShortNamesHigh guards #2887: for a query that
// is a directory name, every file under that directory matches the segment
// alike, so the ones with the shortest paths lead instead of the files whose
// name let the scorer borrow a boundary (savedfilter.go for "ghissues") and
// then the alphabet — which put the picked file at rank 34 of 36.
func TestFileModeDirectoryQueryRanksShortNamesHigh(t *testing.T) {
	setCase(t, "first_letter")
	f := tailFixture()
	cases := []struct {
		query, want string
		matches     int
	}{
		{"ghissues", "internal/ghissues/view.go", 35},
		{"httppane", "internal/httppane/body.go", 35},
		{"telemetry", "internal/telemetry/sink.go", 35},
	}
	for _, c := range cases {
		got := titles(f.Results(c.query, Context{Root: "/proj"}))
		if len(got) != c.matches {
			t.Fatalf("%q matched %d files, want %d: %v", c.query, len(got), c.matches, got)
		}
		rank := -1
		for i, p := range got {
			if p == c.want {
				rank = i
			}
		}
		if rank < 0 || rank > 2 {
			t.Errorf("%q: %s at rank %d, want top 3; list: %v", c.query, c.want, rank, got)
		}
		// The namesake file is still the exact tier, above every segment hit.
		if got[0] != "internal/"+c.query+"/"+c.query+".go" {
			t.Errorf("%q: first row %s, want the namesake file", c.query, got[0])
		}
	}
}

// TestFileModeSegmentMatchBeatsSpanningMatch guards #2887's tier: a hump
// match confined to one directory segment ranks above one that has to be
// stitched together across segments, whatever the whole-path score says.
func TestFileModeSegmentMatchBeatsSpanningMatch(t *testing.T) {
	setCase(t, "first_letter")
	f := fileMode("gh/issues.go", "internal/ghissues/mouse.go", "ghissues_test.go")
	got := titles(f.Results("ghissues", Context{Root: "/proj"}))
	want := []string{"ghissues_test.go", "internal/ghissues/mouse.go", "gh/issues.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ghissues = %v, want %v", got, want)
	}

	// Spans of a segment match index the whole path, not the segment.
	items := f.Results("ghissues", Context{Root: "/proj"})
	if sp := items[1].Spans; len(sp) != 8 || sp[0] != 9 || sp[7] != 16 {
		t.Fatalf("segment spans = %v, want 9..16 (inside internal/ghissues/)", sp)
	}
}

// TestFileModeEqualTierOrderedByLengthThenName guards #2887's tie-break:
// results equal on tier, score, frecency and usage list by path length, then
// alphabetically — never in walk order.
func TestFileModeEqualTierOrderedByLengthThenName(t *testing.T) {
	setCase(t, "first_letter")
	f := fileMode(
		"internal/ghissues/zz.go",
		"internal/ghissues/abc.go",
		"internal/ghissues/ab.go",
		"internal/ghissues/deep/x.go",
	)
	got := titles(f.Results("ghissues", Context{Root: "/proj"}))
	want := []string{
		"internal/ghissues/ab.go",
		"internal/ghissues/zz.go",
		"internal/ghissues/abc.go",
		"internal/ghissues/deep/x.go",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ghissues = %v, want %v", got, want)
	}
}
