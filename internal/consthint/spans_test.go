package consthint

import (
	"testing"

	"ike/internal/epochtime"
	"ike/internal/lang"
	"ike/internal/numhint"
)

// span is the compact expectation of one produced conceal.
type span struct {
	line, start, end int
	capture, replace string
}

func checkSpans(t *testing.T, got []lang.Span, want []span) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d spans %+v, want %d", len(got), got, len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Line != w.line || g.StartCol != w.start || g.EndCol != w.end ||
			g.Capture != w.capture || g.Replace != w.replace {
			t.Errorf("span[%d] = %+v, want %+v", i, g, w)
		}
	}
}

// TestPythonSpans: a CONST_CASE assignment reads by its name — byte words as
// sizes, duration words in their base — with pure literal arithmetic
// evaluated first; lowercase assignments and non-literal right-hand sides
// produce nothing (#1701).
func TestPythonSpans(t *testing.T) {
	got := PythonSpans([]string{
		"MAX_BYTES = 10 * 1024 * 1024",
		"TIMEOUT_MS: Final = 30 * 1000  # half a minute",
		"CACHE_TTL = 3600",
		"SECONDS_PER_DAY = 60 * 60 * 24",
		"_BUFFER_SIZE = 1 << 16",
		"retries = 3 * 1024",
		"LIMIT = get_limit()",
		"SCALE = MAX_BYTES * 2",
		"PORT = 8080",
		"RATIO = 1.5 * 2",
	})
	checkSpans(t, got, []span{
		{0, 12, 28, numhint.SizeCapture, "10 MiB"},
		{1, 20, 29, numhint.DurationCapture, "30s"},
		{2, 12, 16, numhint.DurationCapture, "1h"},
		{3, 18, 30, numhint.GroupCapture, "86_400"},
		{4, 15, 22, numhint.SizeCapture, "64 KiB"},
	})
}

// TestPythonSpansPrefixedLiteral: a single 0x/0b literal gains its decimal
// reading appended, the same rule the config scan applies to hex (#1627).
func TestPythonSpansPrefixedLiteral(t *testing.T) {
	got := PythonSpans([]string{"HEADER_MAGIC = 0xCAFE"})
	checkSpans(t, got, []span{
		{0, 15, 21, numhint.RadixCapture, "0xCAFE" + numhint.Gap + "= 51_966"},
	})
}

// TestGoSpans: single-line `const` declarations and the lines of a
// `const ( … )` block, with optional types; `iota` and identifier arithmetic
// stay raw. Go's own precedence applies: a flag built with `1<<4 + 1` is 17,
// and a `mask` name draws the hex reading (#1701).
func TestGoSpans(t *testing.T) {
	got := GoSpans([]string{
		"const cacheTTL = 60 * 60",
		"const (",
		"\tMaxUploadBytes       = 10 << 20 // ten MiB",
		"\tbackoffMS      int64 = 2500",
		"\tkindFirst            = iota",
		"\tanswer               = 6 * 7",
		")",
		"const mask = 1<<4 + 1",
		"x := 4 * 1024",
	})
	checkSpans(t, got, []span{
		{0, 17, 24, numhint.DurationCapture, "1h"},
		{2, 24, 32, numhint.SizeCapture, "10 MiB"},
		{3, 24, 28, numhint.DurationCapture, "2s500ms"},
		{5, 24, 29, numhint.GroupCapture, "42"},
		{7, 13, 21, numhint.RadixCapture, "1<<4 + 1" + numhint.Gap + "= 0x11"},
	})
}

// TestPHPSpans: `const` (with modifiers and types) and `define()` both
// produce the name-derived reading; strings and identifiers stay raw (#1701).
func TestPHPSpans(t *testing.T) {
	got := PHPSpans([]string{
		"const MAX_BYTES = 10 * 1024 * 1024;",
		"public const TIMEOUT_MS = 2 * 1000; // grace period",
		"private const int MAX_PAYLOAD_BYTES = 2 * 1024 * 1024;",
		"define('CACHE_TTL', 60 * 60 * 24);",
		"define('LEGACY_MODE', 0755);",
		"const GREETING = 'hello';",
		"const DERIVED = self::BASE * 2;",
	})
	checkSpans(t, got, []span{
		{0, 18, 34, numhint.SizeCapture, "10 MiB"},
		{1, 26, 34, numhint.DurationCapture, "2s"},
		{2, 38, 53, numhint.SizeCapture, "2 MiB"},
		{3, 20, 32, numhint.DurationCapture, "24h"},
		{4, 22, 26, numhint.RadixCapture, "0755" + numhint.Gap + "= 493"},
	})
}

// TestFieldUnitMapping: the user's number_hint_units mapping (#1685) covers
// constant names too, and is final — a `none` mapping silences the shape
// families over a value they would otherwise conceal.
func TestFieldUnitMapping(t *testing.T) {
	numhint.SetFieldUnits([]string{"window=ms", "block_step=none", "*_epoch=timestamp-s"})
	t.Cleanup(func() { numhint.SetFieldUnits(nil) })
	got := PythonSpans([]string{
		"WINDOW = 40 * 25",
		"BLOCK_STEP = 4 * 1024",
		"START_EPOCH = 1700000000 + 3600",
	})
	if len(got) != 2 {
		t.Fatalf("got %d spans %+v, want 2", len(got), got)
	}
	if got[0].Capture != numhint.DurationCapture || got[0].Replace != "1s" {
		t.Errorf("mapped ms span = %+v, want 1s duration", got[0])
	}
	if got[1].Capture != epochtime.Capture || got[1].Replace == "" {
		t.Errorf("mapped timestamp span = %+v, want a decoded epoch", got[1])
	}
	for _, s := range got {
		if s.Line == 1 {
			t.Errorf("BLOCK_STEP mapped to none still produced %+v", s)
		}
	}
}

// TestNoRedundantReplacement: a stand-in identical to the source is noise —
// an already-underscored literal stays raw (a 1024-multiple still gains its
// byte reading, which says something the underscores do not).
func TestNoRedundantReplacement(t *testing.T) {
	if got := PythonSpans([]string{"BIG_COUNT = 10_000_000"}); len(got) != 0 {
		t.Errorf("underscored literal produced %+v, want nothing", got)
	}
	got := PythonSpans([]string{"BIG_COUNT = 10_485_760"})
	checkSpans(t, got, []span{{0, 12, 22, numhint.SizeCapture, "10 MiB"}})
}
