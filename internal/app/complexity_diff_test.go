package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/complexitypanel"
	"ike/internal/vcs"
)

// complexity_diff_test.go covers the Changes tab's app half (#2918): the
// generation bookkeeping that never runs git in Update, the base-revision
// switch with its fallback on a rejected ref, and the off-loop computation
// against a real repository — changed, added, deleted, renamed and
// untracked files, a dirty buffer, another base than HEAD.

// changesApp opens the Complexity window on its Changes tab.
func changesApp(t *testing.T) Model {
	t.Helper()
	m, _ := timeApp(t)
	out, _ := m.Update(ComplexityToggleMsg{})
	m = out.(Model)
	m.complexityPanel().SetTab(complexitypanel.TabChanges)
	return m
}

func TestComplexityDiffBookkeepingRunsNoGitInUpdate(t *testing.T) {
	m := changesApp(t)
	// No snapshot: the tab says so and nothing is scheduled.
	m.vcs.snap = nil
	out, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40}) // a settled pass
	m = out.(Model)
	if !m.ComplexityChanges().NotGit || m.cxDiff.tickArmed {
		t.Fatalf("outside a repo: %+v armed=%v", m.ComplexityChanges(), m.cxDiff.tickArmed)
	}
	// A snapshot lands: the settled pass arms the debounce tick once.
	snap := vcs.NewSnapshot("/r", map[string]vcs.FileStatus{"a.go": vcs.StatusModified})
	out, cmd = m.Update(vcs.SnapshotMsg{Snap: snap})
	m = out.(Model)
	if cmd == nil || !m.cxDiff.tickArmed || m.cxDiff.snapGen != 1 {
		t.Fatalf("snapshot must arm the tick (cmd=%v armed=%v gen=%d)", cmd != nil, m.cxDiff.tickArmed, m.cxDiff.snapGen)
	}
	if m.ComplexityChanges().NotGit {
		t.Fatal("back inside a repository the not-git marker must drop")
	}
	out, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	if m.cxDiff.computing {
		t.Fatal("a second settled pass must not launch anything while the tick is armed")
	}
	// The tick launches exactly one computation (the command is not run
	// here: it is the git call) and marks the pane computing.
	out, cmd = m.Update(complexityDiffTickMsg{})
	m = out.(Model)
	if cmd == nil || !m.cxDiff.computing || m.cxDiff.tickArmed || !m.ComplexityChanges().Computing {
		t.Fatalf("tick must launch the diff (cmd=%v computing=%v)", cmd != nil, m.cxDiff.computing)
	}
	out, cmd = m.Update(complexityDiffTickMsg{})
	m = out.(Model)
	if cmd != nil {
		t.Fatal("a tick mid-flight must not launch a second computation")
	}
	// The result lands and is shown; the key settles, no re-arm.
	key := m.currentCxDiffKey()
	res := complexitypanel.Changes{Rev: "", Sha: "abc", Ready: true,
		Diff: complexity.DiffProject(nil, map[string][]complexity.Function{"/r/a.go": {{Name: "f", CCN: 2}}}, 15, 15)}
	out, cmd = m.Update(complexityDiffMsg{key: key, changes: res, base: map[string][]complexity.Function{"abc\x00a.go": nil}})
	m = out.(Model)
	if m.cxDiff.computing || m.cxDiff.tickArmed || cmd != nil {
		t.Fatalf("after the result: computing=%v armed=%v cmd=%v", m.cxDiff.computing, m.cxDiff.tickArmed, cmd != nil)
	}
	p := m.complexityPanel()
	if p.Total() != 1 || p.Rows()[0].Change != complexity.Added || len(m.cxDiff.baseCache) != 1 {
		t.Fatalf("pane rows = %d cache=%d", p.Total(), len(m.cxDiff.baseCache))
	}
	// A moved index generation re-arms; an unchanged snapshot (Equal) does
	// not bump the snapshot generation.
	out, cmd = m.Update(ComplexityChangedMsg{Gen: 7})
	m = out.(Model)
	if cmd == nil || !m.cxDiff.tickArmed || m.cxDiff.indexGen != 7 {
		t.Fatalf("index generation must re-arm (cmd=%v armed=%v gen=%d)", cmd != nil, m.cxDiff.tickArmed, m.cxDiff.indexGen)
	}
	m.cxDiff.tickArmed = false
	m.cxDiff.key = m.currentCxDiffKey()
	same := vcs.NewSnapshot("/r", map[string]vcs.FileStatus{"a.go": vcs.StatusModified})
	out, _ = m.Update(vcs.SnapshotMsg{Snap: same})
	m = out.(Model)
	if m.cxDiff.snapGen != 1 || m.cxDiff.tickArmed {
		t.Fatalf("an equal snapshot must not move the generation (gen=%d armed=%v)", m.cxDiff.snapGen, m.cxDiff.tickArmed)
	}
	// The tab hidden: nothing is scheduled for a new snapshot.
	p.SetTab(complexitypanel.TabProject)
	other := vcs.NewSnapshot("/r", map[string]vcs.FileStatus{"b.go": vcs.StatusModified})
	out, _ = m.Update(vcs.SnapshotMsg{Snap: other})
	m = out.(Model)
	if m.cxDiff.snapGen != 2 || m.cxDiff.tickArmed {
		t.Fatalf("hidden tab must not schedule (gen=%d armed=%v)", m.cxDiff.snapGen, m.cxDiff.tickArmed)
	}
}

func TestComplexityDiffBaseSwitchAndRejectedRef(t *testing.T) {
	m := changesApp(t)
	m.vcs.snap = vcs.NewSnapshot("/r", map[string]vcs.FileStatus{"a.go": vcs.StatusModified})
	m.cxDiff.snapGen = 1
	m.cxDiff.key = m.currentCxDiffKey()
	m.cxDiff.result = complexitypanel.Changes{Rev: "", Ready: true}
	// 'R' main: wanted base moves, the settled pass arms the tick.
	out, cmd := m.Update(complexitypanel.SetBaseMsg{Rev: "main"})
	m = out.(Model)
	if m.cxDiff.rev != "main" || cmd == nil || !m.cxDiff.tickArmed {
		t.Fatalf("R main: rev=%q cmd=%v armed=%v", m.cxDiff.rev, cmd != nil, m.cxDiff.tickArmed)
	}
	out, _ = m.Update(complexityDiffTickMsg{})
	m = out.(Model)
	// A rejected ref keeps the previous base and shows git's message; the
	// key settles so no retry runs.
	out, cmd = m.Update(complexityDiffMsg{key: m.currentCxDiffKey(), err: "git: unknown revision: main"})
	m = out.(Model)
	c := m.ComplexityChanges()
	if m.cxDiff.rev != "" || c.Rev != "" || c.Err != "git: unknown revision: main" || c.Computing {
		t.Fatalf("rejected ref: rev=%q changes=%+v", m.cxDiff.rev, c)
	}
	if cmd != nil || m.cxDiff.tickArmed || m.cxDiff.key != m.currentCxDiffKey() {
		t.Fatal("a rejected ref must not re-arm")
	}
	if v := m.complexityPanel().View(); !strings.Contains(v, "unknown revision: main") {
		t.Fatalf("the pane must show the git error:\n%s", v)
	}
	// HEAD typed explicitly is the default base.
	out, _ = m.Update(complexitypanel.SetBaseMsg{Rev: "HEAD"})
	m = out.(Model)
	if m.cxDiff.rev != "" || m.ComplexityChanges().Err != "" {
		t.Fatalf("HEAD: rev=%q err=%q", m.cxDiff.rev, m.ComplexityChanges().Err)
	}
}

// gitRepo builds a repository with a main branch and a feature branch whose
// working tree has every change kind, and returns its root.
func gitRepo(t *testing.T) (dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	if _, err := complexity.Measure("go", []string{"package p"}); err != nil {
		t.Skip("no Go syntax tree in this build")
	}
	dir = t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-b", "main")
	write("mod.go", "package p\n\nfunc f(a int) int {\n\tif a > 0 {\n\t\treturn 1\n\t}\n\treturn 0\n}\n\nfunc same() {}\n")
	write("gone.go", "package p\n\nfunc g() {}\n")
	write("old.go", "package p\n\nfunc old(x int) {\n\tif x > 1 {\n\t}\n}\n")
	write("notes.txt", "text\n")
	run("add", ".")
	run("commit", "-m", "init")
	run("checkout", "-q", "-b", "feature")
	// Committed on the branch: a new file.
	write("branch.go", "package p\n\nfunc onBranch() {}\n")
	run("add", ".")
	run("commit", "-m", "branch work")
	// Working tree: f grows one branch, gone.go is deleted, old.go renamed,
	// a new untracked file, a text file edited.
	write("mod.go", "package p\n\nfunc f(a int) int {\n\tif a > 0 {\n\t\treturn 1\n\t}\n\tif a < 0 {\n\t\treturn -1\n\t}\n\treturn 0\n}\n\nfunc same() {}\n")
	run("rm", "-q", "gone.go")
	run("mv", "old.go", "renamed.go")
	write("fresh.go", "package p\n\nfunc fresh() {}\n")
	write("notes.txt", "more text\n")
	return dir
}

func TestRunComplexityDiffRealRepo(t *testing.T) {
	dir := gitRepo(t)
	root, err := vcs.DetectRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := vcs.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	job := cxDiffJob{key: cxDiffKey{snapGen: 1, rev: ""}, repoRoot: root, projRoot: root, snap: snap, dirty: map[string]string{}, ccnWarn: 15, cogWarn: 15}
	msg := runComplexityDiff(job).(complexityDiffMsg)
	if msg.err != "" {
		t.Fatal(msg.err)
	}
	c := msg.changes
	if !c.Ready || c.Sha == "" || c.Rev != "" {
		t.Fatalf("changes = %+v", c)
	}
	rel := func(p string) string { r, _ := filepath.Rel(root, p); return r }
	got := map[string]complexity.FileDiff{}
	for p, fd := range c.Diff.Files {
		got[rel(p)] = fd
	}
	// mod.go: f changed (CCN 2 -> 3), same untouched.
	fd := got["mod.go"]
	if len(fd.Changed) != 1 || fd.Changed[0].Before.Name != "f" || fd.Changed[0].Before.CCN != 2 || fd.Changed[0].After.CCN != 3 || len(fd.Added)+len(fd.Removed) != 0 {
		t.Errorf("mod.go = %+v", fd)
	}
	// gone.go: g removed; fresh.go (untracked): fresh added; the rename is
	// removed under the old path + added under the new one with a note.
	if fd := got["gone.go"]; len(fd.Removed) != 1 || fd.Removed[0].Name != "g" {
		t.Errorf("gone.go = %+v", fd)
	}
	if fd := got["fresh.go"]; len(fd.Added) != 1 || fd.Added[0].Name != "fresh" {
		t.Errorf("fresh.go = %+v", fd)
	}
	if fd := got["old.go"]; len(fd.Removed) != 1 || fd.Removed[0].Name != "old" {
		t.Errorf("old.go = %+v", fd)
	}
	if fd := got["renamed.go"]; len(fd.Added) != 1 || fd.Added[0].Name != "old" {
		t.Errorf("renamed.go = %+v", fd)
	}
	if len(c.Renames) != 1 || c.Renames[0] != "old.go → renamed.go" {
		t.Errorf("renames = %v", c.Renames)
	}
	// The committed branch file and the text file are not part of a diff
	// against HEAD.
	if _, ok := got["branch.go"]; ok {
		t.Error("branch.go is clean against HEAD")
	}
	if _, ok := got["notes.txt"]; ok {
		t.Error("a text file has no functions to diff")
	}
	// Verdict: f +1, old moved (0), g -1, fresh +1 → ΣCCN +1, more complex.
	if c.Diff.DeltaCCN() != 1 || c.Diff.Verdict != complexity.MoreComplex {
		t.Errorf("Δccn %d verdict %v", c.Diff.DeltaCCN(), c.Diff.Verdict)
	}
	// Base blobs were measured fresh and offered to the cache.
	if len(msg.base) != 3 {
		t.Errorf("cache additions = %d: %v", len(msg.base), keysOf(msg.base))
	}
	if len(c.Dirty) != 0 {
		t.Errorf("no buffer is dirty: %v", c.Dirty)
	}

	// Against main the branch commit counts too, and a cached base blob is
	// not measured again.
	job.key.rev = "main"
	job.cache = msg.base
	msg = runComplexityDiff(job).(complexityDiffMsg)
	if msg.err != "" {
		t.Fatal(msg.err)
	}
	got = map[string]complexity.FileDiff{}
	for p, fd := range msg.changes.Diff.Files {
		got[rel(p)] = fd
	}
	if fd := got["branch.go"]; len(fd.Added) != 1 || fd.Added[0].Name != "onBranch" {
		t.Errorf("branch.go vs main = %+v", fd)
	}
	if msg.changes.Rev != "main" || msg.changes.Diff.DeltaCCN() != 2 {
		t.Errorf("vs main: rev=%q Δccn=%d", msg.changes.Rev, msg.changes.Diff.DeltaCCN())
	}
	for k := range msg.base {
		if _, cached := job.cache[k]; cached {
			t.Errorf("base blob %q measured again despite the cache", k)
		}
	}

	// A dirty buffer wins over the disk: f shrinks back to one branch and
	// the row is marked.
	job.key.rev = ""
	job.dirty = map[string]string{filepath.Join(root, "mod.go"): "package p\n\nfunc f(a int) int {\n\tif a > 0 {\n\t\treturn 1\n\t}\n\treturn 0\n}\n\nfunc same() {}\n"}
	msg = runComplexityDiff(job).(complexityDiffMsg)
	if msg.err != "" {
		t.Fatal(msg.err)
	}
	if fd, ok := msg.changes.Diff.Files[filepath.Join(root, "mod.go")]; ok {
		t.Errorf("the dirty buffer equals the base, mod.go must not be listed: %+v", fd)
	}
	if !msg.changes.Dirty[filepath.Join(root, "mod.go")] {
		t.Errorf("dirty = %v", msg.changes.Dirty)
	}

	// An invalid base: git's message, no diff.
	job.key.rev = "no-such-ref"
	msg = runComplexityDiff(job).(complexityDiffMsg)
	if !strings.HasPrefix(msg.err, "git: ") || msg.changes.Ready {
		t.Fatalf("invalid ref = %+v", msg)
	}
}

func TestRunComplexityDiffProjectSubdirectoryAndExclude(t *testing.T) {
	dir := gitRepo(t)
	root, _ := vcs.DetectRoot(dir)
	snap, err := vcs.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	// A project root below the repository sees nothing of the top level.
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	job := cxDiffJob{key: cxDiffKey{}, repoRoot: root, projRoot: sub, snap: snap, dirty: map[string]string{}}
	msg := runComplexityDiff(job).(complexityDiffMsg)
	if msg.err != "" || !msg.changes.Diff.Empty() {
		t.Fatalf("subdirectory project = %+v / %q", msg.changes.Diff, msg.err)
	}
	// complexity.exclude applies to the diff like to the index.
	job.projRoot = root
	job.exclude = []string{"*.go"}
	msg = runComplexityDiff(job).(complexityDiffMsg)
	if msg.err != "" || !msg.changes.Diff.Empty() {
		t.Fatalf("excluded = %+v / %q", msg.changes.Diff, msg.err)
	}
}

func keysOf(m map[string][]complexity.Function) []string {
	var out []string
	for k := range m {
		out = append(out, strings.ReplaceAll(k, "\x00", ":"))
	}
	return out
}
