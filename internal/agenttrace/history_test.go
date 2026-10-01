package agenttrace

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// history_test.go covers the session history of #2860: a record
// round-trips through the store so the tree and the path built from it
// match the live session's (rewinds and stored diffs included), the size
// cap trims diffs, the store prunes to its cap oldest first, and the import
// over a projects root stores the originals, skips forks and ask-tagged
// forks, reports a malformed file and is idempotent.

func TestRecordRoundTripsTreeAndPath(t *testing.T) {
	for _, name := range []string{"basic.jsonl", "rewind.jsonl"} {
		s := parseFixture(t, name)
		st := Store{Dir: t.TempDir()}
		r := NewRecord(s, "/t/"+s.ID+".jsonl", true)
		if err := st.Save(r); err != nil {
			t.Fatal(err)
		}
		back, err := st.Load(s.ID)
		if err != nil {
			t.Fatal(err)
		}
		if back.V != RecordVersion || back.ID != s.ID || back.CWD != s.CWD || !back.Ended || back.Transcript != "/t/"+s.ID+".jsonl" {
			t.Fatalf("%s: header = %+v", name, back)
		}
		got := back.Session()
		if a, b := strings.Join(outline(BuildTree(got)), "\n"), strings.Join(outline(BuildTree(s)), "\n"); a != b {
			t.Errorf("%s: tree differs:\n%s\nwant\n%s", name, a, b)
		}
		if a, b := strings.Join(stops(BuildPath(got)), "\n"), strings.Join(stops(BuildPath(s)), "\n"); a != b {
			t.Errorf("%s: path differs:\n%s\nwant\n%s", name, a, b)
		}
		if len(got.Rewinds) != len(s.Rewinds) || got.Turns() != s.Turns() {
			t.Errorf("%s: rewinds %d/%d turns %d/%d", name, len(got.Rewinds), len(s.Rewinds), got.Turns(), s.Turns())
		}
		// The stored diffs answer D like the reconstruction did.
		want := DiffsWith(s, nil)
		have := Diffs(got)
		if len(have) != len(want) {
			t.Fatalf("%s: diffs %d, want %d", name, len(have), len(want))
		}
		for i := range want {
			if have[i].Key != want[i].Key || have[i].Unified() != want[i].Unified() || have[i].Source != want[i].Source {
				t.Errorf("%s: diff %d = %+v, want %+v", name, i, have[i], want[i])
			}
		}
	}
}

func TestRecordSummaryColumns(t *testing.T) {
	s := parseFixture(t, "basic.jsonl")
	r := NewRecord(s, "", false)
	if r.Turns != 2 || r.FirstPrompt != "Add a greeting to main.go" || r.Ended {
		t.Fatalf("record = turns %d first %q ended %v", r.Turns, r.FirstPrompt, r.Ended)
	}
	// main.go (edit, multiedit), hello.go (create), the notebook (delete).
	if r.FilesChanged != 3 {
		t.Errorf("files changed = %d", r.FilesChanged)
	}
	// Tool inputs and outputs are not copied.
	for _, ev := range r.Events {
		if ev.Tool != nil && ev.Tool.Name == "Bash" && ev.Tool.Title != "Build and vet the module" {
			t.Errorf("Bash title = %q", ev.Tool.Title)
		}
	}
	data, err := r.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "undefined: fmt") || strings.Contains(string(data), `"input"`) {
		t.Error("the record must not carry tool outputs or inputs")
	}
}

func TestRecordEncodeTrimsDiffsToCap(t *testing.T) {
	s := parseFixture(t, "basic.jsonl")
	r := NewRecord(s, "", true)
	big := strings.Repeat("x", 1000)
	for i := 0; i < 700; i++ {
		r.Diffs = append(r.Diffs, RecordDiff{Key: "e99/f" + strconv.Itoa(i), Path: "big.go", Hunks: []DiffHunk{{Lines: []string{"+" + big}}}, Added: 1, Counted: true})
	}
	data, err := r.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > MaxRecordBytes || !r.Truncated {
		t.Fatalf("encoded %d bytes, truncated=%v", len(data), r.Truncated)
	}
	kept := 0
	for _, d := range r.Diffs {
		if d.Hunks != nil {
			kept++
		}
	}
	if kept == 0 || kept == len(r.Diffs) {
		t.Errorf("hunks kept on %d of %d diffs", kept, len(r.Diffs))
	}
	// The counts survive the cut: the boxes still show +N −M.
	if r.Diffs[len(r.Diffs)-1].Added != 1 {
		t.Error("counts must survive the hunk cut")
	}
}

func TestStorePrunesOldestFirst(t *testing.T) {
	st := Store{Dir: t.TempDir(), Max: 2}
	base := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	for i, id := range []string{"old", "mid", "new"} {
		r := &Record{V: RecordVersion, ID: id, StartedAt: base.Add(time.Duration(i) * time.Hour), EndedAt: base.Add(time.Duration(i)*time.Hour + 30*time.Minute)}
		if err := st.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	all, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].ID != "new" || all[1].ID != "mid" {
		t.Fatalf("list = %+v", all)
	}
	if _, err := os.Stat(st.Path("old")); !os.IsNotExist(err) {
		t.Error("the oldest record must be pruned")
	}
	if all[0].Duration() != 30*time.Minute {
		t.Errorf("duration = %v", all[0].Duration())
	}
	// A record from a newer IKE is refused, not misread.
	if err := os.WriteFile(st.Path("future"), []byte(`{"v":99,"id":"future"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Load("future"); err != ErrRecordVersion {
		t.Errorf("Load(future) err = %v", err)
	}
}

func TestImportStoresOriginalsSkipsForksReportsMalformed(t *testing.T) {
	projects := t.TempDir()
	cwd := "/Users/dev/src/proj"
	base := time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)
	fixture(t, projects, "basic.jsonl", cwd, origID, base)
	time.Sleep(20 * time.Millisecond)
	fixture(t, projects, "fork.jsonl", cwd, forkID, base.Add(5*time.Minute))
	time.Sleep(20 * time.Millisecond)
	asked := fixture(t, projects, "fork.jsonl", cwd, "44444444-4444-4444-8444-444444444444", base.Add(6*time.Minute))
	rewrite(t, asked, `"content":"Why did you add hello.go?"`, `"content":"`+AskMarker+`\nWhy did you add hello.go?"`)
	time.Sleep(20 * time.Millisecond)
	other := fixture(t, projects, "rewind.jsonl", cwd, "55555555-5555-4555-8555-555555555555", base.Add(-time.Hour))
	_ = other
	dir := filepath.Join(projects, EncodeCWD(cwd))
	if err := os.WriteFile(filepath.Join(dir, "broken.jsonl"), []byte("not json at all\n{\"type\":\"user\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	st := Store{Dir: t.TempDir()}
	var calls int
	res, err := Import(st, projects, cwd, func(done, total int) { calls++ })
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 2 || res.Forks != 2 || len(res.Failed) != 1 || !strings.HasPrefix(res.Failed[0], "broken.jsonl") {
		t.Fatalf("import = %+v", res)
	}
	if calls < 5 {
		t.Errorf("progress called %d times", calls)
	}
	all, err := st.List()
	if err != nil || len(all) != 2 {
		t.Fatalf("list = %+v, %v", all, err)
	}
	for _, s := range all {
		if s.Source != "import" || !s.Ended || s.TranscriptSize == 0 {
			t.Errorf("summary = %+v", s)
		}
	}
	// Newest session end first: the rewind fixture runs at 15:00, basic at 14:00.
	if all[0].ID != "55555555-5555-4555-8555-555555555555" || all[1].ID != origID {
		t.Errorf("order = %s, %s", all[0].ID, all[1].ID)
	}

	// Idempotent: nothing new.
	res, err = Import(st, projects, cwd, nil)
	if err != nil || res.Imported != 0 || res.Unchanged != 2 || len(res.Failed) != 1 {
		t.Fatalf("second import = %+v, %v", res, err)
	}

	// A grown transcript is imported again.
	path := filepath.Join(dir, origID+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(strings.ReplaceAll(strings.ReplaceAll(promptLine, "s1", origID), "/w", cwd))
	f.Close()
	os.Chtimes(path, base.Add(time.Hour), base.Add(time.Hour))
	res, err = Import(st, projects, cwd, nil)
	if err != nil || res.Imported != 1 || res.Unchanged != 1 {
		t.Fatalf("third import = %+v, %v", res, err)
	}
	r, err := st.Load(origID)
	if err != nil || r.Turns != 3 {
		t.Fatalf("re-imported record turns = %d, %v", r.Turns, err)
	}
}

func TestDirStampChangesWithNewTranscript(t *testing.T) {
	projects := t.TempDir()
	cwd := "/Users/dev/src/proj"
	if !DirStamp(projects, cwd).IsZero() {
		t.Fatal("no project dir yet")
	}
	base := time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)
	fixture(t, projects, "basic.jsonl", cwd, origID, base)
	dir := filepath.Join(projects, EncodeCWD(cwd))
	os.Chtimes(dir, base, base)
	first := DirStamp(projects, cwd)
	if !first.Equal(base) {
		t.Fatalf("stamp = %v", first)
	}
	fixture(t, projects, "basic.jsonl", cwd, "66666666-6666-4666-8666-666666666666", base)
	os.Chtimes(dir, base.Add(time.Minute), base.Add(time.Minute))
	if !DirStamp(projects, cwd).After(first) {
		t.Error("a new transcript must move the stamp")
	}
}
