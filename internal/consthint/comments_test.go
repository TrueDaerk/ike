package consthint

import (
	"testing"

	"ike/internal/lang"
	"ike/internal/numhint"
)

// comments_test.go covers the comment unit override (#2816) in the code
// constant producers: a trailing comment or the comment line directly above a
// constant names its unit, over the name's key word and the user mapping, for
// plain literals and computed right-hand sides alike.

// replaces lists the stand-ins of the produced spans, in order.
func replaces(spans []lang.Span) []string {
	var out []string
	for _, s := range spans {
		out = append(out, s.Replace)
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCommentOverrideConstants(t *testing.T) {
	numhint.SetFieldUnits(nil)
	cases := []struct {
		name  string
		spans func([]string) []lang.Span
		lines []string
		want  []string
	}{
		{"php", PHPSpans, []string{
			"# seconds",
			"$timeout = 500;",
			"$timeout2 = 500; // seconds",
			"const TIMEOUT3 = 60 * 1000; # seconds",
			"$timeout4 = 90000;",
		}, []string{"8m20s", "8m20s", "16h40m", "1m30s"}},
		{"python", PythonSpans, []string{
			"TIMEOUT = 500  # seconds",
			"# seconds",
			"timeout = 500",
			"",
			"timeout = 90000",
			"connect(timeout=500)  # seconds",
		}, []string{"8m20s", "8m20s", "1m30s", "8m20s"}},
		{"go", GoSpans, []string{
			"const (",
			"\t// seconds",
			"\tTimeout = 500",
			"\tFlushInterval = 500 // seconds",
			"\tRetryDelay = 90000",
			")",
		}, []string{"8m20s", "8m20s", "1m30s"}},
		{"script", ScriptSpans, []string{
			"// seconds",
			"const TIMEOUT = 500;",
			"const TIMEOUT_2 = 500; // seconds",
			"const TIMEOUT_3 = 90000;",
		}, []string{"8m20s", "8m20s", "1m30s"}},
	}
	for _, c := range cases {
		if got := replaces(c.spans(c.lines)); !sameStrings(got, c.want) {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// TestCommentOverrideBeatsMapping: the comment outranks the user's field
// mapping for a computed right-hand side too.
func TestCommentOverrideBeatsMapping(t *testing.T) {
	numhint.SetFieldUnits([]string{"CACHE_SIZE=bytes"})
	defer numhint.SetFieldUnits(nil)
	got := replaces(PythonSpans([]string{"CACHE_SIZE = 60 * 60  # seconds"}))
	if !sameStrings(got, []string{"1h"}) {
		t.Errorf("spans = %q, want the comment's 1h", got)
	}
}
