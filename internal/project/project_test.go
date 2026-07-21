package project

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"ike/internal/config"

	"github.com/BurntSushi/toml"
)

// --- validate.go ---

func TestValidateResolvesRelativeAndCleans(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "proj")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	got, err := Validate("proj" + string(filepath.Separator))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	// TempDir may itself hold symlinks (macOS /var -> /private/var); compare
	// the resolved forms.
	want, _ := filepath.EvalSymlinks(sub)
	if resolved, _ := filepath.EvalSymlinks(got); resolved != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("result should be absolute, got %q", got)
	}
}

func TestValidateExpandsTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sub := filepath.Join(home, "code")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := Validate("~/code")
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got != sub {
		t.Errorf("got %q, want %q", got, sub)
	}
	if got, err := Validate("~"); err != nil || got != home {
		t.Errorf("bare ~ should resolve to home, got %q, %v", got, err)
	}
}

func TestValidateRejections(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "afile")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, path, wantSub string
	}{
		{"empty", "", "empty"},
		{"blank", "   ", "empty"},
		{"missing", filepath.Join(dir, "nope"), "does not exist"},
		{"file", file, "not a directory"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Validate(tc.path); err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("Validate(%q) = %v, want error containing %q", tc.path, err, tc.wantSub)
			}
		})
	}
}

func TestValidateRejectsUnreadableDir(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("permission bits not enforceable here")
	}
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	if _, err := Validate(locked); err == nil || !strings.Contains(err.Error(), "not readable") {
		t.Errorf("unreadable dir should be rejected, got %v", err)
	}
}

func TestValidateAcceptsEmptyDir(t *testing.T) {
	dir := t.TempDir()
	if _, err := Validate(dir); err != nil {
		t.Errorf("empty dir should be a valid root, got %v", err)
	}
}

// --- entry.go ---

func TestEntryRoundTripAndDefaults(t *testing.T) {
	at := time.Date(2026, 6, 19, 10, 0, 0, 0, time.UTC)
	e := NewEntry("/Users/me/code/ike", at)
	if e.Name != "ike" {
		t.Errorf("default name should be base dir, got %q", e.Name)
	}
	c := e.toConfig()
	if c.LastOpened != "2026-06-19T10:00:00Z" {
		t.Errorf("last_opened should be RFC3339 UTC, got %q", c.LastOpened)
	}
	back := fromConfig(c)
	if back.Path != e.Path || back.Name != e.Name || !back.LastOpened.Equal(at) {
		t.Errorf("round trip lost data: %+v", back)
	}
}

func TestFromConfigToleratesBadFields(t *testing.T) {
	e := fromConfig(config.ProjectHistoryEntry{Path: "/a/b", LastOpened: "not-a-time"})
	if !e.LastOpened.IsZero() {
		t.Errorf("bad timestamp should yield zero time, got %v", e.LastOpened)
	}
	if e.Name != "b" {
		t.Errorf("missing name should fall back to base dir, got %q", e.Name)
	}
}

// --- history.go ---

func entryPaths(entries []Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Path
	}
	return out
}

func TestUpsertMoveToFrontDedupeCap(t *testing.T) {
	now := time.Now()
	var h []Entry
	for _, p := range []string{"/a", "/b", "/a", "/c", "/d"} {
		h = upsert(h, NewEntry(p, now), 3)
	}
	want := []string{"/d", "/c", "/a"}
	got := entryPaths(h)
	if len(got) != len(want) {
		t.Fatalf("history = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("history = %v, want %v", got, want)
		}
	}
}

func TestUpsertZeroMaxKeepsNothing(t *testing.T) {
	if h := upsert(nil, NewEntry("/a", time.Now()), 0); len(h) != 0 {
		t.Errorf("max 0 should keep nothing, got %v", h)
	}
}

// testOpts points config at a throwaway user settings file with no project layer.
func testOpts(t *testing.T) config.Options {
	t.Helper()
	return config.Options{UserPath: filepath.Join(t.TempDir(), "settings.toml")}
}

// readHistory loads the persisted history back through the real config pipeline.
func readHistory(t *testing.T, opts config.Options) []config.ProjectHistoryEntry {
	t.Helper()
	cfg, _ := config.Load(opts)
	return cfg.Project.History
}

func TestRecordOpenPersistsThroughConfig(t *testing.T) {
	opts := testOpts(t)
	rootA, rootB := t.TempDir(), t.TempDir()
	t0 := time.Date(2026, 6, 19, 10, 0, 0, 0, time.UTC)

	if err := RecordOpen(opts, rootA, t0); err != nil {
		t.Fatal(err)
	}
	if err := RecordOpen(opts, rootB, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// Re-open A: moves to front, no duplicate.
	if err := RecordOpen(opts, rootA, t0.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}

	h := readHistory(t, opts)
	if len(h) != 2 {
		t.Fatalf("expected 2 entries, got %+v", h)
	}
	if h[0].Path != rootA || h[1].Path != rootB {
		t.Errorf("order wrong: %+v", h)
	}
	if h[0].Name != filepath.Base(rootA) {
		t.Errorf("name should default to base dir, got %q", h[0].Name)
	}
	if h[0].LastOpened != "2026-06-19T12:00:00Z" {
		t.Errorf("re-open should refresh last_opened, got %q", h[0].LastOpened)
	}

	// The persisted shape is the [[project.history]] table array from the spec.
	data, err := os.ReadFile(opts.UserPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[[project.history]]") {
		t.Errorf("expected [[project.history]] tables, got:\n%s", data)
	}
}

func TestRecordOpenCapsAtMaxHistory(t *testing.T) {
	opts := testOpts(t)
	if err := os.WriteFile(opts.UserPath, []byte("[project]\nmax_history = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	roots := []string{t.TempDir(), t.TempDir(), t.TempDir()}
	for i, r := range roots {
		if err := RecordOpen(opts, r, time.Unix(int64(i), 0)); err != nil {
			t.Fatal(err)
		}
	}
	h := readHistory(t, opts)
	if len(h) != 2 || h[0].Path != roots[2] || h[1].Path != roots[1] {
		t.Errorf("history should cap at 2 newest-first, got %+v", h)
	}
	// max_history itself must survive the history writes (typed setter only
	// touches the one key).
	cfg, _ := config.Load(opts)
	if cfg.Project.MaxHistory != 2 {
		t.Errorf("max_history should round-trip, got %d", cfg.Project.MaxHistory)
	}
}

func TestRecordOpenFailureLeavesHistoryUntouched(t *testing.T) {
	opts := testOpts(t)
	root := t.TempDir()
	if err := RecordOpen(opts, root, time.Now()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(opts.UserPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := RecordOpen(opts, filepath.Join(root, "missing"), time.Now()); err == nil {
		t.Fatal("recording a missing path should fail")
	}
	after, err := os.ReadFile(opts.UserPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("failed record must not touch the stored history")
	}
}

func TestRecordOpenCmdDeliversMsg(t *testing.T) {
	opts := testOpts(t)
	root := t.TempDir()
	msg := RecordOpenCmd(opts, root, time.Now())()
	rec, ok := msg.(RecordedMsg)
	if !ok || rec.Err != nil || rec.Root != root {
		t.Fatalf("unexpected msg: %#v", msg)
	}
	if len(readHistory(t, opts)) != 1 {
		t.Errorf("cmd should persist the entry")
	}

	bad := RecordOpenCmd(opts, filepath.Join(root, "missing"), time.Now())()
	if rec := bad.(RecordedMsg); rec.Err == nil {
		t.Errorf("cmd should surface validation errors")
	}
}

// TestHistoryDecodesTypedEntries guards the read side against the persisted shape.
func TestHistoryDecodesTypedEntries(t *testing.T) {
	opts := testOpts(t)
	doc := map[string]any{"project": map[string]any{"history": []map[string]any{
		{"path": "/x/one", "name": "uno", "last_opened": "2026-01-02T03:04:05Z"},
		{"path": "/x/two"},
	}}}
	data, err := toml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(opts.UserPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, _ := config.Load(opts)
	h := History(cfg)
	if len(h) != 2 {
		t.Fatalf("expected 2 entries, got %+v", h)
	}
	if h[0].Name != "uno" || h[0].LastOpened.IsZero() {
		t.Errorf("typed fields lost: %+v", h[0])
	}
	if h[1].Name != "two" || !h[1].LastOpened.IsZero() {
		t.Errorf("defaults wrong for sparse entry: %+v", h[1])
	}
}

// TestRemoveFromHistory (#842): the entry vanishes from the persisted list;
// other entries and their timestamps survive; a missing path is a no-op.
func TestRemoveFromHistory(t *testing.T) {
	opts := testOpts(t)
	rootA, rootB := t.TempDir(), t.TempDir()
	t0 := time.Date(2026, 6, 19, 10, 0, 0, 0, time.UTC)
	if err := RecordOpen(opts, rootA, t0); err != nil {
		t.Fatal(err)
	}
	if err := RecordOpen(opts, rootB, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if err := RemoveFromHistory(opts, rootA); err != nil {
		t.Fatal(err)
	}
	h := readHistory(t, opts)
	if len(h) != 1 || h[0].Path != rootB {
		t.Fatalf("history after remove = %+v, want only %s", h, rootB)
	}
	if h[0].LastOpened != "2026-06-19T11:00:00Z" {
		t.Fatalf("surviving entry must keep its timestamp, got %q", h[0].LastOpened)
	}

	if err := RemoveFromHistory(opts, "/not/in/list"); err != nil {
		t.Fatalf("removing a missing path must be a no-op, got %v", err)
	}
	if len(readHistory(t, opts)) != 1 {
		t.Fatal("no-op removal must not change the list")
	}
}

// TestRelTime (#842): compact relative rendering, "" for the zero time.
func TestRelTime(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	for want, at := range map[string]time.Time{
		"":         {},
		"just now": now.Add(-30 * time.Second),
		"5m ago":   now.Add(-5 * time.Minute),
		"3h ago":   now.Add(-3 * time.Hour),
		"4d ago":   now.Add(-4 * 24 * time.Hour),
		"3w ago":   now.Add(-21 * 24 * time.Hour),
	} {
		if got := RelTime(at, now); got != want {
			t.Errorf("RelTime(%v) = %q, want %q", at, got, want)
		}
	}
}
