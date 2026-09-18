package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// freezeEvents returns the `freeze` events of the single session file the
// recorder wrote under dir, plus the goroutine dumps that landed next to
// debug.log.
func freezeArtifacts(t *testing.T, dir string) (events []map[string]string, dumps []string) {
	t.Helper()
	tel := filepath.Join(dir, "telemetry")
	files, err := os.ReadDir(tel)
	if err != nil || len(files) != 1 {
		t.Fatalf("want one telemetry session file in %s, got %v (%v)", tel, files, err)
	}
	raw, err := os.ReadFile(filepath.Join(tel, files[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var ev struct {
			Type string            `json:"type"`
			Data map[string]string `json:"data"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("line %q is not valid JSON: %v", line, err)
		}
		if ev.Type == "freeze" {
			events = append(events, ev.Data)
		}
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "ike-freeze-") {
			dumps = append(dumps, e.Name())
		}
	}
	return events, dumps
}

// The heartbeat's freeze watch (#2627) turns a standing pass count into a
// `freeze` event plus one goroutine dump next to the project's debug.log —
// and a second frozen beat of the same episode adds an event, not a dump.
func TestHeartbeatRecordsFreezeAndDumps(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)

	r := newUsageRecorder()
	r.Command("editor.save", "keybind") // opens the session file
	w := newFreezeWatch(func() uint64 { return 4711 })
	snapshot := map[string]string{"passes": "4711"}
	recordFreeze(r, w, snapshot) // priming beat: nothing yet
	recordFreeze(r, w, snapshot) // frozen: event + dump
	w.WaitDumps()
	recordFreeze(r, w, snapshot) // same episode: event only
	w.WaitDumps()
	r.Close()

	events, dumps := freezeArtifacts(t, dir)
	if len(events) != 2 {
		t.Fatalf("want 2 freeze events, got %v", events)
	}
	if events[0]["passes"] != "0" || events[0]["dumped"] != "true" || events[0]["since_ms"] == "" {
		t.Errorf("first freeze event wrong: %v", events[0])
	}
	if events[1]["dumped"] != "false" {
		t.Errorf("follow-up freeze event must not claim a dump: %v", events[1])
	}
	if len(dumps) != 1 {
		t.Fatalf("want exactly one goroutine dump next to debug.log, got %v", dumps)
	}
	body, err := os.ReadFile(filepath.Join(dir, dumps[0]))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "passes=4711") || !strings.Contains(string(body), "goroutine ") {
		t.Errorf("dump misses the heartbeat snapshot or the stacks: %.300q", body)
	}
	log, err := os.ReadFile(filepath.Join(dir, "debug.log"))
	if err != nil {
		t.Fatalf("no debug.log line pointing at the dump: %v", err)
	}
	if !strings.Contains(string(log), "goroutine dump: ") {
		t.Errorf("debug.log misses the dump pointer: %q", log)
	}
}

// A loop that keeps completing passes produces neither event nor dump.
func TestHeartbeatSilentWhileTheLoopRuns(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)

	r := newUsageRecorder()
	r.Command("editor.save", "keybind")
	var passes uint64
	w := newFreezeWatch(func() uint64 { return passes })
	for i := 0; i < 4; i++ {
		passes += 500
		recordFreeze(r, w, map[string]string{"passes": "500"})
	}
	w.WaitDumps()
	r.Close()

	events, dumps := freezeArtifacts(t, dir)
	if len(events) != 0 || len(dumps) != 0 {
		t.Fatalf("live loop produced events %v / dumps %v", events, dumps)
	}
	if _, err := os.Stat(filepath.Join(dir, "debug.log")); !os.IsNotExist(err) {
		t.Errorf("live loop wrote to debug.log")
	}
}
