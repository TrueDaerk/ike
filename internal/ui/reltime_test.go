package ui

// reltime_test.go covers the relative-age renderers: RelTime's badge form and
// ShortAge, the narrow-column variant behind the explorer's Scratches
// "last opened" column (#1965).

import (
	"testing"
	"time"
)

func TestRelTimeBuckets(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{3 * time.Hour, "3h ago"},
		{7 * 24 * time.Hour, "7d ago"},
		{6 * 7 * 24 * time.Hour, "6w ago"},
	}
	for _, c := range cases {
		if got := RelTime(now.Add(-c.ago), now); got != c.want {
			t.Errorf("RelTime(-%v) = %q want %q", c.ago, got, c.want)
		}
	}
	if got := RelTime(time.Time{}, now); got != "" {
		t.Errorf("zero time = %q want empty", got)
	}
}

// TestShortAgeBuckets (#1965): the column form drops the " ago" suffix and
// spells sub-minute ages "now", so every value stays a few cells wide.
func TestShortAgeBuckets(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{0, "now"},
		{59 * time.Second, "now"},
		{5 * time.Minute, "5m"},
		{59 * time.Minute, "59m"},
		{3 * time.Hour, "3h"},
		{23 * time.Hour, "23h"},
		{7 * 24 * time.Hour, "7d"},
		{13 * 24 * time.Hour, "13d"},
		{6 * 7 * 24 * time.Hour, "6w"},
	}
	for _, c := range cases {
		if got := ShortAge(now.Add(-c.ago), now); got != c.want {
			t.Errorf("ShortAge(-%v) = %q want %q", c.ago, got, c.want)
		}
	}
	if got := ShortAge(time.Time{}, now); got != "" {
		t.Errorf("zero time = %q want empty", got)
	}
	// Clock skew must not render a negative age.
	if got := ShortAge(now.Add(time.Hour), now); got != "now" {
		t.Errorf("future time = %q want %q", got, "now")
	}
}
