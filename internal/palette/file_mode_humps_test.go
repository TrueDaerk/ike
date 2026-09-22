package palette

import (
	"testing"

	"ike/internal/config"
)

// humpTree is the acceptance fixture of #2686: a file whose name is the
// acronym, one reachable only across a path separator, one the query is a
// plain prefix of, and one that used to match by scattered letters alone.
func humpTree() *FileMode {
	return fileMode("GoogleAppBlizzard.php", "google/abstract.py", "gabriel.py", "log/database.py")
}

// setCase installs completion.case_sensitivity for the duration of the test,
// the way the word and symbol sources' own hump tests do (#2650).
func setCase(t *testing.T, mode string) {
	t.Helper()
	prev := config.Get()
	c := *prev
	c.Completion.CaseSensitivity = mode
	config.Set(&c)
	t.Cleanup(func() { config.Set(prev) })
}

// TestFileModeHumpFilterDropsScatteredLetters guards #2686's core acceptance
// criterion: "gab" reaches the three files whose segments start with g, a and
// b, and no longer drags in log/database.py, where the letters merely occur in
// order. The tiers order what survives — the name prefix (gabriel.py) above
// the in-name hump (GoogleAppBlizzard.php) above the match that needs a
// directory segment (google/abstract.py).
func TestFileModeHumpFilterDropsScatteredLetters(t *testing.T) {
	setCase(t, "first_letter")
	got := titles(humpTree().Results("gab", Context{Root: "/proj"}))
	want := []string{"gabriel.py", "GoogleAppBlizzard.php", "google/abstract.py"}
	if len(got) != len(want) {
		t.Fatalf("gab = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("gab = %v, want %v", got, want)
		}
	}
}

// TestFileModeHumpFilterUppercaseQuery: an uppercase query is held to the
// case rule the finder shares with the completion popup. Under the
// "first_letter" default an uppercase rune only matches an uppercase one, so
// "GAB" is the acronym of GoogleAppBlizzard.php alone — the lowercase names
// need "gab" (or case_sensitivity = "none", covered below). log/database.py
// stays out either way, which is the point of #2686.
func TestFileModeHumpFilterUppercaseQuery(t *testing.T) {
	setCase(t, "first_letter")
	got := titles(humpTree().Results("GAB", Context{Root: "/proj"}))
	if len(got) != 1 || got[0] != "GoogleAppBlizzard.php" {
		t.Fatalf("GAB = %v, want [GoogleAppBlizzard.php]", got)
	}
}

// TestFileModeCaseSensitivitySetting guards that the finder reads
// completion.case_sensitivity live: "all" holds every rune to its exact case
// (so "gab" loses GoogleAppBlizzard.php), "none" folds every rune (so "GAB"
// reaches the lowercase names).
func TestFileModeCaseSensitivitySetting(t *testing.T) {
	t.Run("all", func(t *testing.T) {
		setCase(t, "all")
		for _, title := range titles(humpTree().Results("gab", Context{Root: "/proj"})) {
			if title == "GoogleAppBlizzard.php" {
				t.Fatal(`case_sensitivity "all": gab must not match GoogleAppBlizzard.php`)
			}
		}
	})
	t.Run("none", func(t *testing.T) {
		setCase(t, "none")
		got := titles(humpTree().Results("GAB", Context{Root: "/proj"}))
		if indexOf(got, "gabriel.py") < 0 {
			t.Fatalf(`case_sensitivity "none": GAB = %v, want gabriel.py among them`, got)
		}
	})
}

// TestFileModeHumpMatchSpansIndexThePath guards the highlight contract for a
// name match: the row is titled with the relative path, so the spans of a
// match found inside the basename are shifted by the directory prefix.
func TestFileModeHumpMatchSpansIndexThePath(t *testing.T) {
	setCase(t, "first_letter")
	items := fileMode("internal/app/app.go").Results("app.go", Context{Root: "/proj"})
	if len(items) != 1 {
		t.Fatalf("items = %v, want the one file", titles(items))
	}
	want := []int{13, 14, 15, 16, 17, 18} // "app.go" inside "internal/app/app.go"
	if len(items[0].Spans) != len(want) {
		t.Fatalf("spans = %v, want %v", items[0].Spans, want)
	}
	for i := range want {
		if items[0].Spans[i] != want[i] {
			t.Fatalf("spans = %v, want %v", items[0].Spans, want)
		}
	}
}

// TestFileModePathSegmentsStillMatch guards #2686's compatibility clause: the
// query is matched against the root-relative path, so a path-shaped query
// keeps working — the separator counts as a word boundary like any other.
func TestFileModePathSegmentsStillMatch(t *testing.T) {
	setCase(t, "first_letter")
	f := fileMode("internal/app/app.go", "internal/app/keys.go")
	got := titles(f.Results("app/app", Context{Root: "/proj"}))
	if len(got) != 1 || got[0] != "internal/app/app.go" {
		t.Fatalf("app/app = %v, want [internal/app/app.go]", got)
	}
}

// TestFileModeNameMatchBeatsPathMatch guards the tier that keeps a typed name
// above a hit that has to borrow a directory segment.
func TestFileModeNameMatchBeatsPathMatch(t *testing.T) {
	setCase(t, "first_letter")
	f := fileMode("api/build/target.go", "abt.go")
	got := titles(f.Results("abt", Context{Root: "/proj"}))
	if len(got) != 2 || got[0] != "abt.go" {
		t.Fatalf("abt = %v, want the basename match first", got)
	}
}

// TestFileModePermissiveFallback guards #2686's fallback: a query that
// hump-matches nothing at all still lists what the old permissive subsequence
// matcher finds, rather than leaving the finder empty.
func TestFileModePermissiveFallback(t *testing.T) {
	setCase(t, "first_letter")
	f := fileMode("xaazzbqq.go", "unrelated.go")
	got := titles(f.Results("xzq", Context{Root: "/proj"}))
	if len(got) != 1 || got[0] != "xaazzbqq.go" {
		t.Fatalf("xzq = %v, want the permissive fallback [xaazzbqq.go]", got)
	}

	// The fallback is a last resort: with one hump match present the
	// scattered-letter candidates stay out.
	f2 := fileMode("xaazzbqq.go", "x-z-q.go")
	got = titles(f2.Results("xzq", Context{Root: "/proj"}))
	if len(got) != 1 || got[0] != "x-z-q.go" {
		t.Fatalf("xzq = %v, want only the hump match [x-z-q.go]", got)
	}
}
