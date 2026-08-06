package cronhint

import (
	"testing"

	"ike/internal/lang"
)

// want asserts that spans holds exactly one hint span with the given rune
// columns and stand-in text.
func want(t *testing.T, spans []lang.Span, line, start, end int, replace string) {
	t.Helper()
	if len(spans) != 1 {
		t.Fatalf("spans = %+v, want exactly one hint", spans)
	}
	s := spans[0]
	if s.Capture != Capture {
		t.Errorf("capture = %q, want %q", s.Capture, Capture)
	}
	if s.Line != line || s.StartCol != start || s.EndCol != end {
		t.Errorf("span = line %d [%d,%d), want line %d [%d,%d)", s.Line, s.StartCol, s.EndCol, line, start, end)
	}
	if s.Replace != replace {
		t.Errorf("replace = %q, want %q", s.Replace, replace)
	}
}

// TestCrontabSpans (#1624): the leading five fields of a job line carry the
// hint; the command, comments and environment assignments stay untouched.
func TestCrontabSpans(t *testing.T) {
	spans := CrontabSpans([]string{"30 4 * * 1-5 /usr/bin/backup.sh --full"})
	want(t, spans, 0, 0, 12, "30 4 * * 1-5"+Gap+"Mon-Fri 04:30")

	spans = CrontabSpans([]string{"  @daily\t/usr/bin/rotate"})
	want(t, spans, 0, 2, 8, "@daily"+Gap+"daily 00:00")

	for _, line := range []string{
		"# 0 3 * * 1 commented out",
		"SHELL=/bin/sh",
		"MAILTO=\"ops@example.com\"",
		"",
		"   ",
		"0 3 * * 1", // schedule without a command: an unfinished line
	} {
		if spans := CrontabSpans([]string{line}); len(spans) != 0 {
			t.Errorf("CrontabSpans(%q) = %+v, want none", line, spans)
		}
	}
}

// TestYAMLKeySpans (#1624): a `cron:`/`schedule:` key names its value a
// schedule, quoted or not — the GitHub Actions and GitLab CI shape.
func TestYAMLKeySpans(t *testing.T) {
	spans := YAMLSpans([]string{"    - cron: \"*/5 * * * *\""})
	want(t, spans, 0, 13, 24, "*/5 * * * *"+Gap+"every 5 min")

	spans = YAMLSpans([]string{"  schedule: 0 3 * * 1  # nightly"})
	want(t, spans, 0, 12, 21, "0 3 * * 1"+Gap+"Mon 03:00")

	spans = YAMLSpans([]string{"  cron: '0 0 1 * *'"})
	want(t, spans, 0, 9, 18, "0 0 1 * *"+Gap+"day 1 00:00")

	for _, line := range []string{
		"  cron: not-a-schedule",
		"  name: \"nightly build\"",
		"  # cron: 0 3 * * 1",
	} {
		if spans := YAMLSpans([]string{line}); len(spans) != 0 {
			t.Errorf("YAMLSpans(%q) = %+v, want none", line, spans)
		}
	}
}

// TestQuotedSpans (#1624): a quoted scalar in a config format hints only when
// it both parses and carries a cron-specific character, so a quoted list of
// numbers is never mistaken for a schedule.
func TestQuotedSpans(t *testing.T) {
	spans := QuotedSpans([]string{`  "schedule": "0 3 * * 1",`})
	want(t, spans, 0, 15, 24, "0 3 * * 1"+Gap+"Mon 03:00")

	spans = QuotedSpans([]string{`schedule = "@hourly"`})
	want(t, spans, 0, 12, 19, "@hourly"+Gap+"hourly :00")

	for _, line := range []string{
		`  "version": "1 2 3 4 5",`, // parses, but no cron character
		`  "note": "runs every five minutes",`,
		`  "range": "1-5",`,
		`  cron: 0 3 * * 1`, // no quotes: the YAML key path handles that one
	} {
		if spans := QuotedSpans([]string{line}); len(spans) != 0 {
			t.Errorf("QuotedSpans(%q) = %+v, want none", line, spans)
		}
	}
}

// TestYAMLQuotedFallback (#1624): a quoted cron expression under any other key
// still hints — CI configs are not the only place a schedule is written.
func TestYAMLQuotedFallback(t *testing.T) {
	spans := YAMLSpans([]string{"  interval: \"*/15 * * * *\""})
	want(t, spans, 0, 13, 25, "*/15 * * * *"+Gap+"every 15 min")
}
