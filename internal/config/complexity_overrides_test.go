package config

import (
	"strings"
	"testing"
)

// TestParseThresholdOverride is the grammar table of complexity.overrides
// (#2960): good entries parse with the keys they set, a bad one fails with
// a message naming the entry.
func TestParseThresholdOverride(t *testing.T) {
	u := ThresholdUnset
	good := []struct {
		entry string
		want  ThresholdOverride
	}{
		{"legacy/** ccn=30", ThresholdOverride{Glob: "legacy/**", CCN: 30, Cog: u, Cmt: u}},
		{"lang:php cog=25 cmt=0", ThresholdOverride{Lang: "php", CCN: u, Cog: 25, Cmt: 0}},
		{"*_test.go ccn=40 cog=40", ThresholdOverride{Glob: "*_test.go", CCN: 40, Cog: 40, Cmt: u}},
		{"  src/core/**   cmt=100 ccn=1 ", ThresholdOverride{Glob: "src/core/**", CCN: 1, Cog: u, Cmt: 100}},
	}
	for _, g := range good {
		got, err := ParseThresholdOverride(g.entry)
		if err != nil || got != g.want {
			t.Errorf("%q = %+v, %v; want %+v", g.entry, got, err, g.want)
		}
	}
	bad := []struct{ entry, why string }{
		{"ccn=0", "selector"},
		{"legacy/** ccn=0", "out of range"},
		{"legacy/** cog=201", "out of range"},
		{"legacy/** cmt=101", "out of range"},
		{"legacy/** ccn=x", "out of range"},
		{"foo", "no threshold"},
		{"lang: ccn=3", "language id"},
		{"legacy/** x=1", "unknown threshold"},
		{"legacy/** ccn", "unknown threshold"},
		{"legacy/** ccn=3 ccn=4", "twice"},
		{"src/[gen ccn=3", "invalid glob"},
		{"   ", "empty"},
	}
	for _, b := range bad {
		_, err := ParseThresholdOverride(b.entry)
		if err == nil {
			t.Errorf("%q: want an error", b.entry)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, b.why) || strings.TrimSpace(b.entry) != "" && !strings.Contains(msg, strings.TrimSpace(b.entry)) {
			t.Errorf("%q: error %q must name the entry and say %q", b.entry, msg, b.why)
		}
	}
}

// TestValidateDropsBadThresholdOverride: a loaded entry that does not parse
// is dropped with a diagnostic naming it; the good ones stay in order.
func TestValidateDropsBadThresholdOverride(t *testing.T) {
	c := defaults()
	c.Complexity.Overrides = []string{"legacy/** ccn=30", "foo", "lang:php cog=25"}
	diags := validate(c)
	if got := c.Complexity.Overrides; len(got) != 2 || got[0] != "legacy/** ccn=30" || got[1] != "lang:php cog=25" {
		t.Fatalf("overrides = %q, want the two good entries", got)
	}
	found := false
	for _, d := range diags {
		if d.Field == "complexity.overrides" && strings.Contains(d.Message, `"foo"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a complexity.overrides diagnostic naming \"foo\", got %+v", diags)
	}
	if os := c.Complexity.ThresholdOverrides(); len(os) != 2 || os[1].Lang != "php" {
		t.Fatalf("ThresholdOverrides = %+v", os)
	}
	if got, ok := Defaults()["complexity.overrides"]; !ok || got != "" {
		t.Fatalf("complexity.overrides defaults to %q, want empty", got)
	}
}
