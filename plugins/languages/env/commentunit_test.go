package langenv

import (
	"testing"

	"ike/internal/lang"
	"ike/internal/numhint"
)

// TestCommentUnitOverride (#2816): a dotenv unit comment — trailing, or on the
// line directly above — decides the reading of the line's literal.
func TestCommentUnitOverride(t *testing.T) {
	numhint.SetFieldUnits(nil)
	l, ok := lang.ByID("dotenv")
	if !ok || l.Spans == nil {
		t.Fatal("dotenv: no Spans producer registered")
	}
	var got []string
	for _, s := range l.Spans([]string{
		"TIMEOUT=500 # seconds",
		"# seconds",
		"TIMEOUT2=500",
		"TIMEOUT3=90000",
	}) {
		if s.Capture == numhint.DurationCapture {
			got = append(got, s.Replace)
		}
	}
	want := []string{"8m20s", "8m20s", "1m30s"}
	if len(got) != len(want) {
		t.Fatalf("durations = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("duration %d = %q, want %q", i, got[i], want[i])
		}
	}
}
