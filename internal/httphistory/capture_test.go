package httphistory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// capture_test.go covers how captured values (#1993) travel with the stored
// response: through the on-disk shape and back out of Store.Captured.

func TestCapturedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	s.Append("api.http", "start", Entry{
		Time: time.Now(), Status: "200 OK", StatusCode: 200,
		Body:     []byte(`{"task":"n1:42"}`),
		Captured: map[string]string{"task": "n1:42"},
	})
	entries := s.List("api.http", "start")
	if len(entries) != 1 || entries[0].Captured["task"] != "n1:42" {
		t.Fatalf("entries: %+v", entries)
	}
	// Readable on disk like every other field (#1267) — a captured value is
	// something the user may want to look up in the history file itself.
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(files) != 1 {
		t.Fatalf("files: %v", files)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"captured"`) || !strings.Contains(string(data), "n1:42") {
		t.Errorf("history file does not carry the captured value:\n%s", data)
	}
}

// TestCapturedOmittedWhenEmpty: an entry that captured nothing writes no
// field, so files of requests without directives look exactly as before.
func TestCapturedOmittedWhenEmpty(t *testing.T) {
	data, err := json.Marshal(Entry{Status: "200 OK"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "captured") {
		t.Errorf("empty capture map written: %s", data)
	}
}

// TestStoreCapturedNewestWins: values come from the stored responses of the
// requests named, and a name captured twice reads the value of whichever
// response is newer — regardless of which request produced it.
func TestStoreCapturedNewestWins(t *testing.T) {
	s := New(t.TempDir())
	old := time.Now().Add(-time.Hour)
	s.Append("api.http", "start", Entry{Time: old, Captured: map[string]string{"task": "old", "node": "n1"}})
	s.Append("api.http", "restart", Entry{Time: time.Now(), Captured: map[string]string{"task": "new"}})
	// An entry without captures (a re-send, or a response from before #1993)
	// contributes nothing and shadows nothing, even though it is the newest.
	s.Append("api.http", "start", Entry{Time: time.Now().Add(time.Minute)})

	got := s.Captured("api.http", []string{"start", "restart"})
	if got["task"] != "new" || got["node"] != "n1" {
		t.Errorf("captured %v, want task=new node=n1", got)
	}
}

// TestStoreCapturedEmpty: nothing stored, nothing to say — and a source with
// no history at all must not invent a map.
func TestStoreCapturedEmpty(t *testing.T) {
	s := New(t.TempDir())
	if got := s.Captured("api.http", []string{"start"}); got != nil {
		t.Errorf("captured %v, want nil", got)
	}
}

// TestStoreCapturedPrunedWithTheResponse: a captured value lives exactly as
// long as the response it came from — once the entry is pruned
// (MaxPerRequest), its value is gone with it.
func TestStoreCapturedPrunedWithTheResponse(t *testing.T) {
	s := New(t.TempDir())
	base := time.Now().Add(-time.Hour)
	s.Append("api.http", "start", Entry{Time: base, Captured: map[string]string{"first": "gone"}})
	for i := 0; i < MaxPerRequest; i++ {
		s.Append("api.http", "start", Entry{Time: base.Add(time.Duration(i+1) * time.Minute)})
	}
	if got := s.Captured("api.http", []string{"start"}); got["first"] != "" {
		t.Errorf("captured %v, want the pruned value gone", got)
	}
}
