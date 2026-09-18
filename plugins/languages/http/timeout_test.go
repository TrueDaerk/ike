package langhttp

import "testing"

// timeout_test.go covers the highlighting of the `# @timeout` directive
// (#2630). Like the capture spans these are Go-computed, so they assert
// without the grammar (and therefore without cgo).

// TestTimeoutDirectiveHighlighted: the marker reads as a keyword and the
// duration as a number, instead of the whole line disappearing into the
// comment colour.
func TestTimeoutDirectiveHighlighted(t *testing.T) {
	lines := []string{
		//        1
		// 0123456789012
		`# @timeout 45s`,
		`GET https://example.com/slow`,
	}
	if got := spanCaptureAt(t, lines, 0, 3); got != "keyword" {
		t.Errorf("@timeout marker = %q, want keyword", got)
	}
	if got := spanCaptureAt(t, lines, 0, 11); got != "number" {
		t.Errorf("duration = %q, want number", got)
	}
}

// A value that does not parse gets the marker painted and nothing else — the
// duration is not there to point at.
func TestTimeoutDirectiveBadValueKeepsMarkerOnly(t *testing.T) {
	lines := []string{`# @timeout soon`}
	if got := spanCaptureAt(t, lines, 0, 3); got != "keyword" {
		t.Errorf("@timeout marker = %q, want keyword", got)
	}
	if got := spanCaptureAt(t, lines, 0, 12); got == "number" {
		t.Errorf("a rejected value must not read as a duration: %q", got)
	}
}
