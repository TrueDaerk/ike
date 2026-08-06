package langcrontab

import (
	"testing"

	"ike/internal/cronhint"
	"ike/internal/lang"
)

// TestRegistered (#1624): the crontab language resolves by extension and by
// the conventional base names.
func TestRegistered(t *testing.T) {
	for _, path := range []string{"deploy.cron", "backup.crontab", "crontab", "/etc/crontab"} {
		l, ok := lang.ByPath(path)
		if !ok || l.ID != "crontab" {
			t.Errorf("ByPath(%q) = %q, %v; want crontab", path, l.ID, ok)
		}
	}
}

// TestSpans (#1624): a job line styles its schedule apart from its command and
// carries the schedule hint; comments and environment assignments do not.
func TestSpans(t *testing.T) {
	l, ok := lang.ByID("crontab")
	if !ok || l.Spans == nil {
		t.Fatal("crontab: no Spans producer registered")
	}
	spans := l.Spans([]string{
		"# nightly backup",
		"SHELL=/bin/sh",
		"30 4 * * 1-5 /usr/bin/backup.sh",
	})
	var hints, comments, commands int
	for _, s := range spans {
		switch s.Capture {
		case cronhint.Capture:
			hints++
			if want := "30 4 * * 1-5" + cronhint.Gap + "Mon-Fri 04:30"; s.Replace != want {
				t.Errorf("hint = %q, want %q", s.Replace, want)
			}
			if s.Line != 2 {
				t.Errorf("hint on line %d, want 2", s.Line)
			}
		case "comment":
			comments++
		case "string":
			commands++
		}
	}
	if hints != 1 || comments != 1 {
		t.Errorf("hints = %d, comments = %d; want 1 and 1", hints, comments)
	}
	// The assignment value and the job's command both style as strings.
	if commands != 2 {
		t.Errorf("string spans = %d, want 2", commands)
	}
}
