package forge

// cache_test.go covers the persistent listing cache (#2108): the snapshot
// store's load/save rules (version, remote key, corruption, the toggle), the
// incremental merge semantics, and the incremental fetch's resync fallbacks.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// cacheEnv points the store at a temp dir and pins the remote key, restoring
// both afterwards.
func cacheEnv(t *testing.T, remote string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	prev := cacheRemote
	cacheRemote = func(string) string { return remote }
	t.Cleanup(func() { cacheRemote = prev })
	return dir
}

func openIssue(n int, title string) Issue {
	return Issue{Number: n, Title: title, State: "OPEN"}
}

func TestCacheRoundTrip(t *testing.T) {
	cacheEnv(t, "git@github.com:o/r.git")
	at := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	msg := IssuesMsg{State: IssuesOpen,
		Issues: []Issue{openIssue(1, "one")},
		PRs:    []PR{{Number: 2, Title: "pr", State: "OPEN"}}}
	storeCache(".", msg, at)
	c, ok := LoadCache(".")
	if !ok {
		t.Fatal("LoadCache: want the stored snapshot back")
	}
	if len(c.Issues) != 1 || c.Issues[0].Title != "one" || len(c.PRs) != 1 || !c.FetchedAt.Equal(at) {
		t.Fatalf("snapshot = %+v, want the stored listing and timestamp", c)
	}
}

func TestCacheIgnoresCorruptAndForeignFiles(t *testing.T) {
	dir := cacheEnv(t, "remote-a")
	path := filepath.Join(dir, "forgecache.json")

	// Missing file.
	if _, ok := LoadCache("."); ok {
		t.Fatal("a missing cache file must read as no cache")
	}
	// Corrupt file: ignored silently, never fatal.
	os.WriteFile(path, []byte("{not json"), 0o644)
	if _, ok := LoadCache("."); ok {
		t.Fatal("a corrupt cache file must read as no cache")
	}
	// Wrong schema version.
	os.WriteFile(path, []byte(`{"version":99,"remote":"remote-a"}`), 0o644)
	if _, ok := LoadCache("."); ok {
		t.Fatal("an unknown schema version must read as no cache")
	}
	// Another remote: a repository or backend switch invalidates the snapshot.
	storeCache(".", IssuesMsg{State: IssuesOpen, Issues: []Issue{openIssue(1, "x")}}, time.Now())
	cacheRemote = func(string) string { return "remote-b" }
	if _, ok := LoadCache("."); ok {
		t.Fatal("a snapshot for another remote must read as no cache")
	}
}

func TestCacheToggleOff(t *testing.T) {
	dir := cacheEnv(t, "remote")
	SetCacheEnabled(false)
	t.Cleanup(func() { SetCacheEnabled(true) })
	storeCache(".", IssuesMsg{State: IssuesOpen, Issues: []Issue{openIssue(1, "x")}}, time.Now())
	if _, err := os.Stat(filepath.Join(dir, "forgecache.json")); !os.IsNotExist(err) {
		t.Fatal("forge.cache off must never write the snapshot file")
	}
	SetCacheEnabled(true)
	storeCache(".", IssuesMsg{State: IssuesOpen, Issues: []Issue{openIssue(1, "x")}}, time.Now())
	SetCacheEnabled(false)
	if _, ok := LoadCache("."); ok {
		t.Fatal("forge.cache off must never read the snapshot file")
	}
}

func TestCacheStoresOnlyCleanOpenListings(t *testing.T) {
	dir := cacheEnv(t, "remote")
	path := filepath.Join(dir, "forgecache.json")
	for _, msg := range []IssuesMsg{
		{State: IssuesClosed, Issues: []Issue{openIssue(1, "x")}},
		{State: IssuesOpen, Setup: "gh missing"},
		{State: IssuesOpen, Err: os.ErrDeadlineExceeded},
	} {
		storeCache(".", msg, time.Now())
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("stored a snapshot for %+v — only clean open listings are snapshots", msg)
		}
	}
}

func TestCachePRErrKeepsPreviousPRs(t *testing.T) {
	cacheEnv(t, "remote")
	storeCache(".", IssuesMsg{State: IssuesOpen,
		Issues: []Issue{openIssue(1, "x")},
		PRs:    []PR{{Number: 9, State: "OPEN"}}}, time.Now())
	// The next fetch's PR half failed: the issues advance, the PRs carry over
	// — mirroring the poll snapshot's in-memory PRErr rule.
	storeCache(".", IssuesMsg{State: IssuesOpen,
		Issues: []Issue{openIssue(2, "y")},
		PRErr:  os.ErrDeadlineExceeded}, time.Now())
	c, ok := LoadCache(".")
	if !ok {
		t.Fatal("LoadCache after a PRErr store")
	}
	if len(c.Issues) != 1 || c.Issues[0].Number != 2 {
		t.Fatalf("issues = %+v, want the fresh listing", c.Issues)
	}
	if len(c.PRs) != 1 || c.PRs[0].Number != 9 {
		t.Fatalf("prs = %+v, want the previously cached PRs kept", c.PRs)
	}
}

func TestMergeOpenIssuesUpdateInsertClose(t *testing.T) {
	base := []Issue{openIssue(3, "three"), openIssue(2, "two"), openIssue(1, "one")}
	updates := []Issue{
		{Number: 2, Title: "two, retitled", State: "OPEN"},       // update in place
		{Number: 4, Title: "four", State: "OPEN"},                // new open issue
		{Number: 3, Title: "three", State: "CLOSED"},             // open → closed: drops out
		{Number: 99, Title: "closed elsewhere", State: "CLOSED"}, // never cached, stays out
	}
	got := mergeOpenIssues(base, updates)
	want := []struct {
		number int
		title  string
	}{{4, "four"}, {2, "two, retitled"}, {1, "one"}}
	if len(got) != len(want) {
		t.Fatalf("merged = %+v, want %d issues", got, len(want))
	}
	for i, w := range want {
		if got[i].Number != w.number || got[i].Title != w.title {
			t.Errorf("merged[%d] = #%d %q, want #%d %q", i, got[i].Number, got[i].Title, w.number, w.title)
		}
	}
	// Idempotent: the overlap window re-delivers updates; merging them again
	// must change nothing.
	again := mergeOpenIssues(got, updates)
	if len(again) != len(got) || again[0].Number != got[0].Number {
		t.Fatalf("re-merge = %+v, want the identical listing", again)
	}
}

// fakeIncForge is a Forge stub with just the listing half implemented; the
// embedded interface panics on anything else, which is exactly what a test
// wants from an unexpected call.
type fakeIncForge struct {
	Forge
	since   []Issue
	full    []Issue
	prs     []PR
	partial bool // IssuesSince reports a possibly truncated answer
	asked   struct {
		since int
		full  int
	}
}

func (f *fakeIncForge) Issues(IssueState) ([]Issue, error) {
	f.asked.full++
	return f.full, nil
}
func (f *fakeIncForge) PRs() ([]PR, error) { return f.prs, nil }
func (f *fakeIncForge) IssuesSince(time.Time) ([]Issue, bool, error) {
	f.asked.since++
	return f.since, !f.partial, nil
}

// plantForge seeds the detection cache with a fake backend for dir.
func plantForge(t *testing.T, dir string, f Forge) {
	t.Helper()
	key, err := filepath.Abs(dir)
	if err != nil {
		key = dir
	}
	detectMu.Lock()
	detectCache[key] = f
	detectMu.Unlock()
	t.Cleanup(func() { ResetDetection(dir) })
}

func TestIncrementalFetchMergesUpdates(t *testing.T) {
	cacheEnv(t, "remote")
	fake := &fakeIncForge{
		since: []Issue{openIssue(5, "fresh"), {Number: 1, State: "CLOSED"}},
		prs:   []PR{{Number: 7, State: "OPEN"}},
	}
	plantForge(t, ".", fake)
	storeCache(".", IssuesMsg{State: IssuesOpen,
		Issues: []Issue{openIssue(1, "one"), openIssue(2, "two")}}, time.Now())

	msg, ok := fetchIncremental(".")
	if !ok {
		t.Fatal("fetchIncremental: want the incremental path with a fresh snapshot")
	}
	if fake.asked.since != 1 || fake.asked.full != 0 {
		t.Fatalf("asked = %+v, want exactly one updated-since fetch and no full listing", fake.asked)
	}
	if len(msg.Issues) != 2 || msg.Issues[0].Number != 5 || msg.Issues[1].Number != 2 {
		t.Fatalf("issues = %+v, want #1 dropped (closed) and #5 inserted", msg.Issues)
	}
	if len(msg.PRs) != 1 {
		t.Fatalf("prs = %+v, want the freshly listed PRs", msg.PRs)
	}
	// The merged result became the new snapshot.
	c, ok := LoadCache(".")
	if !ok || len(c.Issues) != 2 || c.Issues[0].Number != 5 {
		t.Fatalf("snapshot after merge = %+v, want the merged listing persisted", c.Issues)
	}
}

func TestIncrementalFetchFallsBackToFull(t *testing.T) {
	cacheEnv(t, "remote")
	fake := &fakeIncForge{partial: true}
	plantForge(t, ".", fake)

	// No snapshot at all → full path.
	if _, ok := fetchIncremental("."); ok {
		t.Fatal("no snapshot must fall back to the full fetch")
	}
	// A snapshot past resyncAfter → full resync, re-converging any drift.
	storeCache(".", IssuesMsg{State: IssuesOpen, Issues: []Issue{openIssue(1, "x")}},
		time.Now().Add(-resyncAfter-time.Minute))
	if _, ok := fetchIncremental("."); ok {
		t.Fatal("a stale snapshot must fall back to the full fetch")
	}
	// A fresh snapshot but a possibly truncated updated-since page → full.
	storeCache(".", IssuesMsg{State: IssuesOpen, Issues: []Issue{openIssue(1, "x")}}, time.Now())
	if _, ok := fetchIncremental("."); ok {
		t.Fatal("a truncated incremental page must fall back to the full fetch")
	}
	if fake.asked.since != 1 {
		t.Fatalf("since fetches = %d, want 1 (only the fresh-snapshot attempt)", fake.asked.since)
	}
}

func TestPollDiffFiresOnMergedSnapshots(t *testing.T) {
	// The poller's diff must keep working when consecutive listings are
	// incremental merges rather than full fetches (#2108): a closed issue
	// drops out of the merged open listing exactly as it would from a fresh
	// one, so the IssueClosed/IssueOpened events still fire.
	p := NewPoller(".", DefaultPollInterval)
	base := []Issue{openIssue(1, "one"), openIssue(2, "two")}
	p.Tick()
	if res := p.Apply(IssuesMsg{State: IssuesOpen, Issues: base, Poll: true}); !res.Seeded {
		t.Fatal("first poll seeds silently")
	}
	merged := mergeOpenIssues(base, []Issue{
		{Number: 1, State: "CLOSED"},
		openIssue(3, "three"),
	})
	p.Tick()
	res := p.Apply(IssuesMsg{State: IssuesOpen, Issues: merged, Poll: true})
	if len(res.Events) != 2 {
		t.Fatalf("events = %+v, want opened #3 and closed #1", res.Events)
	}
	if res.Events[0].Kind != IssueOpened || res.Events[0].Number != 3 {
		t.Errorf("events[0] = %+v, want IssueOpened #3", res.Events[0])
	}
	if res.Events[1].Kind != IssueClosed || res.Events[1].Number != 1 {
		t.Errorf("events[1] = %+v, want IssueClosed #1", res.Events[1])
	}
}
