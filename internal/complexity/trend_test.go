package complexity

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"ike/internal/lang"
)

// trend_test.go covers the trend walk (#2962): five commits on a temporary
// repository whose points equal an independent full measurement of each
// checkout, the incremental re-measuring counted through the measure seam,
// the cache serving a second walk, the project-subdirectory and exclude
// gates and the first-parent rule.

const trendFnA = "package p\n\nfunc a(x int) int {\n\tif x > 0 {\n\t\treturn 1\n\t}\n\treturn 0\n}\n"
const trendFnAGrown = "package p\n\nfunc a(x int) int {\n\tif x > 0 {\n\t\treturn 1\n\t}\n\tif x < 0 {\n\t\treturn -1\n\t}\n\treturn 0\n}\n"
const trendFnB = "package p\n\nfunc b(x, y int) int {\n\tif x > y && y > 0 {\n\t\treturn x\n\t}\n\treturn y\n}\n"

type trendRepo struct {
	t   *testing.T
	dir string
}

func newTrendRepo(t *testing.T) trendRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	if !Available() {
		t.Skip("no syntax trees in this build")
	}
	r := trendRepo{t: t, dir: t.TempDir()}
	if ev, err := filepath.EvalSymlinks(r.dir); err == nil {
		r.dir = ev
	}
	r.git("init", "-q", "-b", "main")
	return r
}

func (r trendRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", r.dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r trendRepo) write(rel, content string) {
	r.t.Helper()
	p := filepath.Join(r.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r trendRepo) commit(msg string) string {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-qm", msg)
	return r.git("rev-parse", "HEAD")
}

// fiveCommits builds the fixture: a.go; b.go added; a.go grows; b.go
// deleted and a text file edited; a.go renamed to c.go.
func fiveCommits(r trendRepo) []string {
	var shas []string
	r.write("a.go", trendFnA)
	r.write("notes.txt", "one\n")
	shas = append(shas, r.commit("one"))
	r.write("sub/b.go", trendFnB)
	shas = append(shas, r.commit("two"))
	r.write("a.go", trendFnAGrown)
	shas = append(shas, r.commit("three"))
	r.git("rm", "-q", "sub/b.go")
	r.write("notes.txt", "two\n")
	shas = append(shas, r.commit("four"))
	r.git("mv", "a.go", "c.go")
	shas = append(shas, r.commit("five"))
	return shas
}

// fullMeasure independently measures the whole tree at sha: an archive is
// extracted into a fresh directory and walked like the project index does.
func fullMeasure(t *testing.T, r trendRepo, sha string, res Resolver) (Summary, int) {
	t.Helper()
	dir := t.TempDir()
	archive := exec.Command("git", "-C", r.dir, "archive", "--format=tar", sha)
	tar := exec.Command("tar", "-x", "-C", dir)
	pipe, err := archive.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	tar.Stdin = pipe
	if err := archive.Start(); err != nil {
		t.Fatal(err)
	}
	if out, err := tar.CombinedOutput(); err != nil {
		t.Fatalf("tar: %v\n%s", err, out)
	}
	if err := archive.Wait(); err != nil {
		t.Fatal(err)
	}
	files := map[string][]Function{}
	err = WalkProject(dir, nil, func(path string, l lang.Language, measurable bool) {
		if !measurable {
			return
		}
		text, ok := ReadSource(path)
		if !ok {
			return
		}
		fns, err := Measure(l.ID, strings.Split(text, "\n"))
		if err != nil {
			return
		}
		rel, _ := filepath.Rel(dir, path)
		files[filepath.Join(r.dir, rel)] = fns
	})
	if err != nil {
		t.Fatal(err)
	}
	return SummarizeFiles(files, res), len(files)
}

func TestWalkTrendMatchesFullMeasurementAtEverySha(t *testing.T) {
	r := newTrendRepo(t)
	shas := fiveCommits(r)
	res := Uniform(Thresholds{CCN: 2, Cog: 2})
	commits, err := ListTrendCommits(r.dir, "", 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 5 || commits[0].Sha != shas[0] || commits[4].Sha != shas[4] || commits[0].Subject != "one" {
		t.Fatalf("commits = %+v", commits)
	}
	var calls atomic.Int32
	job := TrendJob{RepoRoot: r.dir, Res: res, Cache: NewTrendCache(),
		Measure: func(path, text string) []Function { calls.Add(1); return measureText(path, text) }}
	tr, err := WalkTrend(job, commits)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Points) != 5 {
		t.Fatalf("points = %d", len(tr.Points))
	}
	for i, p := range tr.Points {
		want, files := fullMeasure(t, r, shas[i], res)
		if p.Sha != shas[i] || p.Summary != want || p.Files != files {
			t.Errorf("point %d (%s): files %d summary %+v\nwant files %d %+v", i, p.Subject, p.Files, p.Summary, files, want)
		}
		if p.Short == "" || p.Time.IsZero() {
			t.Errorf("point %d lacks short / time: %+v", i, p)
		}
	}
	// The incremental walk measured a.go at one, b.go at two, a.go at
	// three — nothing at four (a delete and a text edit) or five (a rename
	// keeps the blob).
	if got := calls.Load(); got != 3 || tr.Measured != 3 {
		t.Errorf("Measure calls = %d, measured = %d, want 3 each", got, tr.Measured)
	}
	// The trend reads the growth and the deletion.
	e := func(i int) int { return tr.Points[i].Summary.Excess }
	if !(e(1) > e(0) && e(2) > e(1) && e(3) < e(2) && e(4) == e(3)) {
		t.Errorf("excess per point: %d %d %d %d %d", e(0), e(1), e(2), e(3), e(4))
	}
	if tr.DeltaExcess() != e(4)-e(0) || tr.Newest().Sha != shas[4] || tr.Oldest().Sha != shas[0] {
		t.Errorf("delta %d newest %s oldest %s", tr.DeltaExcess(), tr.Newest().Short, tr.Oldest().Short)
	}

	// A second walk is served by the cache: no measurement, same points.
	calls.Store(0)
	again, err := WalkTrend(job, commits)
	if err != nil || calls.Load() != 0 || again.Measured != 0 || len(again.Points) != 5 {
		t.Fatalf("cached walk: err %v calls %d measured %d points %d", err, calls.Load(), again.Measured, len(again.Points))
	}
	for i := range again.Points {
		if again.Points[i].Summary != tr.Points[i].Summary {
			t.Errorf("cached point %d differs", i)
		}
	}
	// A shorter window over the same commits: the points are cached per sha,
	// nothing is measured either; a new head after a fresh commit measures
	// only its changed file.
	short, err := WalkTrend(job, commits[3:])
	if err != nil || calls.Load() != 0 || len(short.Points) != 2 || short.Points[0].Sha != shas[3] {
		t.Fatalf("short window: err %v calls %d points %+v", err, calls.Load(), short.Points)
	}
	r.write("c.go", trendFnA)
	r.commit("six")
	commits, _ = ListTrendCommits(r.dir, "", 30)
	six, err := WalkTrend(job, commits)
	if err != nil || len(six.Points) != 6 || calls.Load() != 0 || six.Measured != 0 {
		// c.go at six holds a.go's original blob, measured at one: the
		// blob cache serves it.
		t.Fatalf("six: err %v points %d calls %d measured %d", err, len(six.Points), calls.Load(), six.Measured)
	}
	if six.Points[5].Summary != tr.Points[0].Summary {
		t.Errorf("six = %+v, want one's summary %+v", six.Points[5].Summary, tr.Points[0].Summary)
	}
	// Without a cache every walk measures again.
	calls.Store(0)
	job.Cache = nil
	if _, err := WalkTrend(job, commits); err != nil || calls.Load() != 4 {
		t.Fatalf("uncached walk: err %v calls %d, want 4 (a, b, a grown, c at six; the rename carries its blob)", err, calls.Load())
	}
	// Progress is told once per point.
	var seen []int
	job.Progress = func(done, total int) { seen = append(seen, done*100+total) }
	if _, err := WalkTrend(job, commits[:2]); err != nil || len(seen) != 2 || seen[0] != 102 || seen[1] != 202 {
		t.Fatalf("progress %v err %v", seen, err)
	}
}

func TestWalkTrendProjectSubdirectoryExcludeAndFirstParent(t *testing.T) {
	r := newTrendRepo(t)
	r.write("app/a.go", trendFnA)
	r.write("app/gen/skip.go", trendFnB)
	r.write("other/o.go", trendFnB)
	r.commit("one")
	r.git("checkout", "-qb", "side")
	r.write("app/side.go", trendFnB)
	r.commit("side work")
	r.git("checkout", "-q", "main")
	r.git("merge", "-q", "--no-ff", "-m", "merge side", "side")
	commits, err := ListTrendCommits(r.dir, "", 30)
	if err != nil {
		t.Fatal(err)
	}
	// First parent: the merge is a point, the side commit is not.
	if len(commits) != 2 || commits[0].Subject != "one" || commits[1].Subject != "merge side" {
		t.Fatalf("commits = %+v", commits)
	}
	cache := NewTrendCache()
	job := TrendJob{RepoRoot: r.dir, Root: filepath.Join(r.dir, "app"), Exclude: []string{"gen/**"}, Cache: cache,
		Res: Uniform(Thresholds{CCN: 1, Cog: 1})}
	tr, err := WalkTrend(job, commits)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Points) != 2 || tr.Points[0].Files != 1 || tr.Points[1].Files != 2 {
		t.Fatalf("points = %+v", tr.Points)
	}
	if got := cache.TrendPaths(commits[1].Sha); strings.Join(got, ",") != "app/a.go,app/side.go" {
		t.Errorf("files at the merge = %v", got)
	}
	if tr.Points[1].Summary.Functions != 2 || tr.Points[1].Summary.OverThreshold != 2 {
		t.Errorf("merge summary = %+v", tr.Points[1].Summary)
	}
	// Keep narrows further (the CLI's path selection).
	job.Cache, job.Keep = nil, func(abs string) bool { return filepath.Base(abs) == "a.go" }
	if tr, err = WalkTrend(job, commits); err != nil || tr.Points[1].Files != 1 {
		t.Fatalf("keep: err %v points %+v", err, tr.Points)
	}
	// An unknown revision is git's error.
	if _, err := ListTrendCommits(r.dir, "no-such-ref", 5); err == nil {
		t.Error("unknown rev must fail")
	}
	// No commits: an empty walk.
	if tr, err := WalkTrend(job, nil); err != nil || len(tr.Points) != 0 || tr.Newest().Sha != "" {
		t.Errorf("empty walk: %+v %v", tr, err)
	}
}
