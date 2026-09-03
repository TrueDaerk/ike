package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// report_test.go pins the read-back aggregation (#2426) against synthetic
// logs: the two shapes a session file can have (v4 with project.leave, v3
// without), the idle-gap rule, project switching back and forth, and the
// token→name join.

// day is the reference day every synthetic log is written on, in local time —
// the aggregation buckets by the local calendar day, so the fixtures must be
// local too.
var day = time.Date(2026, 9, 3, 9, 0, 0, 0, time.Local)

// at returns the reference day shifted by d.
func at(d time.Duration) time.Time { return day.Add(d) }

// ev builds one event line at t.
func ev(t time.Time, typ string, data map[string]string) Event {
	return Event{V: SchemaVersion, TS: t.UTC().Format("2006-01-02T15:04:05.000Z07:00"), SID: "test", Type: typ, Data: data}
}

// session is the marker that opens a project span.
func session(t time.Time, token string) Event {
	return ev(t, TypeSession, map[string]string{"app": "0.0.0", "os": "test", "project": token})
}

// leave is the v4 project.leave closing a span with an explicit active time.
func leave(t time.Time, token string, active time.Duration) Event {
	return ev(t, TypeProjectLeave, map[string]string{
		"project": token, "reason": "switch",
		"ms": strconv.FormatInt(active.Milliseconds(), 10),
	})
}

// cmd is a command dispatch, which counts as activity and as usage.
func cmd(t time.Time, id string) Event {
	return ev(t, TypeCommand, map[string]string{"id": id, "source": SourceKeybind})
}

// key is a chord resolution, which counts as activity only.
func key(t time.Time, chord string) Event {
	return ev(t, TypeKey, map[string]string{"chord": chord, "status": "resolved"})
}

// writeLog writes one synthetic session file into dir.
func writeLog(t *testing.T, dir, name string, events ...Event) string {
	t.Helper()
	var buf []byte
	for _, e := range events {
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// read aggregates dir and returns the whole-day range for the reference day.
func read(t *testing.T, dir string) *Report {
	t.Helper()
	return NewReader(dir).Read()
}

// today ranges the reference day only.
func today(r *Report) []Summary { return r.Range(day, day) }

func TestReportV4UsesRecordedActiveTime(t *testing.T) {
	dir := t.TempDir()
	// The span is two hours of wall clock, but the recorder reports 25
	// minutes of foreground time — the window was in the background for the
	// rest, and only the recorded number knows that.
	writeLog(t, dir, "a.jsonl",
		session(at(0), "aaa"),
		cmd(at(time.Minute), "editor.save"),
		leave(at(2*time.Hour), "aaa", 25*time.Minute),
	)
	rows := today(read(t, dir))
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Active != 25*time.Minute {
		t.Errorf("active = %v, want 25m", rows[0].Active)
	}
	if rows[0].Sessions != 1 {
		t.Errorf("sessions = %d, want 1", rows[0].Sessions)
	}
}

func TestReportV3SingleSessionCountsWallTime(t *testing.T) {
	dir := t.TempDir()
	// No project.leave: the span is measured from its own marks. Every gap
	// here is a minute, well under IdleGap, so all four minutes count.
	writeLog(t, dir, "a.jsonl",
		session(at(0), "aaa"),
		key(at(time.Minute), "cmd+s"),
		cmd(at(2*time.Minute), "editor.save"),
		key(at(3*time.Minute), "cmd+p"),
		cmd(at(4*time.Minute), "palette.open"),
	)
	rows := today(read(t, dir))
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Active != 4*time.Minute {
		t.Errorf("active = %v, want 4m", rows[0].Active)
	}
}

func TestReportV3DropsIdleGaps(t *testing.T) {
	dir := t.TempDir()
	// Two minutes of work, a two-hour lunch with nothing but heartbeats, two
	// more minutes of work: the lunch must not count.
	writeLog(t, dir, "a.jsonl",
		session(at(0), "aaa"),
		cmd(at(time.Minute), "editor.save"),
		cmd(at(2*time.Minute), "editor.save"),
		ev(at(time.Hour), TypeHeartbeat, map[string]string{"passes": "1"}),
		cmd(at(2*time.Hour), "editor.save"),
		cmd(at(2*time.Hour+time.Minute), "editor.save"),
	)
	rows := today(read(t, dir))
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Active != 3*time.Minute {
		t.Errorf("active = %v, want 3m (2m before the gap, 1m after)", rows[0].Active)
	}
}

func TestReportV3IdleGapBoundaryCounts(t *testing.T) {
	dir := t.TempDir()
	// Exactly IdleGap still counts; a hair more does not.
	writeLog(t, dir, "a.jsonl",
		session(at(0), "aaa"),
		cmd(at(IdleGap), "editor.save"),
		cmd(at(IdleGap+IdleGap+time.Second), "editor.save"),
	)
	rows := today(read(t, dir))
	if rows[0].Active != IdleGap {
		t.Errorf("active = %v, want %v", rows[0].Active, IdleGap)
	}
}

func TestReportPingPongSwitchesSplitPerProject(t *testing.T) {
	dir := t.TempDir()
	// A → B → A, each closed by its own project.leave: three sessions, two
	// projects, and the two A spans add up.
	writeLog(t, dir, "a.jsonl",
		session(at(0), "aaa"),
		cmd(at(time.Minute), "editor.save"),
		leave(at(10*time.Minute), "aaa", 10*time.Minute),
		session(at(10*time.Minute), "bbb"),
		cmd(at(11*time.Minute), "editor.save"),
		leave(at(30*time.Minute), "bbb", 20*time.Minute),
		session(at(30*time.Minute), "aaa"),
		cmd(at(31*time.Minute), "editor.save"),
		leave(at(35*time.Minute), "aaa", 5*time.Minute),
	)
	rep := read(t, dir)
	rep.Resolve(map[string]string{"/a": "A", "/b": "B"})
	// Resolve hashes paths; the fixture tokens are not real hashes, so both
	// rows fall into the unknown bucket. Read the raw per-token aggregate.
	if got := rep.Projects["aaa"].Days[day.Format(DayFormat)]; got.Active != 15*time.Minute {
		t.Errorf("A active = %v, want 15m", got.Active)
	} else if got.Sessions != 2 {
		t.Errorf("A sessions = %d, want 2", got.Sessions)
	}
	if got := rep.Projects["bbb"].Days[day.Format(DayFormat)]; got.Active != 20*time.Minute {
		t.Errorf("B active = %v, want 20m", got.Active)
	}
}

func TestReportResolvesNamesAndGroupsUnknown(t *testing.T) {
	dir := t.TempDir()
	known := ProjectToken("/home/dev/ike")
	writeLog(t, dir, "a.jsonl",
		session(at(0), known),
		cmd(at(time.Minute), "editor.save"),
		leave(at(time.Hour), known, time.Hour),
		session(at(time.Hour), "deadbeef01"),
		cmd(at(time.Hour+time.Minute), "editor.save"),
		leave(at(2*time.Hour), "deadbeef01", 10*time.Minute),
		session(at(2*time.Hour), "deadbeef02"),
		cmd(at(2*time.Hour+time.Minute), "editor.save"),
		leave(at(3*time.Hour), "deadbeef02", 5*time.Minute),
	)
	rep := read(t, dir)
	rep.Resolve(map[string]string{"/home/dev/ike": "ike"})
	rows := today(rep)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (ike + the grouped unknowns)", len(rows))
	}
	if rows[0].Name != "ike" || rows[0].Active != time.Hour {
		t.Errorf("row 0 = %q %v, want ike 1h", rows[0].Name, rows[0].Active)
	}
	if rows[1].Name != UnknownName {
		t.Errorf("row 1 name = %q, want %q", rows[1].Name, UnknownName)
	}
	if rows[1].Active != 15*time.Minute {
		t.Errorf("unknown active = %v, want 15m (both tokens grouped)", rows[1].Active)
	}
}

func TestReportTopCommandsAndDays(t *testing.T) {
	dir := t.TempDir()
	events := []Event{session(at(0), "aaa")}
	for i := 0; i < 3; i++ {
		events = append(events, cmd(at(time.Duration(i)*time.Minute), "editor.save"))
	}
	events = append(events, cmd(at(4*time.Minute), "palette.open"))
	events = append(events, leave(at(5*time.Minute), "aaa", 5*time.Minute))
	writeLog(t, dir, "a.jsonl", events...)

	rows := today(read(t, dir))
	if len(rows[0].Commands) != 2 {
		t.Fatalf("commands = %v", rows[0].Commands)
	}
	if rows[0].Commands[0].ID != "editor.save" || rows[0].Commands[0].N != 3 {
		t.Errorf("top command = %+v, want editor.save×3", rows[0].Commands[0])
	}
	if len(rows[0].Days) != 1 || rows[0].Days[0].Day != day.Format(DayFormat) {
		t.Errorf("days = %+v, want just the reference day", rows[0].Days)
	}
}

func TestReportWeekRangeFillsEmptyDays(t *testing.T) {
	dir := t.TempDir()
	old := day.AddDate(0, 0, -3)
	writeLog(t, dir, "a.jsonl",
		session(old, "aaa"),
		cmd(old.Add(time.Minute), "editor.save"),
		leave(old.Add(time.Hour), "aaa", time.Hour),
	)
	rep := read(t, dir)
	// Today alone finds nothing; the week finds the older day and pads the
	// range so the bar chart has one entry per day.
	if rows := today(rep); len(rows) != 0 {
		t.Fatalf("today rows = %d, want 0", len(rows))
	}
	rows := rep.Range(day.AddDate(0, 0, -6), day)
	if len(rows) != 1 {
		t.Fatalf("week rows = %d, want 1", len(rows))
	}
	if len(rows[0].Days) != 7 {
		t.Fatalf("days = %d, want 7", len(rows[0].Days))
	}
	var nonEmpty int
	for _, d := range rows[0].Days {
		if d.Active > 0 {
			nonEmpty++
		}
	}
	if nonEmpty != 1 {
		t.Errorf("non-empty days = %d, want 1", nonEmpty)
	}
}

func TestReportSpanWithoutLeaveClosesAtFileEnd(t *testing.T) {
	dir := t.TempDir()
	// The live session's file: a marker, some work, no leave yet.
	writeLog(t, dir, "a.jsonl",
		session(at(0), "aaa"),
		cmd(at(time.Minute), "editor.save"),
		cmd(at(2*time.Minute), "editor.save"),
	)
	rows := today(read(t, dir))
	if rows[0].Active != 2*time.Minute {
		t.Errorf("active = %v, want 2m", rows[0].Active)
	}
}

func TestReportSkipsGarbageLines(t *testing.T) {
	dir := t.TempDir()
	path := writeLog(t, dir, "a.jsonl",
		session(at(0), "aaa"),
		cmd(at(time.Minute), "editor.save"),
	)
	// A torn last line is the normal shape of a live session file.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"v":4,"ts":"2026-09-0`)
	f.Close()

	rows := today(read(t, dir))
	if len(rows) != 1 || rows[0].Active != time.Minute {
		t.Fatalf("rows = %+v, want one row of 1m", rows)
	}
}

func TestReportIgnoresNonJSONLFiles(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "a.jsonl", session(at(0), "aaa"), cmd(at(time.Minute), "editor.save"))
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir).Files; got != 1 {
		t.Errorf("files = %d, want 1", got)
	}
}

func TestReaderCachesByMtimeAndEvicts(t *testing.T) {
	dir := t.TempDir()
	path := writeLog(t, dir, "a.jsonl", session(at(0), "aaa"), cmd(at(time.Minute), "editor.save"))
	r := NewReader(dir)
	first := r.Read()
	if first.Files != 1 {
		t.Fatalf("files = %d", first.Files)
	}
	// Corrupt the file's *content* without changing size or mtime: a cached
	// read must not notice, which is exactly what proves the cache is used.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, info.Size()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	second := r.Read()
	if len(second.Projects) != 1 {
		t.Errorf("cached read lost the aggregate: %+v", second.Projects)
	}
	// A deleted file drops out of the cache with the report.
	os.Remove(path)
	third := r.Read()
	if third.Files != 0 || len(third.Projects) != 0 {
		t.Errorf("after delete: files=%d projects=%d, want 0/0", third.Files, len(third.Projects))
	}
	if len(r.cache) != 0 {
		t.Errorf("cache = %d entries, want 0", len(r.cache))
	}
}

func TestReaderEmptyDirAndNilSafe(t *testing.T) {
	if got := NewReader("").Read(); got.Files != 0 {
		t.Errorf("empty dir: files = %d", got.Files)
	}
	if got := NewReader(filepath.Join(t.TempDir(), "missing")).Read(); got.Files != 0 {
		t.Errorf("missing dir: files = %d", got.Files)
	}
	var nilRep *Report
	if got := nilRep.Range(day, day); got != nil {
		t.Errorf("nil report range = %v", got)
	}
	if got := nilRep.Name("x"); got != UnknownName {
		t.Errorf("nil report name = %q", got)
	}
}

func TestProjectTokenMatchesRecorderShape(t *testing.T) {
	tok := ProjectToken("/home/dev/ike")
	if len(tok) != 12 {
		t.Errorf("token = %q, want 12 hex chars", tok)
	}
	if ProjectToken("/home/dev/ike") != tok {
		t.Error("token is not stable")
	}
	if ProjectToken("/home/dev/other") == tok {
		t.Error("different roots share a token")
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "0s"},
		{-time.Second, "0s"},
		{42 * time.Second, "42s"},
		{90 * time.Second, "2m"},
		{47 * time.Minute, "47m"},
		{3*time.Hour + 12*time.Minute, "3h 12m"},
		{25 * time.Hour, "25h 0m"},
	}
	for _, c := range cases {
		if got := FormatDuration(c.in); got != c.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
