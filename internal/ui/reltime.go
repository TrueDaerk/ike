package ui

import (
	"strconv"
	"time"
)

// RelTime renders how long ago t was, compact ("just now", "5m ago",
// "3h ago", "4d ago", "6w ago") — the last-opened badge of the recent
// projects picker (#842) and the Recent Files popup (#1113). The zero time
// (legacy entries without a timestamp) yields "". It lives in ui because both
// internal/project and internal/palette render it, and project already
// imports palette (so palette cannot import project).
func RelTime(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m ago"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h ago"
	case d < 14*24*time.Hour:
		return strconv.Itoa(int(d.Hours()/24)) + "d ago"
	default:
		return strconv.Itoa(int(d.Hours()/(24*7))) + "w ago"
	}
}
