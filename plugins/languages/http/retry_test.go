package langhttp

import "testing"

// retry_test.go covers highlighting and completion of the `# @retry`
// directive (#3116). The spans are Go-computed, so they assert without the
// grammar (and therefore without cgo).

func TestRetryDirectiveHighlighted(t *testing.T) {
	lines := []string{
		//           1         2
		// 01234567890123456789012345
		`# @retry 3 2s on 5xx,error`,
		`GET https://example.com/flaky`,
	}
	for _, c := range []struct {
		col  int
		want string
	}{
		{3, "keyword"},   // @retry
		{9, "number"},    // 3
		{11, "number"},   // 2s
		{14, "keyword"},  // on
		{17, "constant"}, // 5xx
		{21, "constant"}, // error
	} {
		if got := spanCaptureAt(t, lines, 0, c.col); got != c.want {
			t.Errorf("col %d = %q, want %q", c.col, got, c.want)
		}
	}
}

// A rejected condition keeps the parts before it painted; the word the
// parser stumbled over is not presented as a condition.
func TestRetryDirectiveBadConditionNotPainted(t *testing.T) {
	lines := []string{
		// 0123456789012345678901
		`# @retry 3 on 503,soon`,
	}
	if got := spanCaptureAt(t, lines, 0, 9); got != "number" {
		t.Errorf("count = %q, want number", got)
	}
	if got := spanCaptureAt(t, lines, 0, 14); got != "constant" {
		t.Errorf("503 = %q, want constant", got)
	}
	if got := spanCaptureAt(t, lines, 0, 19); got == "constant" {
		t.Errorf("a rejected word must not read as a condition")
	}
	// A rejected delay is not a number either.
	if got := spanCaptureAt(t, []string{`# @retry 3 soon`}, 0, 12); got == "number" {
		t.Errorf("a rejected delay must not read as a number")
	}
}

func TestRetryDirectiveCompletes(t *testing.T) {
	items := completeAt(t, "### x\n# @|\nGET https://example.com\n")
	if !has(items, "@retry") {
		t.Fatalf("items = %v, want @retry", labels(items))
	}
	if got := insertFor(items, "@retry"); got != "@retry " {
		t.Errorf("insert = %q, want the marker plus a space for the count", got)
	}
	items = completeAt(t, "### x\n# @re|\nGET https://example.com\n")
	if !has(items, "@retry") || has(items, "@assert") {
		t.Errorf("typed prefix must filter: %v", labels(items))
	}
}
