package langini

import (
	"testing"

	"ike/internal/lang"
	"ike/internal/numhint"
)

// TestCommentUnitOverride (#2816): a ini unit comment — trailing, or on the
// line directly above — decides the reading of the line's literal.
func TestCommentUnitOverride(t *testing.T) {
	numhint.SetFieldUnits(nil)
	l, ok := lang.ByID("ini")
	if !ok || l.Spans == nil {
		t.Fatal("ini: no Spans producer registered")
	}
	var got []string
	for _, s := range l.Spans([]string{
		"timeout = 500 ; seconds",
		"; seconds",
		"timeout2 = 500",
		"# seconds",
		"timeout3 = 500",
		"",
		"timeout4 = 90000",
	}) {
		if s.Capture == numhint.DurationCapture {
			got = append(got, s.Replace)
		}
	}
	want := []string{"8m20s", "8m20s", "8m20s", "1m30s"}
	if len(got) != len(want) {
		t.Fatalf("durations = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("duration %d = %q, want %q", i, got[i], want[i])
		}
	}
}
