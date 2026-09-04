package langgo

import "testing"

// spans2345_test.go covers the families #2345 added to the Go hook: secret
// masks on suspect assignments and cron hints on quoted schedules.
func TestGoSpansMaskAndCron(t *testing.T) {
	lines := []string{
		`password := "hunter2"`,
		`c.AddFunc("*/5 * * * *", task)`,
	}
	want := map[int]string{0: "secret.value", 1: "cron.hint"}
	found := map[int]bool{}
	for _, s := range goSpans(lines) {
		if want[s.Line] == s.Capture {
			found[s.Line] = true
		}
	}
	for li, capture := range want {
		if !found[li] {
			t.Errorf("line %d: no %q span produced", li, capture)
		}
	}
}
