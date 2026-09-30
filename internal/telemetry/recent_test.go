package telemetry

import (
	"fmt"
	"strings"
	"testing"
)

// TestRecentRing (#2836): the recorder keeps the last recentEvents events in
// memory, oldest first, whether or not telemetry is enabled — the crash
// report's context.
func TestRecentRing(t *testing.T) {
	r := New(t.TempDir(), func() bool { return false })
	if got := r.Recent(); len(got) != 0 {
		t.Fatalf("fresh recorder Recent = %v", got)
	}
	r.Key("cmd+f", "editor[python]", "editor.find", "resolved")
	got := r.Recent()
	if len(got) != 1 || !strings.Contains(got[0], "key") || !strings.Contains(got[0], "chord=cmd+f") ||
		!strings.Contains(got[0], "command=editor.find") || !strings.Contains(got[0], "context=editor[python]") {
		t.Fatalf("Recent after one key = %v", got)
	}
	for i := 0; i < recentEvents+10; i++ {
		r.Command(fmt.Sprintf("cmd.%d", i), SourcePalette)
	}
	got = r.Recent()
	if len(got) != recentEvents {
		t.Fatalf("ring holds %d, want %d", len(got), recentEvents)
	}
	if !strings.Contains(got[0], "cmd.10") || !strings.Contains(got[len(got)-1], fmt.Sprintf("cmd.%d", recentEvents+9)) {
		t.Fatalf("ring order wrong: first=%q last=%q", got[0], got[len(got)-1])
	}
	var nilRec *Recorder
	if nilRec.Recent() != nil {
		t.Fatal("nil recorder must report nothing")
	}
}
