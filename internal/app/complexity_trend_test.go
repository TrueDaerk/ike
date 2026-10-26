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
	"ike/internal/telemetry"
	"ike/internal/vcs"
)

// complexity_trend_test.go covers the Trend tab's app half (#2962): the
// bookkeeping that launches one walk per key and never git in Update, the
// off-loop walk against a real repository landing in the pane, enter
// setting the Changes base, 'r' and a config reload dropping the cache, the
// progress relay and the telemetry op.

// trendApp opens the Complexity window on its Trend tab.
func trendApp(t *testing.T) Model {
	t.Helper()
	m, _ := timeApp(t)
	out, _ := m.Update(ComplexityToggleMsg{})
	m = out.(Model)
	m.complexityPanel().SetTab(complexitypanel.TabTrend)
	return m
}

func TestComplexityTrendBookkeeping(t *testing.T) {
	m := trendApp(t)
	// No snapshot: the tab says so and nothing is scheduled.
	m.vcs.snap = nil
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40}) // a settled pass
	m = out.(Model)
	if !m.ComplexityTrend().NotGit || m.cxTrend.computing {
		t.Fatalf("outside a repo: %+v computing=%v", m.ComplexityTrend(), m.cxTrend.computing)
	}
	if !strings.Contains(m.complexityPanel().View(), "not a git repository") {
		t.Fatal("the pane must say not a git repository")
	}
	// A snapshot lands: the settled pass launches exactly one walk and
	// marks the pane computing; a second pass launches nothing more.
	snap := vcs.NewSnapshot(t.TempDir(), map[string]vcs.FileStatus{})
	out, cmd := m.Update(vcs.SnapshotMsg{Snap: snap})
	m = out.(Model)
	if cmd == nil || !m.cxTrend.computing || m.cxTrend.pending.commits != 30 || m.cxTrend.pending.snapGen != 1 {
		t.Fatalf("snapshot must launch the walk (cmd=%v computing=%v pending=%+v)", cmd != nil, m.cxTrend.computing, m.cxTrend.pending)
	}
	if tr := m.ComplexityTrend(); !tr.Computing || tr.NotGit || tr.Window != 30 {
		t.Fatalf("pane state = %+v", tr)
	}
	pending := m.cxTrend.pending
	out, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	if m.cxTrend.pending != pending {
		t.Fatal("a second settled pass must not launch another walk")
	}
	// The progress tick relays the counter into the pane and re-arms.
	m.cxTrend.progress.Store(12)
	m.cxTrend.totalCh <- 30
	out, cmd = m.Update(complexityTrendTickMsg{key: pending})
	m = out.(Model)
	if cmd == nil || m.ComplexityTrend().Done != 12 || m.ComplexityTrend().Total != 30 {
		t.Fatalf("tick: cmd=%v trend=%+v", cmd != nil, m.ComplexityTrend())
	}
	if !strings.Contains(m.complexityPanel().View(), "computing… 12/30") {
		t.Fatalf("pane must show the progress:\n%s", m.complexityPanel().View())
	}
	// A failed walk keeps the previous points and shows the message; the
	// key settles so no retry storms.
	out, _ = m.Update(complexityTrendMsg{key: pending, err: "git: boom"})
	m = out.(Model)
	if m.cxTrend.computing || m.cxTrend.key != pending || m.ComplexityTrend().Err != "git: boom" {
		t.Fatalf("after error: %+v key=%+v", m.ComplexityTrend(), m.cxTrend.key)
	}
	out, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	if m.cxTrend.computing {
		t.Fatal("a settled key must not relaunch")
	}
	// A stale tick (another key) ends its chain.
	if cmd := m.complexityTrendProgress(complexityTrendTickMsg{key: cxTrendKey{gen: 99}}); cmd != nil {
		t.Fatal("a stale tick must not re-arm")
	}
	// 'r' drops the cache and moves the key: the next pass walks again.
	m.cxTrend.cache = complexity.NewTrendCache()
	out, _ = m.Update(complexitypanel.RefreshMsg{})
	m = out.(Model)
	if m.cxTrend.gen != 1 {
		t.Fatalf("r must bump the trend generation: %d", m.cxTrend.gen)
	}
	out, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	if !m.cxTrend.computing || m.cxTrend.pending.gen != 1 {
		t.Fatalf("r must relaunch: computing=%v pending=%+v", m.cxTrend.computing, m.cxTrend.pending)
	}
	// A result lands: the pane shows the points, newest first.
	pts := []complexity.TrendPoint{
		{Sha: "a", Short: "aaa1111", Subject: "one", Summary: complexity.Summary{Excess: 10}},
		{Sha: "b", Short: "bbb2222", Subject: "two", Summary: complexity.Summary{Excess: 4}},
	}
	out, _ = m.Update(complexityTrendMsg{key: m.cxTrend.pending, trend: complexity.Trend{Points: pts, Measured: 3}, ms: 42})
	m = out.(Model)
	tr := m.ComplexityTrend()
	if !tr.Ready || tr.Computing || tr.Err != "" || len(tr.Points) != 2 || tr.Window != 30 {
		t.Fatalf("after result: %+v", tr)
	}
	out, _ = m.Update(tea.WindowSizeMsg{Width: 160, Height: 40}) // the five-tab bar leaves the header room
	m = out.(Model)
	p := m.complexityPanel()
	if rows := p.Rows(); len(rows) != 2 || rows[0].Trend.Point.Short != "bbb2222" {
		t.Fatalf("rows = %+v", rows)
	}
	if !strings.Contains(p.View(), "Σexcess 10 → ") {
		t.Fatalf("header:\n%s", p.View())
	}
	// enter on the newest row: the Changes tab compares bbb2222..HEAD.
	out, cmd = m.Update(complexitypanel.TrendBaseMsg{Rev: "bbb2222"})
	m = out.(Model)
	if m.cxDiff.rev != "bbb2222..HEAD" || m.complexityPanel().Tab() != complexitypanel.TabChanges {
		t.Fatalf("enter: rev %q tab %v", m.cxDiff.rev, m.complexityPanel().Tab())
	}
	// Back on the Trend tab the data is still there; a reopened pane is
	// seeded with it.
	m.complexityPanel().SetTab(complexitypanel.TabTrend)
	if len(m.complexityPanel().Rows()) != 2 {
		t.Fatal("trend rows lost on a tab round trip")
	}
	fresh := complexitypanel.New(nil)
	m.seedComplexityTrend(&fresh)
	if len(fresh.TrendData().Points) != 2 {
		t.Fatal("a reopened pane must be seeded")
	}
	// A config reload drops the cache and bumps the generation too.
	m.cxTrend.cache = complexity.NewTrendCache()
	gen := m.cxTrend.gen
	m.rearmComplexityTrend()
	if m.cxTrend.gen != gen+1 {
		t.Fatal("rearm must bump the generation")
	}
	// The walk's telemetry: one complexity.trend op per landed walk.
	m.usage.Close()
	var ops []telemetry.Event
	for _, ev := range cxTelemetryEvents(t, filepath.Join(os.Getenv("IKE_CONFIG_DIR"), "telemetry")) {
		if ev.Type == telemetry.TypeOp && ev.Data["id"] == telemetry.OpComplexityTrend {
			ops = append(ops, ev)
		}
	}
	if len(ops) != 2 {
		t.Fatalf("ops = %d, want the failed and the landed walk", len(ops))
	}
	if d := ops[0].Data; d["phase"] != telemetry.OpPhaseError {
		t.Errorf("first op = %v", d)
	}
	if d := ops[1].Data; d["phase"] != telemetry.OpPhaseOK || d["ms"] != "42" || d["points"] != "2" || d["files"] != "3" {
		t.Errorf("second op = %v", d)
	}
	for _, ev := range ops {
		for k, v := range ev.Data {
			if strings.Contains(v, string(os.PathSeparator)) {
				t.Errorf("op field %s carries a path: %q", k, v)
			}
		}
	}
}

// trendGitRepo builds a repository of three commits: f grows, g is added.
func trendGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	if _, err := complexity.Measure("go", []string{"package p"}); err != nil {
		t.Skip("no Go syntax tree in this build")
	}
	dir := t.TempDir()
	if ev, err := filepath.EvalSymlinks(dir); err == nil {
		dir = ev
	}
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
	run("init", "-q", "-b", "main")
	write("mod.go", "package p\n\nfunc f(a int) int {\n\tif a > 0 {\n\t\treturn 1\n\t}\n\treturn 0\n}\n")
	run("add", ".")
	run("commit", "-qm", "one")
	write("mod.go", "package p\n\nfunc f(a int) int {\n\tif a > 0 {\n\t\treturn 1\n\t}\n\tif a < 0 {\n\t\treturn -1\n\t}\n\treturn 0\n}\n")
	run("commit", "-qam", "two")
	write("more.go", "package p\n\nfunc g(a, b int) int {\n\tif a > b && b > 0 {\n\t\treturn a\n\t}\n\treturn b\n}\n")
	run("add", ".")
	run("commit", "-qm", "three")
	return dir
}

func TestRunComplexityTrendRealRepo(t *testing.T) {
	dir := trendGitRepo(t)
	cache := complexity.NewTrendCache()
	job := complexity.TrendJob{RepoRoot: dir, Root: dir, Cache: cache, Res: complexity.Uniform(complexity.Thresholds{CCN: 2, Cog: 2})}
	var progress []int
	job.Progress = func(done, total int) { progress = append(progress, done) }
	key := cxTrendKey{snapGen: 1, commits: 2, root: dir}
	msg := runComplexityTrend(job, key)
	if msg.err != "" {
		t.Fatal(msg.err)
	}
	// Two commits of three: two and three, oldest first.
	pts := msg.trend.Points
	if len(pts) != 2 || pts[0].Subject != "two" || pts[1].Subject != "three" || msg.key != key {
		t.Fatalf("points = %+v", pts)
	}
	if pts[0].Summary.Functions != 1 || pts[1].Summary.Functions != 2 || pts[1].Summary.Excess <= pts[0].Summary.Excess {
		t.Errorf("summaries = %+v / %+v", pts[0].Summary, pts[1].Summary)
	}
	if msg.trend.Measured != 2 || len(progress) != 3 || progress[0] != 0 || progress[2] != 2 {
		t.Errorf("measured %d progress %v", msg.trend.Measured, progress)
	}
	// The whole history, served from the cache for the two known points.
	key.commits = 30
	again := runComplexityTrend(job, key)
	if again.err != "" || len(again.trend.Points) != 3 || again.trend.Points[0].Subject != "one" || again.trend.Measured != 1 {
		t.Fatalf("again: err %q points %d measured %d", again.err, len(again.trend.Points), again.trend.Measured)
	}
	// The model's job carries the repository root, the project root, the
	// excludes and the cache; applying the result fills the pane.
	m := trendApp(t)
	snap, err := vcs.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.vcs.snap = snap
	m.cxTrend.cache = cache
	mj := m.complexityTrendJob(cxTrendKey{commits: 30, root: dir})
	if mj.RepoRoot != dir || mj.Root != dir || mj.Cache != cache || mj.Res == nil {
		t.Fatalf("job = %+v", mj)
	}
	m.applyComplexityTrend(again)
	if got := m.complexityPanel().Rows(); len(got) != 3 || got[0].Trend.Point.Subject != "three" {
		t.Fatalf("pane rows = %d", len(got))
	}
	// An unknown repository is a git error, not a panic.
	bad := runComplexityTrend(complexity.TrendJob{RepoRoot: t.TempDir()}, cxTrendKey{commits: 5})
	if bad.err == "" {
		t.Fatal("a non-repository must fail")
	}
}
