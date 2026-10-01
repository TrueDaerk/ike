package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/pane"
	"ike/internal/vcs"
	"ike/internal/vcspanel"
)

// reviewRepo builds the working tree the review tests walk (#2848):
//
//	a.txt    modified, unstaged
//	b.txt    staged change plus a further unstaged edit (compared against HEAD)
//	bin.dat  modified binary (a placeholder, skipped by stepping)
//	c.txt    deleted
//	z.txt    untracked (last)
func reviewRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	write("a.txt", []byte("a1\na2\na3\n"))
	write("b.txt", []byte("b1\n"))
	write("c.txt", []byte("c1\n"))
	write("bin.dat", []byte("bin\x00old"))
	git("add", ".")
	git("commit", "-qm", "initial")
	write("a.txt", []byte("A1\na2\na3\n"))
	write("b.txt", []byte("b2\n"))
	git("add", "b.txt")
	write("b.txt", []byte("b3\n"))
	write("bin.dat", []byte("bin\x00new"))
	os.Remove(filepath.Join(dir, "c.txt"))
	write("z.txt", []byte("z1\n"))
	// git reports the resolved root (/private/var on macOS); the tests
	// compare paths against it.
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return dir
}

// reviewApp opens the review over reviewRepo and returns the app, the repo
// and the review instance.
func reviewApp(t *testing.T) (Model, string, *pane.Instance) {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := reviewRepo(t)
	m := newSized()
	m = reviewSnapshot(t, m, dir)
	out, cmd := m.Update(ReviewChangesMsg{})
	m = drainCmd(out.(Model), cmd)
	inst := reviewInstance(t, m)
	return m, dir, inst
}

// reviewSnapshot loads the repo's status into the app, the way a refresh
// would.
func reviewSnapshot(t *testing.T, m Model, dir string) Model {
	t.Helper()
	snap, err := vcs.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	out, cmd := m.Update(vcs.SnapshotMsg{Snap: snap})
	return drainCmd(out.(Model), cmd)
}

// reviewInstance finds the one review pane.
func reviewInstance(t *testing.T, m Model) *pane.Instance {
	t.Helper()
	_, _, inst, ok := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindDiff && c.Diff().Reviewing()
	})
	if !ok {
		t.Fatal("no review pane open")
	}
	return inst
}

func reviewPaths(inst *pane.Instance) []string {
	var paths []string
	for _, f := range inst.Diff().ReviewFiles() {
		p := f.Path
		if f.Skip != "" {
			p += "(" + f.Skip + ")"
		}
		paths = append(paths, p)
	}
	return paths
}

func TestReviewChangesOrdersFilesAndComparesAgainstHEAD(t *testing.T) {
	m, dir, inst := reviewApp(t)
	d := inst.Diff()
	if got := strings.Join(reviewPaths(inst), " "); got != "a.txt b.txt bin.dat(binary) c.txt z.txt" {
		t.Fatalf("review order = %q", got)
	}
	if cur, _ := d.ReviewCurrent(); cur.Path != "a.txt" || d.ReviewIndex() != 0 {
		t.Fatalf("review starts on %q (index %d), want a.txt", cur.Path, d.ReviewIndex())
	}
	l, r := d.SideLabels()
	if l != "a.txt @ HEAD" || r != "a.txt (working tree)" {
		t.Fatalf("labels = %q / %q", l, r)
	}
	if d.RightPath() != filepath.Join(dir, "a.txt") {
		t.Fatalf("right path = %q", d.RightPath())
	}
	v := ansi.Strip(d.View())
	if !strings.Contains(v, "file 1/5 · hunk 1/1") {
		t.Fatalf("footer lacks the position:\n%s", v)
	}
	// The focused pane is the review, so the diff-scoped file steps apply.
	if f := m.focusedContent(); f == nil || f.Kind() != pane.KindDiff {
		t.Fatal("the review pane must take the focus")
	}
	// Staged + modified: working tree vs HEAD, one diff — the index
	// version never shows.
	out, cmd := m.Update(DiffFileStepMsg{Delta: 1})
	m = drainCmd(out.(Model), cmd)
	inst = reviewInstance(t, m)
	d = inst.Diff()
	if cur, _ := d.ReviewCurrent(); cur.Path != "b.txt" {
		t.Fatalf("next file = %q, want b.txt", cur.Path)
	}
	v = ansi.Strip(d.View())
	if !strings.Contains(v, "b1") || !strings.Contains(v, "b3") || strings.Contains(v, "b2") {
		t.Fatalf("b.txt must compare HEAD (b1) with the working tree (b3), not the index (b2):\n%s", v)
	}
}

func TestReviewHunkStepsCrossFilesAndSkipBinary(t *testing.T) {
	m, _, inst := reviewApp(t)
	// a.txt has one hunk: F7 crosses into b.txt.
	out, cmd := m.Update(DiffStepMsg{Delta: 1})
	m = drainCmd(out.(Model), cmd)
	inst = reviewInstance(t, m)
	if cur, _ := inst.Diff().ReviewCurrent(); cur.Path != "b.txt" {
		t.Fatalf("F7 past the last hunk landed on %q, want b.txt", cur.Path)
	}
	// Past b.txt's only hunk: bin.dat is a placeholder, so c.txt follows —
	// the deleted file, with an empty working-tree side.
	out, cmd = m.Update(DiffStepMsg{Delta: 1})
	m = drainCmd(out.(Model), cmd)
	inst = reviewInstance(t, m)
	d := inst.Diff()
	if cur, _ := d.ReviewCurrent(); cur.Path != "c.txt" {
		t.Fatalf("F7 landed on %q, want c.txt (bin.dat skipped)", cur.Path)
	}
	if v := ansi.Strip(d.View()); !strings.Contains(v, "c1") || !strings.Contains(v, "file 4/5") {
		t.Fatalf("deleted file view:\n%s", v)
	}
	// shift+F7 from c.txt's first hunk walks back over the placeholder to
	// b.txt's last hunk.
	out, cmd = m.Update(DiffStepMsg{Delta: -1})
	m = drainCmd(out.(Model), cmd)
	inst = reviewInstance(t, m)
	d = inst.Diff()
	if cur, _ := d.ReviewCurrent(); cur.Path != "b.txt" || d.CurrentHunk() != 0 {
		t.Fatalf("shift+F7 landed on %q hunk %d, want b.txt's last hunk", cur.Path, d.CurrentHunk())
	}
	// The untracked file at the end diffs against an empty HEAD side.
	for i := 0; i < 2; i++ {
		out, cmd = m.Update(DiffFileStepMsg{Delta: 1})
		m = drainCmd(out.(Model), cmd)
	}
	inst = reviewInstance(t, m)
	d = inst.Diff()
	if cur, _ := d.ReviewCurrent(); cur.Path != "z.txt" {
		t.Fatalf("file steps ended on %q, want z.txt", cur.Path)
	}
	if v := ansi.Strip(d.View()); !strings.Contains(v, "z1") || !strings.Contains(v, "file 5/5") {
		t.Fatalf("untracked file view:\n%s", v)
	}
	// Nothing after the last file.
	out, cmd = m.Update(DiffStepMsg{Delta: 1})
	m = drainCmd(out.(Model), cmd)
	if cur, _ := reviewInstance(t, m).Diff().ReviewCurrent(); cur.Path != "z.txt" {
		t.Fatalf("F7 at the end moved to %q", cur.Path)
	}
}

func TestReviewRefreshDropsCleanFileAndKeepsPosition(t *testing.T) {
	m, dir, inst := reviewApp(t)
	out, cmd := m.Update(DiffFileStepMsg{Delta: 1})
	m = drainCmd(out.(Model), cmd)
	inst = reviewInstance(t, m)
	if cur, _ := inst.Diff().ReviewCurrent(); cur.Path != "b.txt" {
		t.Fatalf("on %q, want b.txt", cur.Path)
	}
	// a.txt goes back to its HEAD content: the next status snapshot drops
	// it, b.txt stays current at its new index.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a1\na2\na3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = reviewSnapshot(t, m, dir)
	inst = reviewInstance(t, m)
	d := inst.Diff()
	if got := strings.Join(reviewPaths(inst), " "); got != "b.txt bin.dat(binary) c.txt z.txt" {
		t.Fatalf("list after refresh = %q", got)
	}
	if cur, _ := d.ReviewCurrent(); cur.Path != "b.txt" || d.ReviewIndex() != 0 {
		t.Fatalf("position after refresh: %q at %d, want b.txt at 0", cur.Path, d.ReviewIndex())
	}
	if v := ansi.Strip(d.View()); !strings.Contains(v, "file 1/4") || !strings.Contains(v, "b3") {
		t.Fatalf("view after refresh:\n%s", v)
	}
	// The file on screen itself becomes clean: its successor loads.
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitReset := exec.Command("git", "reset", "-q", "b.txt")
	gitReset.Dir = dir
	if out, err := gitReset.CombinedOutput(); err != nil {
		t.Fatalf("git reset: %v\n%s", err, out)
	}
	m = reviewSnapshot(t, m, dir)
	inst = reviewInstance(t, m)
	d = inst.Diff()
	if got := strings.Join(reviewPaths(inst), " "); got != "bin.dat(binary) c.txt z.txt" {
		t.Fatalf("list after second refresh = %q", got)
	}
	if cur, _ := d.ReviewCurrent(); cur.Path != "c.txt" {
		t.Fatalf("successor = %q, want c.txt (the placeholder is stepped over)", cur.Path)
	}
}

func TestReviewWorkingTreeEditReloadsInPlace(t *testing.T) {
	m, dir, inst := reviewApp(t)
	d := inst.Diff()
	if d.HunkCount() != 1 {
		t.Fatalf("a.txt hunks = %d", d.HunkCount())
	}
	abs := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(abs, []byte("A1\na2\nA3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.reloadReviewForPath(abs)
	if d.HunkCount() != 2 {
		t.Fatalf("after the on-disk edit hunks = %d, want 2 (re-diffed in place)", d.HunkCount())
	}
	if cur, _ := d.ReviewCurrent(); cur.Path != "a.txt" {
		t.Fatalf("reload changed the file to %q", cur.Path)
	}
}

func TestReviewEntryPointsFromThePanel(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := reviewRepo(t)
	m := newSized()
	m = reviewSnapshot(t, m, dir)
	// shift+enter on a row: review mode positioned at that file.
	out, cmd := m.Update(vcspanel.OpenDiffMsg{Path: "c.txt", Review: true})
	m = drainCmd(out.(Model), cmd)
	inst := reviewInstance(t, m)
	if cur, _ := inst.Diff().ReviewCurrent(); cur.Path != "c.txt" {
		t.Fatalf("positioned at %q, want c.txt", cur.Path)
	}
	if n := countDiffViewers(m); n != 1 {
		t.Fatalf("diff viewers = %d", n)
	}
	// The panel's review-all action reuses the pane and keeps the place.
	out, cmd = m.Update(vcspanel.ReviewChangesMsg{})
	m = drainCmd(out.(Model), cmd)
	if n := countDiffViewers(m); n != 1 {
		t.Fatalf("review-all duplicated the pane: %d viewers", n)
	}
	if cur, _ := reviewInstance(t, m).Diff().ReviewCurrent(); cur.Path != "c.txt" {
		t.Fatalf("re-running the review moved to %q", cur.Path)
	}
	// Plain enter still opens the single-file HEAD diff, which stops at
	// its ends.
	out, cmd = m.Update(vcspanel.OpenDiffMsg{Path: "a.txt"})
	m = drainCmd(out.(Model), cmd)
	f := m.focusedContent()
	if f == nil || f.Kind() != pane.KindDiff || f.Diff().Reviewing() {
		t.Fatal("enter must open a single-file diff, not the review")
	}
	if cmd := f.Diff().StepHunk(1); cmd != nil {
		t.Fatal("a single-file diff must not request another file")
	}
}

func TestReviewChangesOnCleanTreeNotifies(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	git := exec.Command("git", "init", "-q")
	git.Dir = dir
	if out, err := git.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	m := newSized()
	m = reviewSnapshot(t, m, dir)
	out, cmd := m.Update(ReviewChangesMsg{})
	m = drainCmd(out.(Model), cmd)
	if n := countDiffViewers(m); n != 0 {
		t.Fatalf("a clean tree opened %d viewers", n)
	}
}
