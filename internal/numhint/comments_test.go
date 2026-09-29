package numhint

import (
	"testing"
	"time"
)

// comments_test.go covers the comment unit override (#2816): a trailing
// comment or the comment line directly above names the unit of a line's
// literals, outranking the field mapping and the key words.

// replaceOn returns the stand-in hinted on line li, "" when none.
func replaceOn(hints []Hint, li int) string {
	for _, h := range hints {
		if h.Span.Line == li && h.hinted() {
			return h.Span.Replace
		}
	}
	return ""
}

// TestCommentOverrideIssueExample is the issue's example verbatim.
func TestCommentOverrideIssueExample(t *testing.T) {
	SetFieldUnits(nil)
	lines := []string{
		`x = {`,
		`    "timeout": 500,        # milliseconds`,
		`    "timeout2": 500000,    # microseconds`,
		`    "timeout3": 500,       # seconds`,
		`}`,
		``,
		`# seconds`,
		`$timeout = 500;`,
	}
	hs := Hints(lines)
	// 500 in its own base renders nothing — the duration formatter's normal
	// output for a value that already reads as written — but the comment
	// still decided it and claims the digits.
	for li, want := range map[int]string{1: "", 2: "500ms", 3: "8m20s", 7: "8m20s"} {
		if got := replaceOn(hs, li); got != want {
			t.Errorf("line %d = %q, want %q", li, got, want)
		}
	}
	for _, h := range hs {
		if h.Why.Source != SourceComment || !h.Claims {
			t.Errorf("line %d: source %v claims %v, want a claiming comment override", h.Span.Line, h.Why.Source, h.Claims)
		}
	}
	if len(hs) != 4 {
		t.Errorf("hints = %+v, want one per literal", hs)
	}
}

// TestCommentOverrideWithoutOne: the same buffer without the comments reads
// every timeout in the key word's milliseconds — the baseline the comments
// override.
func TestCommentOverrideWithoutOne(t *testing.T) {
	SetFieldUnits(nil)
	hs := Hints([]string{`"timeout3": 90000,`})
	if got := replaceOn(hs, 0); got != "1m30s" {
		t.Fatalf("baseline = %q, want 1m30s", got)
	}
}

func TestPrecedingCommentScope(t *testing.T) {
	SetFieldUnits(nil)
	cases := []struct {
		name  string
		lines []string
		li    int
		want  string
	}{
		{"directly above", []string{"# seconds", "timeout = 500"}, 1, "8m20s"},
		{"blank line breaks", []string{"# seconds", "", "timeout = 90000"}, 2, "1m30s"},
		{"statement breaks", []string{"# seconds", "a = 1", "timeout = 90000"}, 2, "1m30s"},
		{"next line only", []string{"# seconds", "a = 1", "timeout = 500"}, 1, ""},
		{"nearest wins", []string{"# minutes", "# seconds", "timeout = 500"}, 2, "8m20s"},
		{"nearest naming a unit", []string{"# seconds", "# see docs", "timeout = 500"}, 2, "8m20s"},
		{"trailing beats above", []string{"# minutes", "timeout = 500  # seconds"}, 1, "8m20s"},
		{"indented comment", []string{"  // seconds", "  timeout = 500"}, 1, "8m20s"},
	}
	for _, c := range cases {
		if got := replaceOn(Hints(c.lines), c.li); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCommentWithoutUnit(t *testing.T) {
	SetFieldUnits(nil)
	for _, line := range []string{
		"timeout = 90000  # see docs",
		"timeout = 90000  # msg",
		"timeout = 90000  # it's slow",
		"timeout = 90000  # let us know",
		"timeout = 90000  # min 1, max 10",
	} {
		hs := Hints([]string{line})
		if got := replaceOn(hs, 0); got != "1m30s" {
			t.Errorf("%q = %q, want the key word's 1m30s", line, got)
		}
		if hs[0].Why.Source == SourceComment {
			t.Errorf("%q: comment override without a unit word", line)
		}
	}
}

func TestCommentUnitWords(t *testing.T) {
	cases := []struct {
		text string
		word string
		unit Unit
	}{
		{"# SECONDS", "seconds", Unit{Kind: UnitDuration, Base: time.Second}},
		{"// in ms, please", "ms", Unit{Kind: UnitDuration, Base: time.Millisecond}},
		{"# (bytes)", "bytes", Unit{Kind: UnitBytes}},
		{"-- octal", "octal", Unit{Kind: UnitOctal}},
		{"; Hex flags", "hex", Unit{Kind: UnitHex}},
		{"# microseconds, not ms", "microseconds", Unit{Kind: UnitDuration, Base: time.Microsecond}},
	}
	for _, c := range cases {
		got, ok := CommentUnit(c.text)
		if !ok || got.Word != c.word || got.Unit != c.unit || got.Text != c.text {
			t.Errorf("CommentUnit(%q) = %+v %v, want %q %+v", c.text, got, ok, c.word, c.unit)
		}
	}
	for _, text := range []string{"# none", "# group", "# size", "# timestamp", "# msecs2"} {
		if got, ok := CommentUnit(text); ok {
			t.Errorf("CommentUnit(%q) = %+v, want no unit", text, got)
		}
	}
}

// TestCommentBeatsFieldRule: the comment is more specific than the user's
// mapping, and even a field mapped to none reads by the comment.
func TestCommentBeatsFieldRule(t *testing.T) {
	SetFieldUnits([]string{"timeout=ms", "trace=none"})
	defer SetFieldUnits(nil)
	hs := Hints([]string{"timeout = 500  # seconds", "trace = 2048 # bytes"})
	if got := replaceOn(hs, 0); got != "8m20s" {
		t.Errorf("mapped timeout = %q, want 8m20s", got)
	}
	if got := replaceOn(hs, 1); got != "2 KiB" {
		t.Errorf("trace = %q, want 2 KiB", got)
	}
}

// TestCommentFamilies: bytes and radix words override too, and a comment on a
// line claims the digits against the epoch family like a field rule does.
func TestCommentFamilies(t *testing.T) {
	SetFieldUnits(nil)
	hs := Hints([]string{
		"limit = 1048576  # bytes",
		"perm = 493  # octal",
		"bits = 255  # hex",
		"stamp = 1722945600  # seconds",
	})
	want := map[int]string{0: "1 MiB", 1: "493" + Gap + "= 0o755", 2: "255" + Gap + "= 0xFF", 3: "19941d12h"}
	for li, w := range want {
		if got := replaceOn(hs, li); got != w {
			t.Errorf("line %d = %q, want %q", li, got, w)
		}
	}
	for _, h := range hs {
		if !h.Claims {
			t.Errorf("line %d does not claim its digits", h.Span.Line)
		}
	}
}

// TestCommentDigitGroupingUnaffected: a comment naming no in-scope unit leaves
// a grouped value alone.
func TestCommentDigitGroupingUnaffected(t *testing.T) {
	SetFieldUnits(nil)
	if got := replaceOn(Hints([]string{"count = 1000000  # group"}), 0); got != "1_000_000" {
		t.Errorf("count = %q, want grouped", got)
	}
}

// TestCommentLeaders: the leaders decide what opens a comment, and a leader
// inside a string is no comment.
func TestCommentLeaders(t *testing.T) {
	SetFieldUnits(nil)
	cases := []struct {
		name    string
		lines   []string
		leaders []string
		want    string
	}{
		{"sql dashes trailing", []string{"timeout = 500 -- seconds"}, []string{"--"}, "8m20s"},
		{"sql dashes above", []string{"-- seconds", "timeout = 500"}, []string{"--"}, "8m20s"},
		{"ini semicolon", []string{"timeout = 500 ; seconds"}, []string{";", "#"}, "8m20s"},
		{"slashes", []string{"timeout = 500 // seconds"}, []string{"//"}, "8m20s"},
		{"hash not a leader", []string{"timeout = 500 # seconds"}, []string{"//"}, ""},
		{"default leaders", []string{"timeout = 500 // seconds"}, nil, "8m20s"},
		{"semicolon not default", []string{"timeout = 90000 ; seconds"}, nil, "1m30s"},
		{"hash inside a string", []string{`"label": "x # seconds", "timeout": 90000`}, nil, "1m30s"},
		{"string then comment", []string{`"timeout": 500, "k": "#x" # seconds`}, nil, "8m20s"},
		{"leader glued to a token", []string{"timeout = 500#seconds"}, nil, ""},
	}
	for _, c := range cases {
		if got := replaceOn(Hints(c.lines, c.leaders...), len(c.lines)-1); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCommentLeadersByLanguage(t *testing.T) {
	if got := CommentLeaders("ini"); !containsString(got, ";") {
		t.Errorf("ini leaders = %v, want ; among them", got)
	}
	if got := CommentLeaders("json"); !containsString(got, "//") {
		t.Errorf("json leaders = %v, want //", got)
	}
	if got := CommentLeaders("no-such-language"); len(got) != 2 || got[0] != "#" || got[1] != "//" {
		t.Errorf("fallback leaders = %v, want the defaults", got)
	}
}

// TestCommentLineHints: the single-line entry sees a trailing comment only.
func TestCommentLineHints(t *testing.T) {
	SetFieldUnits(nil)
	if got := replaceOn(LineHints(4, "timeout = 500 # seconds"), 4); got != "8m20s" {
		t.Errorf("LineHints trailing = %q, want 8m20s", got)
	}
}

// TestCommentSpansWith: the config-format entry point threads the leaders.
func TestCommentSpansWith(t *testing.T) {
	SetFieldUnits(nil)
	hints, _ := SpansWith([]string{"; seconds", "timeout = 500"}, nil, ";")
	if len(hints) != 1 || hints[0].Replace != "8m20s" {
		t.Errorf("hints = %+v, want the seconds reading", hints)
	}
}
