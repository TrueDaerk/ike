package langpython

import "testing"

// spans2345_test.go covers the cron hints #2345 added to the Python hook: a
// quoted schedule in a scheduler call carries its reading, shape-guarded so
// ordinary strings stay raw.
func TestPythonSpansCron(t *testing.T) {
	lines := []string{
		`scheduler.add_job(task, CronTrigger.from_crontab("*/5 * * * *"))`,
		`label = "just some words here now"`,
	}
	var cron bool
	for _, s := range pythonSpans(lines) {
		if s.Capture == "cron.hint" {
			if s.Line != 0 {
				t.Fatalf("cron hint on line %d, want only the schedule line", s.Line)
			}
			cron = true
		}
	}
	if !cron {
		t.Error("the quoted schedule must carry a cron hint")
	}
}
