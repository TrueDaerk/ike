package app

import (
	"strconv"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/complexitypanel"
	"ike/internal/config"
	"ike/internal/safego"
	"ike/internal/telemetry"
)

// complexity_trend.go wires the Trend tab of the Complexity window
// (Roadmap 0551, #2962): the project's sums at each of the last
// complexity.trend_commits first-parent commits. The walk —
// complexity.ListTrendCommits + complexity.WalkTrend, one tree read for
// the oldest point and one blob diff per later point — runs off the Update
// goroutine; Update only books generations, relays the walk's progress to
// the pane every tick while it runs and hands the finished
// complexitypanel.Trend over. Like the Changes tab it is pulled, not
// pushed: once per settled Update pass, while the Trend tab is on screen,
// the model compares the key of the trend the pane shows — (VCS snapshot
// generation, window, project root, rebuild generation) — with the current
// one and launches the walk when they differ. The per-blob and per-commit
// cache (complexity.TrendCache) makes a re-walk after a commit cost the new
// head's changed files only; 'r' and a config reload drop the cache and
// walk afresh.

// cxTrendTick is how often the pane's "computing… 12/30" follows the walk.
const cxTrendTick = 200 * time.Millisecond

// cxTrendKey identifies the inputs a trend was computed from.
type cxTrendKey struct {
	snapGen int    // bumped per distinct VCS snapshot (a commit moves it)
	commits int    // complexity.trend_commits
	root    string // the project root
	gen     int    // bumped by 'r' and a config reload
}

// complexityTrendState is the session state behind the Trend tab, shared
// by pointer across value-model copies like cxDiff.
type complexityTrendState struct {
	key       cxTrendKey // the inputs of the result the pane shows
	pending   cxTrendKey // the inputs of the walk in flight
	gen       int
	computing bool
	result    complexitypanel.Trend
	cache     *complexity.TrendCache
	// progress is the walk's "done" count, written by the walk goroutine
	// and read by the tick; total its point count.
	progress *atomic.Int64
	total    int
	totalCh  chan int
}

// complexityTrendMsg carries one finished walk (or its failure).
type complexityTrendMsg struct {
	key   cxTrendKey
	trend complexity.Trend
	err   string
	ms    int64
}

// complexityTrendTickMsg relays the running walk's progress to the pane.
type complexityTrendTickMsg struct{ key cxTrendKey }

// trendCommits is the configured window, the default for a config that
// has none (a bare test model).
func trendCommits() int {
	if c := config.Get(); c != nil && c.Complexity.TrendCommits > 0 {
		return c.Complexity.TrendCommits
	}
	return 30
}

// currentCxTrendKey is the key a walk launched now would carry.
func (m Model) currentCxTrendKey() cxTrendKey {
	k := cxTrendKey{commits: trendCommits()}
	if m.cxDiff != nil {
		k.snapGen = m.cxDiff.snapGen
	}
	if m.complexityIndex != nil {
		k.root = m.complexityIndex.Root()
	}
	if m.cxTrend != nil {
		k.gen = m.cxTrend.gen
	}
	return k
}

// rearmComplexityTrend asks for a fresh walk on the next settled pass ('r',
// a config reload): the cache is dropped so the thresholds and excludes it
// measured under do not survive, and the key moves so the pass launches.
func (m Model) rearmComplexityTrend() {
	if m.cxTrend == nil {
		return
	}
	m.cxTrend.gen++
	m.cxTrend.cache.Reset()
}

// complexityTrendWanted reports whether anything consumes the trend: the
// Trend tab on screen.
func (m Model) complexityTrendWanted() bool {
	if m.cxTrend == nil {
		return false
	}
	p := m.complexityPanel()
	return p != nil && p.Tab() == complexitypanel.TabTrend
}

// complexityTrendSyncCmd runs once per settled Update pass: while the
// Trend tab is on screen inside a repository and the pane's trend is behind
// the current inputs, it launches one walk off the loop. Outside a
// repository the tab is told so and nothing runs.
func (m *Model) complexityTrendSyncCmd() tea.Cmd {
	if !m.complexityTrendWanted() {
		return nil
	}
	st := m.cxTrend
	p := m.complexityPanel()
	if m.vcs == nil || m.vcs.snap == nil {
		if !st.result.NotGit {
			st.result = complexitypanel.Trend{NotGit: true, Window: trendCommits()}
			st.key = cxTrendKey{}
			p.SetTrend(st.result)
		}
		return nil
	}
	if st.result.NotGit {
		st.result = complexitypanel.Trend{Window: trendCommits()}
		p.SetTrend(st.result)
	}
	key := m.currentCxTrendKey()
	if st.computing || key == st.key || key.root == "" {
		return nil
	}
	return m.startComplexityTrend(key)
}

// startComplexityTrend launches the walk for key: the pane shows the
// computing state, the walk runs in its own goroutine feeding the progress
// counter, one command delivers its result and a tick relays the progress
// meanwhile.
func (m *Model) startComplexityTrend(key cxTrendKey) tea.Cmd {
	st := m.cxTrend
	st.computing, st.pending = true, key
	st.progress, st.total = new(atomic.Int64), 0
	st.result.Computing, st.result.Done, st.result.Total, st.result.Err = true, 0, 0, ""
	st.result.Window = key.commits
	if p := m.complexityPanel(); p != nil {
		p.SetTrend(st.result)
	}
	if st.cache == nil {
		st.cache = complexity.NewTrendCache()
	}
	job := m.complexityTrendJob(key)
	progress := st.progress
	total := make(chan int, 1)
	job.Progress = func(done, n int) {
		progress.Store(int64(done))
		select {
		case total <- n:
		default:
		}
	}
	done := make(chan complexityTrendMsg, 1)
	safego.Go("app.complexityTrend.walk", func() { done <- runComplexityTrend(job, key) })
	st.totalCh = total
	return tea.Batch(
		func() tea.Msg { return <-done },
		tickCmd(cxTrendTick, func(time.Time) tea.Msg { return complexityTrendTickMsg{key: key} }),
	)
}

// complexityTrendJob snapshots the model state the walk reads: the
// repository and project roots, the exclude globs, the thresholds and the
// session cache.
func (m Model) complexityTrendJob(key cxTrendKey) complexity.TrendJob {
	job := complexity.TrendJob{
		RepoRoot: m.vcs.snap.Root,
		Root:     key.root,
		Res:      m.complexityResolver(config.Get().Complexity),
	}
	if m.cxTrend != nil {
		job.Cache = m.cxTrend.cache
	}
	if m.complexityIndex != nil {
		job.Exclude = m.complexityIndex.Options().Exclude
	}
	return job
}

// runComplexityTrend is the off-loop walk: list the last key.commits
// first-parent commits and measure the project at each. Every git call
// carries its package timeout; a failure is the message's err.
func runComplexityTrend(job complexity.TrendJob, key cxTrendKey) complexityTrendMsg {
	start := time.Now()
	msg := complexityTrendMsg{key: key}
	commits, err := complexity.ListTrendCommits(job.RepoRoot, "", key.commits)
	if err == nil {
		if job.Progress != nil {
			job.Progress(0, len(commits))
		}
		msg.trend, err = complexity.WalkTrend(job, commits)
	}
	if err != nil {
		msg.err = err.Error()
	}
	msg.ms = time.Since(start).Milliseconds()
	return msg
}

// complexityTrendProgress runs on the tick: while the walk for key is still
// in flight it pushes the progress into the pane and re-arms the tick; a
// finished or superseded walk ends the chain.
func (m *Model) complexityTrendProgress(msg complexityTrendTickMsg) tea.Cmd {
	st := m.cxTrend
	if st == nil || !st.computing || st.pending != msg.key {
		return nil
	}
	select {
	case n := <-st.totalCh:
		st.total = n
	default:
	}
	done := int(st.progress.Load())
	if done != st.result.Done || st.total != st.result.Total {
		st.result.Done, st.result.Total = done, st.total
		if p := m.complexityPanel(); p != nil {
			p.SetTrend(st.result)
		}
	}
	return tickCmd(cxTrendTick, func(time.Time) tea.Msg { return complexityTrendTickMsg{key: msg.key} })
}

// applyComplexityTrend stores a finished walk: on success the pane shows
// it and the key settles; on a git error the previous points stay and the
// message shows inline. A superseded result (the key moved while it ran)
// is applied too — the settled pass that follows launches the fresh walk.
func (m *Model) applyComplexityTrend(msg complexityTrendMsg) {
	st := m.cxTrend
	if st == nil {
		return
	}
	st.computing = false
	st.key = msg.key
	st.result.Computing, st.result.Done, st.result.Total = false, 0, 0
	phase := telemetry.OpPhaseOK
	if msg.err != "" {
		st.result.Err = msg.err
		phase = telemetry.OpPhaseError
	} else {
		st.result = complexitypanel.Trend{Points: msg.trend.Points, Window: msg.key.commits, Ready: true}
	}
	if m.usage != nil {
		m.usage.Op(telemetry.OpComplexityTrend, phase, map[string]string{
			"ms":     strconv.FormatInt(msg.ms, 10),
			"points": strconv.Itoa(len(msg.trend.Points)),
			"files":  strconv.Itoa(msg.trend.Measured),
		})
	}
	if p := m.complexityPanel(); p != nil {
		p.SetTrend(st.result)
	}
}

// setComplexityTrendBase handles the Trend tab's enter: the Changes tab
// compares the commit against HEAD ("<sha>..HEAD", the 'R' path of #2961)
// and comes to the front.
func (m *Model) setComplexityTrendBase(rev string) {
	if rev == "" {
		return
	}
	m.setComplexityBase(rev + "..HEAD")
	if p := m.complexityPanel(); p != nil {
		p.SetTab(complexitypanel.TabChanges)
	}
}

// seedComplexityTrend hands a (re)opened pane the Trend data the session
// holds, so a layout restore does not start from an empty tab.
func (m *Model) seedComplexityTrend(p *complexitypanel.Model) {
	if p != nil && m.cxTrend != nil {
		p.SetTrend(m.cxTrend.result)
	}
}

// ComplexityTrend exposes the Trend tab's data (tests).
func (m Model) ComplexityTrend() complexitypanel.Trend {
	if m.cxTrend == nil {
		return complexitypanel.Trend{}
	}
	return m.cxTrend.result
}
