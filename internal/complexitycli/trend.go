package complexitycli

// trend.go is `ike complexity --trend[=N]` (Roadmap 0551, #2962): the
// project's sums at each of the last N first-parent commits, oldest first,
// as a text table or as `{"commits": N, "points": [...]}` JSON. The walk is
// complexity.WalkTrend over the named paths' selection, the thresholds and
// the test-file rule of the other reports.

import (
	"fmt"
	"io"
	"path/filepath"
	"text/tabwriter"
	"time"

	"ike/internal/complexity"
	"ike/internal/vcs/vcscore"
)

// defaultTrendCommits is the --trend window when neither the flag nor the
// config names one.
const defaultTrendCommits = 30

// trendReport is the measured trend and its settings.
type trendReport struct {
	s     settings
	trend complexity.Trend
}

// runTrend walks the last s.trendCommits commits of the repository holding
// the project, measuring the files the selection keeps.
func runTrend(s settings) (trendReport, error) {
	r := trendReport{s: s}
	repo, err := vcscore.DetectRoot(s.dir)
	if err != nil {
		return r, err
	}
	if ev, err := filepath.EvalSymlinks(repo); err == nil {
		repo = ev
	}
	commits, err := complexity.ListTrendCommits(repo, "", s.trendCommits)
	if err != nil {
		return r, err
	}
	job := complexity.TrendJob{
		RepoRoot: repo,
		Root:     s.dir,
		Res:      s.th,
		Cache:    complexity.NewTrendCache(),
		// The gate of the other reports: inside the selection, not a hidden
		// test file, and not excluded unless named explicitly (#2936).
		Keep: func(abs string) bool {
			if !s.selected(abs) || s.hidesTest(abs) {
				return false
			}
			return s.named(abs) || !complexity.Excluded(s.dir, s.exclude, abs)
		},
	}
	r.trend, err = complexity.WalkTrend(job, commits)
	return r, err
}

// runTrendReport runs the trend and prints it; the exit code is the
// caller's.
func runTrendReport(s settings, stdout, stderr io.Writer) int {
	r, err := runTrend(s)
	if err != nil {
		fmt.Fprintln(stderr, "ike complexity:", err)
		return ExitError
	}
	if s.args.JSON {
		err = writeTrendJSON(stdout, r)
	} else {
		writeTrendText(stdout, r)
	}
	if err != nil {
		fmt.Fprintln(stderr, "ike complexity:", err)
		return ExitError
	}
	return ExitOK
}

// jsonTrendPoint is one commit of the --trend JSON.
type jsonTrendPoint struct {
	Sha     string      `json:"sha"`
	Short   string      `json:"short"`
	Time    string      `json:"time"`
	Subject string      `json:"subject"`
	Summary jsonSummary `json:"summary"`
	// Delta is the change against the previous (older) point; every
	// field is 0 on the oldest.
	Delta jsonTrendDelta `json:"delta"`
}

// jsonTrendDelta is a point's change against the previous one.
type jsonTrendDelta struct {
	Functions     int `json:"functions"`
	CCN           int `json:"ccn"`
	Cognitive     int `json:"cognitive"`
	Excess        int `json:"excess"`
	OverThreshold int `json:"over_threshold"`
}

// jsonTrend is the --trend JSON document.
type jsonTrend struct {
	Commits int              `json:"commits"`
	Points  []jsonTrendPoint `json:"points"`
	Skipped jsonSkipped      `json:"skipped"`
}

// trendDelta is the change of point i against its predecessor.
func trendDelta(pts []complexity.TrendPoint, i int) jsonTrendDelta {
	if i == 0 {
		return jsonTrendDelta{}
	}
	a, b := pts[i].Summary, pts[i-1].Summary
	return jsonTrendDelta{Functions: a.Functions - b.Functions, CCN: a.CCNSum - b.CCNSum, Cognitive: a.CogSum - b.CogSum,
		Excess: a.Excess - b.Excess, OverThreshold: a.OverThreshold - b.OverThreshold}
}

// writeTrendJSON prints the points oldest first with the summary fields of
// the project report per point.
func writeTrendJSON(w io.Writer, r trendReport) error {
	out := jsonTrend{Commits: r.s.trendCommits, Points: make([]jsonTrendPoint, 0, len(r.trend.Points))}
	for i, p := range r.trend.Points {
		out.Points = append(out.Points, jsonTrendPoint{
			Sha: p.Sha, Short: p.Short, Time: p.Time.UTC().Format(time.RFC3339), Subject: p.Subject,
			Summary: toJSONSummary(p.Summary, p.Files, r.s),
			Delta:   trendDelta(r.trend.Points, i),
		})
	}
	return writeJSON(w, out)
}

// writeTrendText prints the header, one row per commit oldest first with
// the sums and their change against the previous row, and the window's
// totals.
func writeTrendText(w io.Writer, r trendReport) {
	pts := r.trend.Points
	fmt.Fprintf(w, "complexity trend: last %d %s (first parent)\n", len(pts), plural(len(pts), "commit", "commits"))
	if len(pts) == 0 {
		fmt.Fprintln(w, "\nno commit to measure")
		return
	}
	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "COMMIT\tDATE\tSUBJECT\tFUNCS\tCCN\tCOG\tEXCESS\tOVER")
	for i, p := range pts {
		s := p.Summary
		d := trendDelta(pts, i)
		cell := func(v, delta int) string {
			if i == 0 {
				return fmt.Sprintf("%d", v)
			}
			return fmt.Sprintf("%d (%s)", v, signed(delta))
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\n", p.Short, p.Time.Format("2006-01-02"), p.Subject, s.Functions,
			cell(s.CCNSum, d.CCN), cell(s.CogSum, d.Cognitive), cell(s.Excess, d.Excess), cell(s.OverThreshold, d.OverThreshold))
	}
	_ = tw.Flush()
	o, n := r.trend.Oldest().Summary, r.trend.Newest().Summary
	fmt.Fprintf(w, "\ntotals: %s → %s: ΣCCN %d → %d (%s), Σcog %d → %d (%s), excess %d → %d (%s), over %d → %d (%s)\n",
		r.trend.Oldest().Short, r.trend.Newest().Short,
		o.CCNSum, n.CCNSum, signed(n.CCNSum-o.CCNSum),
		o.CogSum, n.CogSum, signed(n.CogSum-o.CogSum),
		o.Excess, n.Excess, signed(n.Excess-o.Excess),
		o.OverThreshold, n.OverThreshold, signed(n.OverThreshold-o.OverThreshold))
	if r.s.args.NoTests {
		fmt.Fprintln(w, "tests hidden (--no-tests)")
	}
}
