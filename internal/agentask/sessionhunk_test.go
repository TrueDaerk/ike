package agentask

import (
	"strconv"
	"strings"
	"testing"
)

// TestSessionHunk: without a feed link the context takes the hunk the
// transcript records (#2859), capped like the feed's.
func TestSessionHunk(t *testing.T) {
	s := session()
	tool := s.Events[3].Tool
	tool.Input = []byte(`{"file_path":"/proj/main.go","old_string":"c","new_string":"d"}`)
	tool.Result = []byte(`{"structuredPatch":[{"oldStart":3,"oldLines":1,"newStart":3,"newLines":1,"lines":["-c","+d"]}]}`)
	if got := SessionHunk(s, "e3/f0"); got != "@@ -3,1 +3,1 @@\n-c\n+d" {
		t.Fatalf("hunk = %q", got)
	}
	if got := SessionHunk(s, "e3"); !strings.Contains(got, "+d") {
		t.Fatalf("a single-file tool node shares the hunk, got %q", got)
	}
	if SessionHunk(s, "e9/f0") != "" {
		t.Fatal("an unknown key has no hunk")
	}
	var lines []string
	for i := 0; i < 2*MaxHunkLines; i++ {
		lines = append(lines, `"+l`+strconv.Itoa(i)+`"`)
	}
	tool.Result = []byte(`{"structuredPatch":[{"oldStart":1,"oldLines":0,"newStart":1,"newLines":120,"lines":[` + strings.Join(lines, ",") + `]}]}`)
	if got := strings.Split(SessionHunk(s, "e3/f0"), "\n"); len(got) != MaxHunkLines+1 || got[len(got)-1] != "…" {
		t.Fatalf("capped hunk = %d lines, last %q", len(got), got[len(got)-1])
	}
}
