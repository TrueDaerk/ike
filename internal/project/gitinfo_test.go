package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/palette"
)

// --- gitinfo.go (#2178) ---

// probeOut assembles porcelain-v2 -z output from its NUL-terminated records.
func probeOut(records ...string) []byte {
	var b strings.Builder
	for _, r := range records {
		b.WriteString(r)
		b.WriteByte(0)
	}
	return []byte(b.String())
}

func TestParseGitProbeCleanBranch(t *testing.T) {
	got := parseGitProbe(probeOut(
		"# branch.oid 1234567890abcdef",
		"# branch.head main",
		"# branch.upstream origin/main",
		"# branch.ab +0 -0",
	))
	if !got.Repo || got.Branch != "main" || got.Dirty {
		t.Errorf("got %+v, want main, clean, repo", got)
	}
}

func TestParseGitProbeDirty(t *testing.T) {
	for name, rec := range map[string]string{
		"modified":  "1 .M N... 100644 100644 100644 aaa bbb main.go",
		"renamed":   "2 R. N... 100644 100644 100644 aaa bbb R100 new.go",
		"conflict":  "u UU N... 100644 100644 100644 100644 aaa bbb ccc x.go",
		"untracked": "? scratch.txt",
	} {
		got := parseGitProbe(probeOut("# branch.head main", rec))
		if !got.Dirty {
			t.Errorf("%s: Dirty = false, want true (%+v)", name, got)
		}
		if got.Branch != "main" {
			t.Errorf("%s: branch = %q", name, got.Branch)
		}
	}
}

func TestParseGitProbeDetachedHead(t *testing.T) {
	got := parseGitProbe(probeOut("# branch.oid 1234567890abcdef", "# branch.head (detached)"))
	if got.Branch != "1234567" {
		t.Errorf("branch = %q, want the short hash", got.Branch)
	}
	// A repository without any commit reports "(initial)" — nothing to show.
	got = parseGitProbe(probeOut("# branch.oid (initial)", "# branch.head (detached)"))
	if got.Branch != "" || !got.Repo {
		t.Errorf("initial detached: got %+v", got)
	}
}

func TestGitInfoBadge(t *testing.T) {
	cases := []struct {
		info GitInfo
		want string
	}{
		{GitInfo{}, ""},           // never probed / not a repo
		{GitInfo{Repo: true}, ""}, // repo without commits
		{GitInfo{Repo: true, Branch: "main"}, "⎇ main"}, // clean
		{GitInfo{Repo: true, Branch: "main", Dirty: true}, "⎇ main*"},
		{GitInfo{Branch: "main", Dirty: true}, ""}, // failed probe stays plain
	}
	for _, c := range cases {
		if got := c.info.Badge(); got != c.want {
			t.Errorf("%+v.Badge() = %q, want %q", c.info, got, c.want)
		}
	}
	long := GitInfo{Repo: true, Branch: strings.Repeat("x", maxBranchWidth+10)}.Badge()
	if got := len([]rune(strings.TrimPrefix(long, "⎇ "))); got != maxBranchWidth {
		t.Errorf("clipped branch is %d runes, want %d (%q)", got, maxBranchWidth, long)
	}
	if !strings.HasSuffix(long, "…") {
		t.Errorf("clipped branch %q should end in an ellipsis", long)
	}
}

func TestGitCacheSetGet(t *testing.T) {
	c := NewGitCache()
	if _, ok := c.Get("/code/ike"); ok {
		t.Error("fresh cache should know nothing")
	}
	c.Set(GitInfo{Path: "/code/ike", Branch: "main", Repo: true})
	if info, ok := c.Get("/code/ike"); !ok || info.Branch != "main" {
		t.Errorf("Get = %+v, %v", info, ok)
	}
	// A later probe replaces the earlier answer.
	c.Set(GitInfo{Path: "/code/ike", Branch: "feature", Repo: true, Dirty: true})
	if info, _ := c.Get("/code/ike"); info.Branch != "feature" || !info.Dirty {
		t.Errorf("second Set did not replace: %+v", info)
	}
	// A nil cache is inert, so an unwired picker still works.
	var nilCache *GitCache
	nilCache.Set(GitInfo{Path: "/code/ike"})
	if _, ok := nilCache.Get("/code/ike"); ok {
		t.Error("nil cache should stay empty")
	}
}

func TestEnrichCmdCapsAndDedupes(t *testing.T) {
	if EnrichCmd(nil) != nil {
		t.Error("empty history should produce no command")
	}
	if EnrichCmd([]Entry{{Path: ""}}) != nil {
		t.Error("a pathless entry is nothing to probe")
	}
	batch, ok := EnrichCmd([]Entry{
		{Path: "/code/ike"},
		{Path: "/code/ike"}, // duplicate: one probe is enough
		{Path: "/code/website"},
	})().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("batch = %v (%d cmds), want 2", ok, len(batch))
	}

	var long []Entry
	for i := 0; i < maxGitProbes+5; i++ {
		long = append(long, Entry{Path: "/code/p" + strconv.Itoa(i)})
	}
	batch, _ = EnrichCmd(long)().(tea.BatchMsg)
	if len(batch) != maxGitProbes {
		t.Errorf("probed %d entries, want the %d cap", len(batch), maxGitProbes)
	}
}

// TestProbeGitRealRepo exercises the probe against a throwaway repository;
// skipped when git is not on PATH.
func TestProbeGitRealRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "init")

	got := probeGit(dir, gitProbeTimeout)
	if !got.Repo || got.Branch != "main" || got.Dirty || got.Path != dir {
		t.Fatalf("clean repo: got %+v", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := probeGit(dir, gitProbeTimeout); !got.Dirty {
		t.Errorf("untracked file should read dirty: %+v", got)
	}
}

func TestProbeGitDegradesToPlain(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	// A directory outside any repository, a path that does not exist, and an
	// empty path all resolve to "nothing to show" — never to an error the
	// picker would have to render.
	for _, p := range []string{t.TempDir(), filepath.Join(t.TempDir(), "gone"), ""} {
		if got := probeGit(p, gitProbeTimeout); got.Repo || got.Badge() != "" {
			t.Errorf("probeGit(%q) = %+v, want a plain row", p, got)
		}
	}
}

// TestProbeGitTimeout pins the bound: a git call that cannot answer within
// the timeout leaves the row plain instead of holding anything up. A real
// repository is used so only the deadline can be the reason.
func TestProbeGitTimeout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	cmd := exec.Command("git", "-C", dir, "init", "-b", "main")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	start := time.Now()
	got := probeGit(dir, time.Nanosecond)
	if got.Repo || got.Badge() != "" {
		t.Errorf("timed-out probe = %+v, want a plain row", got)
	}
	if got.Path != dir {
		t.Errorf("Path = %q, want %q — the msg must still name its row", got.Path, dir)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("probe took %v — the deadline did not bound it", elapsed)
	}
}

// --- picker.go row enrichment (#2178) ---

func TestPickerRowsMergeGitInfo(t *testing.T) {
	m, _ := newPicker(t, fixedHistory)
	cache := NewGitCache()
	m.SetGitCache(cache)

	// Nothing probed yet: every row renders exactly as before.
	for _, it := range m.Results("", palette.Context{}) {
		if it.Badge != "" {
			t.Errorf("%s: badge %q before any probe answered", it.Title, it.Badge)
		}
	}

	cache.Set(GitInfo{Path: "/code/ike", Branch: "main", Dirty: true, Repo: true})
	cache.Set(GitInfo{Path: "/code/website", Branch: "main", Repo: true})
	cache.Set(GitInfo{Path: "/work/intra"}) // not a git project
	want := map[string]string{
		"ike":     "⎇ main*",
		"website": "⎇ main",
		"intra":   "",
	}
	for _, it := range m.Results("", palette.Context{}) {
		if it.Badge != want[it.Title] {
			t.Errorf("%s: badge = %q, want %q", it.Title, it.Badge, want[it.Title])
		}
	}
}

func TestPickerGitBadgeJoinsOpenDot(t *testing.T) {
	m, _ := newPicker(t, fixedHistory)
	cache := NewGitCache()
	m.SetGitCache(cache)
	m.SetOpen(func(p string) bool { return p == "/code/ike" })
	cache.Set(GitInfo{Path: "/code/ike", Branch: "main", Repo: true})

	items := m.Results("", palette.Context{})
	if items[0].Badge != "● ⎇ main" {
		t.Errorf("badge = %q, want the dot and the branch", items[0].Badge)
	}
	// The open marker's aux action survives the merge.
	if _, ok := items[0].Aux.(CloseWorkspaceMsg); !ok {
		t.Errorf("aux = %T, want CloseWorkspaceMsg", items[0].Aux)
	}
	// An unprobed open row keeps the bare dot.
	m.SetOpen(func(p string) bool { return p == "/work/intra" })
	items = m.Results("", palette.Context{})
	if items[2].Badge != "●" {
		t.Errorf("unprobed open row badge = %q, want %q", items[2].Badge, "●")
	}
}

func TestPickerWithoutGitCacheIsUnchanged(t *testing.T) {
	m, _ := newPicker(t, fixedHistory)
	for _, it := range m.Results("", palette.Context{}) {
		if it.Badge != "" {
			t.Errorf("%s: badge %q without a cache wired", it.Title, it.Badge)
		}
	}
}
