package agenttrace

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestEncodeCWD(t *testing.T) {
	cases := map[string]string{
		"/Users/dev/src/ike":                             "-Users-dev-src-ike",
		"/Users/dev/src/ike/.claude/worktrees/issue+1-x": "-Users-dev-src-ike--claude-worktrees-issue-1-x",
		"/private/tmp/ike-fork-test":                     "-private-tmp-ike-fork-test",
		"/home/dev/src/proj":                             "-home-dev-src-proj",
		"/home/dev/my_project (copy)":                    "-home-dev-my-project--copy-",
		`C:\src\ike`:                                     "C--src-ike",
		"/Users/dev/Automation/ike/.claude/worktrees/issue+2842-0540-transcript-parser-session-discovery-internal": "-Users-dev-Automation-ike--claude-worktrees-issue-2842-0540-transcript-parser-session-discovery-internal",
	}
	for in, want := range cases {
		if got := EncodeCWD(in); got != want {
			t.Errorf("EncodeCWD(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProjectsDirHonoursConfigDir(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/cfg")
	if got := ProjectsDir(); got != filepath.Join("/cfg", "projects") {
		t.Errorf("ProjectsDir = %q", got)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if got := ProjectsDir(); !strings.HasSuffix(got, filepath.Join(".claude", "projects")) {
		t.Errorf("ProjectsDir default = %q", got)
	}
}

// sessionIDRE matches the session id field of a fixture line.
var sessionIDRE = regexp.MustCompile(`"sessionId":"[^"]*"`)

// fixture copies a testdata transcript into projects/<EncodeCWD(cwd)>/<id>.jsonl
// with its cwd and session id rewritten, and pins the modification time.
func fixture(t *testing.T, projects, name, cwd, id string, mod time.Time) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.ReplaceAll(string(data), "/Users/dev/src/proj", cwd))
	data = sessionIDRE.ReplaceAll(data, []byte(`"sessionId":"`+id+`"`))
	dir := filepath.Join(projects, EncodeCWD(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
	return path
}

const (
	origID = "11111111-1111-4111-8111-111111111111"
	forkID = "22222222-2222-4222-8222-222222222222"
)

// TestDiscoverSkipsFork: the fork is the newest file right after an "ask",
// but it is a copy of the original's root line, so the original wins.
func TestDiscoverSkipsFork(t *testing.T) {
	projects := t.TempDir()
	cwd := "/Users/dev/src/proj"
	base := time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)
	fixture(t, projects, "basic.jsonl", cwd, origID, base)
	// Creation order matters for the birth-time tie-break on macOS.
	time.Sleep(20 * time.Millisecond)
	fixture(t, projects, "fork.jsonl", cwd, forkID, base.Add(5*time.Minute))
	// An unrelated older session in the same directory.
	time.Sleep(20 * time.Millisecond)
	other := fixture(t, projects, "basic.jsonl", cwd, "33333333-3333-4333-8333-333333333333", base.Add(-time.Hour))
	rewrite(t, other, "u-root", "u-other-root")

	all, err := ListIn(projects, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("ListIn = %+v", all)
	}
	if all[0].ID != forkID || all[0].ParentID != origID {
		t.Errorf("newest = %s parent=%q, want fork of %s", all[0].ID, all[0].ParentID, origID)
	}
	if all[1].ID != origID || all[1].ParentID != "" {
		t.Errorf("second = %s parent=%q", all[1].ID, all[1].ParentID)
	}
	if all[2].ParentID != "" {
		t.Errorf("unrelated session marked as fork: %+v", all[2])
	}

	got, err := DiscoverIn(projects, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != origID || got.CWD != cwd || !strings.HasSuffix(got.Path, origID+".jsonl") {
		t.Errorf("Discover = %+v", got)
	}

	// Excluding the original leaves only the unrelated session.
	got, err = DiscoverIn(projects, cwd, origID)
	if err != nil || got.ID != "33333333-3333-4333-8333-333333333333" {
		t.Errorf("Discover excluding original = %+v err=%v", got, err)
	}
}

func rewrite(t *testing.T, path, old, new string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(data), old, new)), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(path, st.ModTime(), st.ModTime())
}

func TestDiscoverNotFound(t *testing.T) {
	projects := t.TempDir()
	if _, err := DiscoverIn(projects, "/nowhere"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v", err)
	}
	// A directory whose transcripts belong to another cwd (hash-like
	// collision of the encoded name) is not a match.
	cwd := "/Users/dev/src/proj"
	fixture(t, projects, "basic.jsonl", cwd, origID, time.Now())
	if _, err := DiscoverIn(projects, "/Users/dev/src-proj"); !errors.Is(err, ErrNotFound) {
		t.Errorf("collision err = %v", err)
	}
	if got, err := DiscoverIn(projects, cwd+"/"); err != nil || got.ID != origID {
		t.Errorf("trailing slash: %+v %v", got, err)
	}
}

// TestDiscoverSymlinkedCWD: on macOS a session started in /tmp/x records
// /private/tmp/x; the tool pane's cwd may be either spelling.
func TestDiscoverSymlinkedCWD(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on windows")
	}
	root := t.TempDir()
	real := filepath.Join(root, "real")
	link := filepath.Join(root, "link")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlink:", err)
	}
	// The harness records the fully resolved directory (t.TempDir itself sits
	// behind /var -> /private/var on macOS).
	real, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	projects := t.TempDir()
	fixture(t, projects, "basic.jsonl", real, origID, time.Now())
	got, err := DiscoverIn(projects, link)
	if err != nil {
		t.Fatalf("link → real: %v", err)
	}
	if got.ID != origID {
		t.Errorf("got %+v", got)
	}
	if got, err := DiscoverIn(projects, real); err != nil || got.ID != origID {
		t.Errorf("real → real: %+v %v", got, err)
	}
}

func TestMarkForksOrdersByCreation(t *testing.T) {
	ts := func(s int) time.Time { return time.Date(2026, 9, 30, 14, 0, s, 0, time.UTC) }
	// Birth time wins over first timestamp and mtime.
	all := []Located{
		{ID: "fork", rootUUID: "r", Born: ts(30), FirstAt: ts(1), ModTime: ts(1)},
		{ID: "orig", rootUUID: "r", Born: ts(0), FirstAt: ts(2), ModTime: ts(99)},
		{ID: "solo", rootUUID: "x", Born: ts(0)},
		{ID: "headless", Born: ts(0)},
	}
	markForks(all)
	if all[0].ParentID != "orig" || all[1].ParentID != "" || all[2].ParentID != "" || all[3].ParentID != "" {
		t.Errorf("born: %+v", all)
	}
	// Without birth times the first timestamp decides; then mtime.
	all = []Located{
		{ID: "a", rootUUID: "r", FirstAt: ts(5), ModTime: ts(1)},
		{ID: "b", rootUUID: "r", FirstAt: ts(1), ModTime: ts(9)},
		{ID: "c", rootUUID: "r", FirstAt: ts(1), ModTime: ts(10)},
	}
	markForks(all)
	if all[0].ParentID != "b" || all[1].ParentID != "" || all[2].ParentID != "b" {
		t.Errorf("first-at/mtime: %+v", all)
	}
}

func TestLocateReadsHeaderOnly(t *testing.T) {
	path := filepath.Join("testdata", "fork.jsonl")
	l, ok := locate(path)
	if !ok {
		t.Fatal("locate failed")
	}
	if l.ID != forkID || l.CWD != "/Users/dev/src/proj" || l.rootUUID != "u-root" {
		t.Errorf("header = %+v", l)
	}
	// The first timestamped line of a -p fork is its own queue entry at fork
	// time, which orders it after the original even without birth times.
	if got := l.FirstAt.Format(time.RFC3339); got != "2026-09-30T14:05:00Z" {
		t.Errorf("FirstAt = %s", got)
	}
	if runtime.GOOS == "darwin" && l.Born.IsZero() {
		t.Error("darwin reports no birth time")
	}
}
