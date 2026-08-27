package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readSession returns the parsed lines of the single session file in dir.
func readSession(t *testing.T, dir string) []Event {
	t.Helper()
	files := sessionFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("want exactly one session file, got %v", files)
	}
	raw, err := os.ReadFile(filepath.Join(dir, files[0]))
	if err != nil {
		t.Fatal(err)
	}
	var out []Event
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("line %q is not valid JSON: %v", line, err)
		}
		out = append(out, ev)
	}
	return out
}

func sessionFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestEventsLandAsJSONL(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	r.Command("editor.save", SourceKeybind)
	r.Key("cmd+k cmd+c", "editor[go]", "editor.commentLine", "resolved")
	r.Layout("split", map[string]string{"zone": "right"})
	r.Close()

	evs := readSession(t, dir)
	if len(evs) != 3 {
		t.Fatalf("want 3 events, got %d", len(evs))
	}
	for _, ev := range evs {
		if ev.V != SchemaVersion {
			t.Errorf("event %v: v = %d, want %d", ev, ev.V, SchemaVersion)
		}
		if ev.SID == "" || ev.TS == "" {
			t.Errorf("event %v: missing sid/ts", ev)
		}
		if ev.SID != r.SessionID() {
			t.Errorf("sid %q != recorder session %q", ev.SID, r.SessionID())
		}
	}
	if evs[0].Type != TypeCommand || evs[0].Data["id"] != "editor.save" || evs[0].Data["source"] != SourceKeybind {
		t.Errorf("command event wrong: %v", evs[0])
	}
	if evs[1].Type != TypeKey || evs[1].Data["chord"] != "cmd+k cmd+c" || evs[1].Data["context"] != "editor[go]" ||
		evs[1].Data["command"] != "editor.commentLine" || evs[1].Data["status"] != "resolved" {
		t.Errorf("key event wrong: %v", evs[1])
	}
	if evs[2].Type != TypeLayout || evs[2].Data["op"] != "split" || evs[2].Data["zone"] != "right" {
		t.Errorf("layout event wrong: %v", evs[2])
	}
}

// The schema is the interface for later analysis: only structural fields may
// appear. A new payload key must be added here deliberately.
func TestSchemaCarriesOnlyStructuralFields(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	r.Command("a.b", SourcePalette)
	r.Key("ctrl+x", "editor", "", "unbound")
	r.Layout("tab.switch", nil)
	r.Close()

	allowed := map[string]bool{
		"id": true, "source": true, // command
		"chord": true, "context": true, "command": true, "status": true, // key
		"op": true, "zone": true, "direction": true, // layout
	}
	for _, ev := range readSession(t, dir) {
		for k := range ev.Data {
			if !allowed[k] {
				t.Errorf("event %v carries unexpected payload key %q — content leak?", ev, k)
			}
		}
	}
}

func TestDisabledWritesNothing(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, func() bool { return false })
	r.Command("x", SourceInternal)
	r.Layout("split", nil)
	r.Close()
	if files := sessionFiles(t, dir); len(files) != 0 {
		t.Fatalf("disabled recorder wrote files: %v", files)
	}
}

func TestEnabledFlipAppliesToNextEvent(t *testing.T) {
	dir := t.TempDir()
	on := false
	r := New(dir, func() bool { return on })
	r.Command("dropped", SourceInternal)
	on = true
	r.Command("kept", SourceInternal)
	on = false
	r.Command("dropped2", SourceInternal)
	r.Close()

	evs := readSession(t, dir)
	if len(evs) != 1 || evs[0].Data["id"] != "kept" {
		t.Fatalf("want exactly the 'kept' event, got %v", evs)
	}
}

func TestNilRecorderIsInert(t *testing.T) {
	var r *Recorder
	r.Command("x", SourceInternal)
	r.Key("a", "", "", "unbound")
	r.Layout("split", nil)
	r.Flush()
	r.Close()
	if r.SessionID() != "" {
		t.Fatal("nil recorder minted a session id")
	}
}

func TestSizeCapStopsWriting(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	r.MaxBytes = 300
	for i := 0; i < 100; i++ {
		r.Command("some.command.id", SourceKeybind)
		r.Flush() // serialize so the cap check runs between events
	}
	r.Close()

	files := sessionFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("want one file, got %v", files)
	}
	st, err := os.Stat(filepath.Join(dir, files[0]))
	if err != nil {
		t.Fatal(err)
	}
	// One event may straddle the cap; anywhere near it is fine, 100 events
	// (~10KB) would not be.
	if st.Size() > 600 {
		t.Fatalf("cap ignored: file is %d bytes", st.Size())
	}
}

func TestPruneKeepsNewestSessions(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"20250101T000000-aaa.jsonl",
		"20250102T000000-bbb.jsonl",
		"20250103T000000-ccc.jsonl",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := New(dir, nil)
	r.KeepFiles = 3
	r.Command("x", SourceInternal)
	r.Close()

	files := sessionFiles(t, dir)
	if len(files) != 3 {
		t.Fatalf("want 3 files after prune, got %v", files)
	}
	for _, f := range files {
		if f == "20250101T000000-aaa.jsonl" {
			t.Fatal("oldest session survived the prune")
		}
	}
}

func TestEmptyDirYieldsInertRecorder(t *testing.T) {
	r := New("", nil)
	if r != nil {
		t.Fatal("empty dir should yield a nil recorder")
	}
}

func TestCloseIsIdempotentAndStopsRecording(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	r.Command("before", SourceInternal)
	r.Close()
	r.Command("after", SourceInternal) // must not panic or write
	r.Close()
	evs := readSession(t, dir)
	if len(evs) != 1 || evs[0].Data["id"] != "before" {
		t.Fatalf("want only the pre-close event, got %v", evs)
	}
}
