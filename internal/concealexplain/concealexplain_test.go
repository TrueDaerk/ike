package concealexplain

import (
	"strings"
	"testing"

	"ike/internal/epochtime"
	"ike/internal/numhint"
	"ike/internal/secret"
)

// spanOf locates a substring on a line as rune columns, so the tests point at
// a value the way the editor does — by the columns of its conceal span.
func spanOf(t *testing.T, line, value string) (int, int) {
	t.Helper()
	i := strings.Index(line, value)
	if i < 0 {
		t.Fatalf("value %q not in line %q", value, line)
	}
	start := len([]rune(line[:i]))
	return start, start + len([]rune(value))
}

// TestExplainKeyWordHint (#1998): a hint the built-in field-name heuristic
// produced names the word it fired on and the unit it chose.
func TestExplainKeyWordHint(t *testing.T) {
	numhint.SetFieldUnits(nil)
	line := "max_buffer_size = 10485760"
	start, end := spanOf(t, line, "10485760")
	ex, ok := Explain(Request{Line: line, Start: start, End: end, Capture: numhint.SizeCapture, Display: "10 MiB"})
	if !ok {
		t.Fatal("no explanation")
	}
	if ex.Source != SourceKeyWord {
		t.Fatalf("source = %v, want SourceKeyWord", ex.Source)
	}
	if !strings.Contains(ex.Rule, `"size"`) {
		t.Fatalf("rule does not name the key word: %q", ex.Rule)
	}
	if ex.Unit != "bytes" || ex.Reading != "binary byte size" {
		t.Fatalf("unit/reading = %q/%q, want bytes", ex.Unit, ex.Reading)
	}
	if ex.Key != "max_buffer_size" || ex.Raw != "10485760" {
		t.Fatalf("key/raw = %q/%q", ex.Key, ex.Raw)
	}
	if ex.Family != "byte_size_hints" {
		t.Fatalf("family = %q", ex.Family)
	}
}

// TestExplainFieldRuleWins (#1998): a configured field rule is reported as the
// rule that fired, with its own pattern — the precedence over the built-in key
// word has to be visible in the explanation, not just in the rendering.
func TestExplainFieldRuleWins(t *testing.T) {
	numhint.SetFieldUnits([]string{"*_size=ms"})
	defer numhint.SetFieldUnits(nil)
	line := "flush_size = 90000"
	start, end := spanOf(t, line, "90000")
	ex, _ := Explain(Request{Line: line, Start: start, End: end, Capture: numhint.DurationCapture, Display: "1m30s"})
	if ex.Source != SourceFieldRule {
		t.Fatalf("source = %v, want SourceFieldRule", ex.Source)
	}
	if !strings.Contains(ex.Rule, "*_size=ms") {
		t.Fatalf("rule does not name the entry: %q", ex.Rule)
	}
	if ex.Unit != "ms" {
		t.Fatalf("unit = %q, want ms", ex.Unit)
	}
}

// TestExplainShapeHint (#1998): a hint no field name asked for names the shape
// rule instead of a pattern.
func TestExplainShapeHint(t *testing.T) {
	numhint.SetFieldUnits(nil)
	line := "chunk: 1048576"
	start, end := spanOf(t, line, "1048576")
	ex, _ := Explain(Request{Line: line, Start: start, End: end, Capture: numhint.SizeCapture, Display: "1 MiB"})
	if ex.Source != SourceShape {
		t.Fatalf("source = %v, want SourceShape", ex.Source)
	}
	if !strings.Contains(ex.Rule, "multiple of 1024") {
		t.Fatalf("rule = %q", ex.Rule)
	}
	if ex.Unit != "bytes" {
		t.Fatalf("unit = %q", ex.Unit)
	}
}

// TestExplainEpochRange (#1998): a decoded timestamp rests on the digit count
// and the range, not on the field name — and the explanation says so.
func TestExplainEpochRange(t *testing.T) {
	numhint.SetFieldUnits(nil)
	line := `{"created": 1722945600}`
	start, end := spanOf(t, line, "1722945600")
	ex, _ := Explain(Request{Line: line, Start: start, End: end, Capture: epochtime.Capture, Display: "2024-08-06 12:00:00Z"})
	if ex.Source != SourceEpochRange {
		t.Fatalf("source = %v, want SourceEpochRange", ex.Source)
	}
	if !strings.Contains(ex.Rule, "epoch range") || !strings.Contains(ex.Rule, "seconds") {
		t.Fatalf("rule = %q", ex.Rule)
	}
	if ex.Unit != "timestamp-s" {
		t.Fatalf("unit = %q", ex.Unit)
	}
	if ex.Family != "timestamp_decoding" {
		t.Fatalf("family = %q", ex.Family)
	}
}

// TestExplainConstEval (#1998): a computed constant right-hand side reports
// the evaluation plus the unit its name supplied.
func TestExplainConstEval(t *testing.T) {
	numhint.SetFieldUnits(nil)
	line := "MAX_UPLOAD_SIZE = 10 * 1024 * 1024"
	start, end := spanOf(t, line, "10 * 1024 * 1024")
	ex, _ := Explain(Request{Line: line, Start: start, End: end, Capture: numhint.SizeCapture, Display: "10 MiB", Lang: "python"})
	if ex.Source != SourceConstEval {
		t.Fatalf("source = %v, want SourceConstEval", ex.Source)
	}
	if !strings.Contains(ex.Rule, "10485760") {
		t.Fatalf("rule does not name the computed value: %q", ex.Rule)
	}
	if ex.Unit != "bytes" {
		t.Fatalf("unit = %q", ex.Unit)
	}
}

// TestExplainSecretBuiltin (#1998): a masked value names the built-in pattern
// that matched its key.
func TestExplainSecretBuiltin(t *testing.T) {
	secret.SetKeyPatterns(nil)
	line := "DB_PASSWORD=hunter2"
	start, end := spanOf(t, line, "hunter2")
	ex, _ := Explain(Request{Line: line, Start: start, End: end, Capture: secret.Capture, Display: secret.Mask})
	if ex.Kind != KindSecret || !ex.Masked {
		t.Fatalf("kind/masked = %v/%v", ex.Kind, ex.Masked)
	}
	if ex.Source != SourceSecretBuiltin || !strings.Contains(ex.Rule, "PASSWORD") {
		t.Fatalf("source/rule = %v/%q", ex.Source, ex.Rule)
	}
	if ex.Key != "DB_PASSWORD" || ex.Raw != "hunter2" {
		t.Fatalf("key/raw = %q/%q", ex.Key, ex.Raw)
	}
}

// TestExplainSecretUserPattern (#1998): a configured pattern is named as the
// rule, ahead of the built-in tables.
func TestExplainSecretUserPattern(t *testing.T) {
	secret.SetKeyPatterns([]string{"*_license"})
	defer secret.SetKeyPatterns(nil)
	line := "ACME_LICENSE=abc-123"
	start, end := spanOf(t, line, "abc-123")
	ex, _ := Explain(Request{Line: line, Start: start, End: end, Capture: secret.Capture, Display: secret.Mask})
	if ex.Source != SourceSecretPattern || !strings.Contains(ex.Rule, "*_license") {
		t.Fatalf("source/rule = %v/%q", ex.Source, ex.Rule)
	}
}

// TestExplainUnmaskedSuspectKey (#1998): with no conceal span at all, a
// credential-shaped key still explains — the "why is this not masked" half.
func TestExplainUnmaskedSuspectKey(t *testing.T) {
	secret.SetKeyPatterns(nil)
	line := "GITHUB_TOKEN_URL=https://example.test"
	ex, ok := Explain(Request{Line: line, Col: len("GITHUB_TOKEN_URL=") + 2})
	if !ok {
		t.Fatal("no explanation")
	}
	if ex.Kind != KindSecret || ex.Masked {
		t.Fatalf("kind/masked = %v/%v", ex.Kind, ex.Masked)
	}
	if ex.Source != SourceSecretPublic || !strings.Contains(ex.Rule, "TOKEN_URL") {
		t.Fatalf("source/rule = %v/%q", ex.Source, ex.Rule)
	}
}

// TestExplainNoRule (#1998): a plain number nothing claimed says so, rather
// than inventing a rule.
func TestExplainNoRule(t *testing.T) {
	numhint.SetFieldUnits(nil)
	line := "retries = 3"
	ex, ok := Explain(Request{Line: line, Col: len("retries = ")})
	if !ok {
		t.Fatal("no explanation")
	}
	if ex.Source != SourceNone || ex.Unit != "" {
		t.Fatalf("source/unit = %v/%q", ex.Source, ex.Unit)
	}
	if !strings.Contains(ex.Rule, "no rule matches") {
		t.Fatalf("rule = %q", ex.Rule)
	}
	if ex.Key != "retries" || ex.Raw != "3" {
		t.Fatalf("key/raw = %q/%q", ex.Key, ex.Raw)
	}
}

// TestExplainEmptyLine (#1998): nothing to explain reports false rather than
// an empty explanation.
func TestExplainEmptyLine(t *testing.T) {
	if _, ok := Explain(Request{Line: "   ", Col: 1}); ok {
		t.Fatal("blank line explained")
	}
}

// TestEntries (#1998): the entries written into the existing stores are in the
// format those stores parse.
func TestEntries(t *testing.T) {
	if got := UnitEntry(" Max_Size ", "bytes"); got != "max_size=bytes" {
		t.Fatalf("unit entry = %q", got)
	}
	if got := SecretExemptEntry("PUBLIC_TOKEN"); got != "-PUBLIC_TOKEN" {
		t.Fatalf("exempt entry = %q", got)
	}
	if u, ok := numhint.ParseUnit("timestamp-ms"); !ok || numhint.UnitName(u) != "timestamp-ms" {
		t.Fatalf("unit round trip failed: %v %v", u, ok)
	}
	for _, c := range Choices() {
		u, ok := numhint.ParseUnit(c.Unit)
		if !ok {
			t.Fatalf("choice %q is not a mapping unit", c.Unit)
		}
		if name := numhint.UnitName(u); name != c.Unit {
			t.Fatalf("choice %q round-trips to %q", c.Unit, name)
		}
	}
}

// TestReclassifiedEntryOverridesHeuristic (#1998): the entry the popover
// writes is one the mapping store parses, and once installed it decides the
// value's reading over the heuristic that was there before — which is what
// "reopening the file applies it" comes down to.
func TestReclassifiedEntryOverridesHeuristic(t *testing.T) {
	numhint.SetFieldUnits(nil)
	defer numhint.SetFieldUnits(nil)
	line := "created_at: 1722945600"
	start, end := spanOf(t, line, "1722945600")

	// Before: the digits are in the epoch range, so nothing but the timestamp
	// producer claims them.
	before, _ := Explain(Request{Line: line, Start: start, End: end, Capture: epochtime.Capture})
	if before.Source != SourceEpochRange {
		t.Fatalf("source before = %v, want SourceEpochRange", before.Source)
	}

	// The popover's "reclassify as bytes" writes this entry.
	entry := UnitEntry(before.Key, "bytes")
	if entry != "created_at=bytes" {
		t.Fatalf("entry = %q", entry)
	}
	numhint.SetFieldUnits([]string{entry})

	hints := numhint.LineHints(0, line)
	if len(hints) != 1 || hints[0].Span.Capture != numhint.SizeCapture {
		t.Fatalf("hints after the rule: %+v", hints)
	}
	after, _ := Explain(Request{Line: line, Start: start, End: end, Capture: numhint.SizeCapture, Display: hints[0].Span.Replace})
	if after.Source != SourceFieldRule || after.Unit != "bytes" {
		t.Fatalf("source/unit after = %v/%q", after.Source, after.Unit)
	}
	if !strings.Contains(after.Rule, entry) {
		t.Fatalf("rule does not name the new entry: %q", after.Rule)
	}
}

// TestReclassifiedSecretEntryOverridesTables (#1998): the same for the secret
// store — an exempting entry made in the popover beats the built-in tables.
func TestReclassifiedSecretEntryOverridesTables(t *testing.T) {
	secret.SetKeyPatterns(nil)
	defer secret.SetKeyPatterns(nil)
	if !secret.Suspect("SESSION_TOKEN") {
		t.Fatal("SESSION_TOKEN must be masked by the built-in tables")
	}
	secret.SetKeyPatterns([]string{SecretExemptEntry("SESSION_TOKEN")})
	if secret.Suspect("SESSION_TOKEN") {
		t.Fatal("the exempting entry did not take effect")
	}
	line := "SESSION_TOKEN=abc"
	start, end := spanOf(t, line, "abc")
	ex, _ := Explain(Request{Line: line, Start: start, End: end})
	if ex.Source != SourceSecretExempt || ex.Masked {
		t.Fatalf("source/masked = %v/%v", ex.Source, ex.Masked)
	}
}

// TestEntryPattern (#1998): the pattern half is what a rule replaces on, in
// either store and in either polarity.
func TestEntryPattern(t *testing.T) {
	for entry, want := range map[string]string{
		"*_BYTES=bytes": "*_bytes",
		"  size = ms":   "size",
		"-PUBLIC_TOKEN": "public_token",
		"!x":            "x",
		"+db_pass*":     "db_pass*",
	} {
		if got := EntryPattern(entry); got != want {
			t.Fatalf("EntryPattern(%q) = %q, want %q", entry, got, want)
		}
	}
}
