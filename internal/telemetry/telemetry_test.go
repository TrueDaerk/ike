package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

// Internally triggered dispatches (source SourceInternal — polling/background
// funnels) must land as TypeInternal, while every user-triggered source lands
// as TypeCommand, so a "command" query never mixes the two (#2304).
func TestCommandTypeBySource(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	r.Command("lsp.documentSymbols", SourceInternal)
	r.Command("editor.save", SourceKeybind)
	r.Command("palette.searchEverywhere", SourcePalette)
	r.Command("view.toggleMinimap", SourceMenu)
	r.Command("pane.focus", SourceMouse)
	r.Close()

	evs := readSession(t, dir)
	if len(evs) != 5 {
		t.Fatalf("want 5 events, got %d", len(evs))
	}
	if evs[0].Type != TypeInternal || evs[0].Data["id"] != "lsp.documentSymbols" || evs[0].Data["source"] != SourceInternal {
		t.Errorf("internal dispatch wrong: %v", evs[0])
	}
	for _, ev := range evs[1:] {
		if ev.Type != TypeCommand {
			t.Errorf("user-triggered dispatch %v: type = %q, want %q", ev, ev.Type, TypeCommand)
		}
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
	r.Session("0.1.0", "darwin", "ab12cd34ef56")
	r.Op("http.flight", "ok", map[string]string{"ms": "120", "class": "2xx", "stream": "false"})
	r.Op("project.switch", "lsp", map[string]string{"ms": "0", "skipped": "no_server_docs"})
	r.CommandOutcome("editor.save", SourceKeybind, false, 0)
	r.PaletteDismiss("%", 4, 7, 900*time.Millisecond)
	r.ProjectLeave("ab12cd34ef56", "switch", time.Minute)
	r.Close()

	allowed := map[string]bool{
		"id": true, "source": true, // command
		"chord": true, "context": true, "command": true, "status": true, // key
		"op": true, "zone": true, "direction": true, // layout
		"app": true, "os": true, "project": true, // session (#2348)
		"passes": true,                                            // heartbeat (#2348)
		"top":    true,                                            // heartbeat (#2402) — Go message type names, never content
		"phase":  true, "ms": true, "class": true, "stream": true, // op (#2348)
		"ok":        true, // command outcome (#2408)
		"mode":      true, // palette.dismiss (#2408) — a prefix rune, never the query
		"query_len": true, // palette.dismiss (#2408) — the length, never the text
		"results":   true, // palette.dismiss (#2490) — a row count, never content
		"reason":    true, // project.leave (#2408)
		"skipped":   true, // project.switch lsp phase (#2492) — a reason token, never content
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

// TestPeriodicFlushWithoutExplicitFlush pins the freeze-fest guarantee: the
// writer goroutine puts enqueued events on disk on its own ticker, with no
// caller-driven Flush and no sleep — the fake ticker channel is the seam.
func TestPeriodicFlushWithoutExplicitFlush(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	tick := make(chan time.Time)
	r.newTicker = func(d time.Duration) (<-chan time.Time, func()) {
		return tick, func() {}
	}
	r.Command("x", SourceInternal)

	// The unbuffered send only rendezvous with the writer's receive; wait
	// for a subsequent ack so the tick's flush is guaranteed to have run
	// (the writer's select loop completes one case fully before the next).
	tick <- time.Now()
	r.Flush()

	evs := readSession(t, dir)
	if len(evs) != 1 || evs[0].Data["id"] != "x" {
		t.Fatalf("periodic tick did not flush the enqueued event: %v", evs)
	}
	r.Close()
}

// TestCloseStopsTicker pins that Close tears the ticker down — no busy
// ticker running after the writer goroutine has exited.
func TestCloseStopsTicker(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	var stopped atomic.Bool
	r.newTicker = func(d time.Duration) (<-chan time.Time, func()) {
		return make(chan time.Time), func() { stopped.Store(true) }
	}
	r.Command("x", SourceInternal)
	r.Close()

	if !stopped.Load() {
		t.Fatal("Close did not stop the periodic ticker")
	}
}

func TestDefaultFlushIntervalIsAFewSeconds(t *testing.T) {
	r := New(t.TempDir(), nil)
	if r.FlushInterval <= 0 || r.FlushInterval > 5*time.Second {
		t.Fatalf("FlushInterval = %v, want something in the 2-5s range", r.FlushInterval)
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

// A launch that only ever emits the synthetic startup pane.focus must leave
// no file at all — that ghost file was pure noise (#2318).
func TestLoneFocusEventCreatesNoFile(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	r.Layout("pane.focus", nil)
	r.Flush()
	r.Close()
	if files := sessionFiles(t, dir); len(files) != 0 {
		t.Fatalf("lone pane.focus created files: %v", files)
	}
}

// Deferred focus events are held, not dropped: the first meaningful event
// opens the file and the earlier focus changes land ahead of it, in order.
func TestDeferredFocusEventsLandOnFirstRealEvent(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	r.Layout("pane.focus", nil)
	r.Layout("pane.focus", nil)
	if files := sessionFiles(t, dir); len(files) != 0 {
		t.Fatalf("file created before the first meaningful event: %v", files)
	}
	r.Command("editor.save", SourceKeybind)
	r.Layout("pane.focus", nil)
	r.Close()

	evs := readSession(t, dir)
	if len(evs) != 4 {
		t.Fatalf("want 4 events, got %d: %v", len(evs), evs)
	}
	wantOps := []string{"pane.focus", "pane.focus", "", "pane.focus"}
	for i, want := range wantOps {
		if evs[i].Data["op"] != want {
			t.Errorf("event %d: op = %q, want %q", i, evs[i].Data["op"], want)
		}
	}
	if evs[2].Type != TypeCommand || evs[2].Data["id"] != "editor.save" {
		t.Errorf("meaningful event wrong: %v", evs[2])
	}
	for _, ev := range evs {
		if ev.SID != r.SessionID() {
			t.Errorf("event %v: sid %q != session %q", ev, ev.SID, r.SessionID())
		}
	}
}

// Holding focus events must stay bounded — a session that only switches
// panes for a long time may not grow the pending buffer without limit.
func TestPendingFocusEventsAreBounded(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	for i := 0; i < maxPending*3; i++ {
		r.Layout("pane.focus", nil)
	}
	r.Command("editor.save", SourceKeybind)
	r.Close()

	evs := readSession(t, dir)
	if len(evs) != maxPending+1 {
		t.Fatalf("want %d events (capped pending plus the real one), got %d", maxPending+1, len(evs))
	}
}

// The session marker (#2348) is deferred like pane.focus: alone it must not
// create a file (#2318), but it lands first once a meaningful event arrives.
func TestSessionMarkerDeferredUntilMeaningful(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	r.Session("0.1.0", "darwin", "ab12cd34ef56")
	r.Flush()
	if files := sessionFiles(t, dir); len(files) != 0 {
		t.Fatalf("lone session marker created files: %v", files)
	}
	r.Command("editor.save", SourceKeybind)
	r.Close()

	evs := readSession(t, dir)
	if len(evs) != 2 {
		t.Fatalf("want session + command, got %v", evs)
	}
	if evs[0].Type != TypeSession || evs[0].Data["app"] != "0.1.0" ||
		evs[0].Data["os"] != "darwin" || evs[0].Data["project"] != "ab12cd34ef56" {
		t.Fatalf("session marker wrong: %v", evs[0])
	}
	if evs[1].Type != TypeCommand {
		t.Fatalf("meaningful event wrong: %v", evs[1])
	}
}

// The session marker survives the pending trim (#2348): a long stretch of
// deferred focus events may not evict the attribution anchor.
func TestSessionMarkerSurvivesPendingTrim(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	r.Session("0.1.0", "linux", "ab12cd34ef56")
	for i := 0; i < maxPending*2; i++ {
		r.Layout("pane.focus", nil)
	}
	r.Command("editor.save", SourceKeybind)
	r.Close()

	evs := readSession(t, dir)
	if evs[0].Type != TypeSession {
		t.Fatalf("session marker evicted; first event: %v", evs[0])
	}
	if len(evs) != maxPending+1 {
		t.Fatalf("want %d events (bounded pending incl. marker, plus the real one), got %d", maxPending+1, len(evs))
	}
}

// The heartbeat (#2348) starts with the session file, records what the
// snapshot returns and skips nil snapshots — driven entirely through the
// fake-ticker seam, no sleeping.
func TestHeartbeatRecordsSnapshotPerTick(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	const hbInterval = 42 * time.Millisecond
	hbTick := make(chan time.Time)
	r.newTicker = func(d time.Duration) (<-chan time.Time, func()) {
		if d == hbInterval {
			return hbTick, func() {}
		}
		return make(chan time.Time), func() {} // writer's flush ticker: never fires
	}
	beat := 0
	r.SetHeartbeat(hbInterval, func() map[string]string {
		beat++
		if beat == 2 {
			return nil // an idle snapshot records nothing
		}
		return map[string]string{"passes": "7"}
	})
	r.Command("editor.save", SourceKeybind) // opens the file, spawns the heartbeat
	hbTick <- time.Now()
	hbTick <- time.Now() // the nil snapshot
	hbTick <- time.Now()
	// The third tick's send rendezvoused, but its record may still be in
	// flight; a fourth rendezvous proves the loop completed the third pass.
	hbTick <- time.Now()
	r.Close()

	var beats []Event
	for _, ev := range readSession(t, dir) {
		if ev.Type == TypeHeartbeat {
			beats = append(beats, ev)
		}
	}
	if len(beats) < 2 {
		t.Fatalf("want at least 2 heartbeat events, got %v", beats)
	}
	for _, b := range beats {
		if b.Data["passes"] != "7" {
			t.Fatalf("heartbeat payload wrong: %v", b)
		}
	}
}

// A heartbeat before the session file exists neither creates the file nor
// pollutes the pending buffer (#2348) — and Close ends the goroutine cleanly.
func TestHeartbeatNeverStartsASession(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	r.SetHeartbeat(time.Minute, func() map[string]string {
		return map[string]string{"passes": "1"}
	})
	// No meaningful event: the heartbeat goroutine never spawns, so a direct
	// record of its type must be dropped, not pended.
	r.record(TypeHeartbeat, map[string]string{"passes": "1"})
	r.Command("editor.save", SourceKeybind)
	r.Close()

	for _, ev := range readSession(t, dir) {
		if ev.Type == TypeHeartbeat {
			t.Fatalf("pre-session heartbeat leaked into the file: %v", ev)
		}
	}
}

// FlushSoon puts already-enqueued events on disk without blocking and without
// an explicit Flush/Close (#2348) — the guarantee that the events leading up
// to a long-running operation are on the platter before it starts.
func TestFlushSoonWritesWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	r.FlushInterval = time.Hour // the periodic flush must not be the one that saves us
	r.Op("http.flight", "start", nil)
	r.FlushSoon()

	deadline := time.Now().Add(5 * time.Second)
	for {
		files := sessionFiles(t, dir)
		if len(files) == 1 {
			raw, err := os.ReadFile(filepath.Join(dir, files[0]))
			if err == nil && strings.Contains(string(raw), "http.flight") {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("FlushSoon did not put the enqueued event on disk")
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.Close()
}

// Op events carry the operation id, the phase and the structural detail
// (#2348).
func TestOpEventShape(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	r.Op("http.flight", "start", nil)
	r.Op("http.flight", "ok", map[string]string{"ms": "120", "class": "2xx", "stream": "true"})
	r.Close()

	evs := readSession(t, dir)
	if len(evs) != 2 {
		t.Fatalf("want 2 op events, got %v", evs)
	}
	if evs[0].Type != TypeOp || evs[0].Data["id"] != "http.flight" || evs[0].Data["phase"] != "start" {
		t.Fatalf("start op wrong: %v", evs[0])
	}
	end := evs[1]
	if end.Data["phase"] != "ok" || end.Data["ms"] != "120" || end.Data["class"] != "2xx" || end.Data["stream"] != "true" {
		t.Fatalf("end op wrong: %v", end)
	}
}

// The retention pass drops zero-byte session files — litter from launches
// killed before the writer flushed anything (#2318).
func TestPruneDeletesEmptySessionFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "20250101T000000-aaa.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "20250102T000000-bbb.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := New(dir, nil)
	r.KeepFiles = 20
	r.Command("x", SourceInternal)
	r.Close()

	files := sessionFiles(t, dir)
	if len(files) != 2 {
		t.Fatalf("want the non-empty file plus this session's, got %v", files)
	}
	for _, f := range files {
		if f == "20250101T000000-aaa.jsonl" {
			t.Fatal("empty session file survived the prune")
		}
	}
}

// TestCommandOutcomeShapes is the #2408 acceptance criterion for the command
// event: a fast success keeps the v3 shape, a slow one and a failure both
// carry "ok" and "ms".
func TestCommandOutcomeShapes(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	r.CommandOutcome("editor.save", SourceKeybind, true, time.Millisecond)        // fast success
	r.CommandOutcome("project.switch", SourcePalette, true, 400*time.Millisecond) // slow success
	r.CommandOutcome("gone.command", SourceMenu, false, 0)                        // failed dispatch
	r.CommandOutcome("lsp.documentSymbols", SourceInternal, true, 2*time.Second)  // slow internal
	r.Close()

	evs := readSession(t, dir)
	if len(evs) != 4 {
		t.Fatalf("want 4 events, got %d: %v", len(evs), evs)
	}
	if _, ok := evs[0].Data["ok"]; ok {
		t.Errorf("a fast success must keep the plain shape: %v", evs[0])
	}
	if _, ok := evs[0].Data["ms"]; ok {
		t.Errorf("a fast success must not carry ms: %v", evs[0])
	}
	if evs[1].Data["ok"] != "true" || evs[1].Data["ms"] != "400" {
		t.Errorf("slow success: ok/ms = %q/%q, want true/400", evs[1].Data["ok"], evs[1].Data["ms"])
	}
	if evs[2].Data["ok"] != "false" || evs[2].Data["ms"] != "0" {
		t.Errorf("failed dispatch: ok/ms = %q/%q, want false/0", evs[2].Data["ok"], evs[2].Data["ms"])
	}
	if evs[3].Type != TypeInternal || evs[3].Data["ms"] != "2000" {
		t.Errorf("an internal dispatch keeps its own type and gains ms: %v", evs[3])
	}
}

// A dismissal is its own event type carrying the mode, the query *length*, the
// number of rows listed (#2490) and how long the box stood open (#2408).
func TestPaletteDismissEvent(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	r.PaletteDismiss("%", 4, 0, 1500*time.Millisecond)
	r.Close()

	evs := readSession(t, dir)
	if len(evs) != 1 || evs[0].Type != TypePaletteDismiss {
		t.Fatalf("want one %s event, got %v", TypePaletteDismiss, evs)
	}
	d := evs[0].Data
	if d["mode"] != "%" || d["query_len"] != "4" || d["ms"] != "1500" || d["results"] != "0" {
		t.Fatalf("payload = %v, want mode %%, query_len 4, results 0, ms 1500", d)
	}
}

// A dismissal off a matching query records the match count, so an export can
// tell it from the fruitless search above (#2490); a negative count clamps.
func TestPaletteDismissResults(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	r.PaletteDismiss(":", 3, 12, time.Second)
	r.PaletteDismiss("@", 0, -1, time.Second)
	r.Close()

	evs := readSession(t, dir)
	if len(evs) != 2 {
		t.Fatalf("want two events, got %v", evs)
	}
	if got := evs[0].Data["results"]; got != "12" {
		t.Errorf("results = %q, want 12", got)
	}
	if got := evs[1].Data["results"]; got != "0" {
		t.Errorf("negative results = %q, want 0", got)
	}
}

// project.leave carries the hashed token, the reason and the foreground time
// (#2408) — and, like the session marker, never opens a file on its own: a
// launch that only leaves again must stay a ghost (#2318).
func TestProjectLeaveEvent(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	r.ProjectLeave("ab12cd34ef56", "quit", 90*time.Second)
	r.Close()
	if files := sessionFiles(t, dir); len(files) != 0 {
		t.Fatalf("a lone project.leave opened a session file: %v", files)
	}

	dir = t.TempDir()
	r = New(dir, nil)
	r.Command("editor.save", SourceKeybind)
	r.ProjectLeave("ab12cd34ef56", "switch", 90*time.Second)
	r.Close()

	evs := readSession(t, dir)
	if len(evs) != 2 || evs[1].Type != TypeProjectLeave {
		t.Fatalf("want a %s event after the command, got %v", TypeProjectLeave, evs)
	}
	d := evs[1].Data
	if d["project"] != "ab12cd34ef56" || d["reason"] != "switch" || d["ms"] != "90000" {
		t.Fatalf("payload = %v, want the token, reason switch and ms 90000", d)
	}
}

// The version analysis scripts branch on (#2490).
func TestSchemaVersionIsSix(t *testing.T) {
	if SchemaVersion != 6 {
		t.Fatalf("SchemaVersion = %d, want 6", SchemaVersion)
	}
	dir := t.TempDir()
	r := New(dir, nil)
	r.Command("editor.save", SourceKeybind)
	r.Close()
	if evs := readSession(t, dir); len(evs) != 1 || evs[0].V != 6 {
		t.Fatalf("events must be stamped v6, got %v", evs)
	}
}
