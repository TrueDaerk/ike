package complexitycli

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"text/tabwriter"

	"ike/internal/complexity"
)

// render.go holds the two renderings of both reports. The JSON field names
// are a stable interface documented in userdocs/guides/complexity.md: add
// fields, never rename or drop one.

// --- JSON shapes ---

// jsonSummary is a complexity.Summary in the CLI's snake_case spelling plus
// the file count and the thresholds it was taken at.
type jsonSummary struct {
	Files         int     `json:"files"`
	Functions     int     `json:"functions"`
	NLOC          int     `json:"nloc"`
	CCNSum        int     `json:"ccn_sum"`
	CCNAvg        float64 `json:"ccn_avg"`
	CCNMax        int     `json:"ccn_max"`
	CogSum        int     `json:"cog_sum"`
	CogAvg        float64 `json:"cog_avg"`
	CogMax        int     `json:"cog_max"`
	OverThreshold int     `json:"over_threshold"`
	// Excess is the mass above the CCN and cognitive thresholds (#2945).
	Excess  int `json:"excess"`
	CCNWarn int `json:"ccn_warn"`
	CogWarn int `json:"cog_warn"`
	// The comment density (#2934): summed comment-only lines, the
	// aggregate ratio and the threshold in percent (0 = off).
	CommentLines   int     `json:"comment_lines"`
	CommentDensity float64 `json:"comment_density"`
	CmtWarn        int     `json:"cmt_warn"`
}

func toJSONSummary(s complexity.Summary, files int, st settings) jsonSummary {
	return jsonSummary{
		Files: files, Functions: s.Functions, NLOC: s.NLOC,
		CCNSum: s.CCNSum, CCNAvg: round2(s.CCNAvg), CCNMax: s.CCNMax,
		CogSum: s.CogSum, CogAvg: round2(s.CogAvg), CogMax: s.CogMax,
		OverThreshold: s.OverThreshold, Excess: s.Excess, CCNWarn: st.ccnWarn, CogWarn: st.cogWarn,
		CommentLines: s.CommentLines, CommentDensity: ratio(s.CommentDensity), CmtWarn: st.cmtWarn,
	}
}

func round2(f float64) float64 { return float64(int64(f*100+0.5)) / 100 }

// ratio rounds a comment density (or its delta, which may be negative) to
// four decimals: 0.5556.
func ratio(f float64) float64 { return math.Round(f*10000) / 10000 }

// jsonFunction is one function; line numbers are 1-based.
type jsonFunction struct {
	File      string `json:"file"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Line      int    `json:"line"`
	EndLine   int    `json:"end_line"`
	CCN       int    `json:"ccn"`
	Cognitive int    `json:"cognitive"`
	NLOC      int    `json:"nloc"`
	Params    int    `json:"params"`
	Depth     int    `json:"depth"`
	// CommentLines / CommentDensity (#2934): the body's comment-only lines
	// and their share as a ratio (0.55).
	CommentLines   int     `json:"comment_lines"`
	CommentDensity float64 `json:"comment_density"`
}

func toJSONFunction(file string, f complexity.Function) jsonFunction {
	return jsonFunction{File: file, Name: f.Name, Kind: f.Kind, Line: f.Line + 1, EndLine: f.EndLine + 1,
		CCN: f.CCN, Cognitive: f.Cognitive, NLOC: f.NLOC, Params: f.Params, Depth: f.Depth,
		CommentLines: f.CommentLines, CommentDensity: ratio(f.CommentDensity)}
}

// jsonMetrics is one side of a changed function.
type jsonMetrics struct {
	CCN       int `json:"ccn"`
	Cognitive int `json:"cognitive"`
	NLOC      int `json:"nloc"`
	Params    int `json:"params"`
	Depth     int `json:"depth"`
	// The comment density (#2934), as on jsonFunction.
	CommentLines   int     `json:"comment_lines"`
	CommentDensity float64 `json:"comment_density"`
}

func toJSONMetrics(f complexity.Function) jsonMetrics {
	return jsonMetrics{CCN: f.CCN, Cognitive: f.Cognitive, NLOC: f.NLOC, Params: f.Params, Depth: f.Depth,
		CommentLines: f.CommentLines, CommentDensity: ratio(f.CommentDensity)}
}

// jsonDelta is after − before.
type jsonDelta struct {
	CCN       int `json:"ccn"`
	Cognitive int `json:"cognitive"`
	NLOC      int `json:"nloc,omitempty"`
	// Excess is the delta above the CCN and cognitive thresholds (#2945),
	// always present.
	Excess int `json:"excess"`
	// The comment density delta (#2934): lines and ratio, always present.
	CommentLines   int     `json:"comment_lines"`
	CommentDensity float64 `json:"comment_density"`
}

// jsonChange is one changed function: the head position, both sides and
// the delta.
type jsonChange struct {
	File   string      `json:"file"`
	Name   string      `json:"name"`
	Kind   string      `json:"kind"`
	Line   int         `json:"line"`
	Before jsonMetrics `json:"before"`
	After  jsonMetrics `json:"after"`
	Delta  jsonDelta   `json:"delta"`
}

// jsonSkipped reports the files a language claims but no spec measures.
type jsonSkipped struct {
	Files     int      `json:"files"`
	Languages []string `json:"languages"`
}

func toJSONSkipped(k skipped) jsonSkipped {
	return jsonSkipped{Files: k.files, Languages: k.languages()}
}

type jsonProject struct {
	Summary   jsonSummary    `json:"summary"`
	Functions []jsonFunction `json:"functions"`
	Skipped   jsonSkipped    `json:"skipped"`
}

// jsonGroup is one class or file group (#2937): its key and the aggregate
// under the field names jsonSummary uses.
type jsonGroup struct {
	File           string  `json:"file"`
	Class          string  `json:"class,omitempty"`
	Functions      int     `json:"functions"`
	NLOC           int     `json:"nloc"`
	CCNSum         int     `json:"ccn_sum"`
	CCNAvg         float64 `json:"ccn_avg"`
	CCNMax         int     `json:"ccn_max"`
	CogSum         int     `json:"cog_sum"`
	CogAvg         float64 `json:"cog_avg"`
	CogMax         int     `json:"cog_max"`
	OverThreshold  int     `json:"over_threshold"`
	Excess         int     `json:"excess"`
	CommentLines   int     `json:"comment_lines"`
	CommentDensity float64 `json:"comment_density"`
}

func toJSONGroup(g complexity.Group) jsonGroup {
	s := g.Summary
	return jsonGroup{File: g.File, Class: g.Class, Functions: s.Functions, NLOC: s.NLOC,
		CCNSum: s.CCNSum, CCNAvg: round2(s.CCNAvg), CCNMax: s.CCNMax,
		CogSum: s.CogSum, CogAvg: round2(s.CogAvg), CogMax: s.CogMax,
		OverThreshold: s.OverThreshold, Excess: s.Excess, CommentLines: s.CommentLines, CommentDensity: ratio(s.CommentDensity)}
}

// jsonGroups is the grouped project report: the functions array gives way
// to the groups.
type jsonGroups struct {
	Summary jsonSummary `json:"summary"`
	Groups  []jsonGroup `json:"groups"`
	Skipped jsonSkipped `json:"skipped"`
}

type jsonDiff struct {
	Rev     string         `json:"rev"`
	Sha     string         `json:"sha"`
	Base    jsonSummary    `json:"base"`
	Head    jsonSummary    `json:"head"`
	Delta   jsonDelta      `json:"delta"`
	Changed []jsonChange   `json:"changed"`
	Added   []jsonFunction `json:"added"`
	Removed []jsonFunction `json:"removed"`
	Verdict string         `json:"verdict"`
	Skipped jsonSkipped    `json:"skipped"`
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func writeProjectJSON(w io.Writer, r projectReport) error {
	out := jsonProject{
		Summary:   toJSONSummary(r.summary, r.files, r.s),
		Functions: make([]jsonFunction, 0, len(r.rows)),
		Skipped:   toJSONSkipped(r.skipped),
	}
	for _, rw := range r.rows {
		out.Functions = append(out.Functions, toJSONFunction(rw.file, rw.fn))
	}
	return writeJSON(w, out)
}

func writeGroupsJSON(w io.Writer, r groupReport) error {
	out := jsonGroups{
		Summary: toJSONSummary(r.summary, r.files, r.s),
		Groups:  make([]jsonGroup, 0, len(r.groups)),
		Skipped: toJSONSkipped(r.skipped),
	}
	for _, g := range r.groups {
		out.Groups = append(out.Groups, toJSONGroup(g))
	}
	return writeJSON(w, out)
}

func writeDiffJSON(w io.Writer, r diffReport) error {
	changed, added, removed := r.sections()
	out := jsonDiff{
		Rev: r.rev, Sha: r.sha,
		Base: toJSONSummary(r.diff.Base, r.baseFiles, r.s),
		Head: toJSONSummary(r.diff.Head, r.headFiles, r.s),
		Delta: jsonDelta{CCN: r.diff.DeltaCCN(), Cognitive: r.diff.DeltaCognitive(), Excess: r.diff.DeltaExcess(),
			CommentLines:   r.diff.Head.CommentLines - r.diff.Base.CommentLines,
			CommentDensity: ratio(r.diff.Head.CommentDensity - r.diff.Base.CommentDensity)},
		Changed: make([]jsonChange, 0, len(changed)),
		Added:   make([]jsonFunction, 0, len(added)),
		Removed: make([]jsonFunction, 0, len(removed)),
		Verdict: verdictID(r.diff.Verdict),
		Skipped: toJSONSkipped(r.skipped),
	}
	for _, c := range changed {
		out.Changed = append(out.Changed, jsonChange{
			File: c.file, Name: c.ch.After.Name, Kind: c.ch.After.Kind, Line: c.ch.After.Line + 1,
			Before: toJSONMetrics(c.ch.Before), After: toJSONMetrics(c.ch.After),
			Delta: jsonDelta{CCN: c.ch.DeltaCCN(), Cognitive: c.ch.DeltaCognitive(), NLOC: c.ch.DeltaNLOC(),
				Excess:       c.ch.After.Excess(r.s.ccnWarn, r.s.cogWarn) - c.ch.Before.Excess(r.s.ccnWarn, r.s.cogWarn),
				CommentLines: c.ch.DeltaCommentLines(), CommentDensity: ratio(c.ch.DeltaCommentDensity())},
		})
	}
	for _, rw := range added {
		out.Added = append(out.Added, toJSONFunction(rw.file, rw.fn))
	}
	for _, rw := range removed {
		out.Removed = append(out.Removed, toJSONFunction(rw.file, rw.fn))
	}
	return writeJSON(w, out)
}

// --- text ---

// writeProjectText prints the table (CCN descending), the summary line and
// the skipped line.
func writeProjectText(w io.Writer, r projectReport) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FILE\tFUNCTION\tLINE\tCCN\tCOG\tNLOC\tPARAMS\tCMT%")
	for _, rw := range r.rows {
		f := rw.fn
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\n", rw.file, f.Name, f.Line+1, f.CCN, f.Cognitive, f.NLOC, f.Params,
			complexity.DensityPercent(f.CommentDensity))
	}
	_ = tw.Flush()
	r.writeSummaryLine(w)
	if r.s.filtered() {
		fmt.Fprintf(w, "shown: %d of %d functions (%s)\n", len(r.rows), r.summary.Functions, r.s.filterText())
	}
	if l := r.skipped.line(); l != "" {
		fmt.Fprintln(w, l)
	}
}

// writeSummaryLine prints the blank line and the whole-project summary
// both project tables end with.
func (r projectReport) writeSummaryLine(w io.Writer) {
	s := r.summary
	fmt.Fprintf(w, "\nsummary: %d functions in %d files · ΣCCN %d (avg %.1f, max %d) · Σcog %d (avg %.1f, max %d) · NLOC %d · cmt %d%% · %d over threshold (%s) · excess %d\n",
		s.Functions, r.files, s.CCNSum, s.CCNAvg, s.CCNMax, s.CogSum, s.CogAvg, s.CogMax, s.NLOC,
		complexity.DensityPercent(s.CommentDensity), s.OverThreshold, r.s.thresholdText(), s.Excess)
}

// writeGroupsText prints the group table (#2937, CCN sum descending), the
// summary line and the skipped line; the CLASS column only per class.
func writeGroupsText(w io.Writer, r groupReport) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	byClass := r.by == complexity.ByClass
	if byClass {
		fmt.Fprint(tw, "FILE\tCLASS\t")
	} else {
		fmt.Fprint(tw, "FILE\t")
	}
	fmt.Fprintln(tw, "FUNCS\tNLOC\tCCN\tCCN_AVG\tCCN_MAX\tCOG\tCOG_AVG\tCOG_MAX\tCMT%\tOVER\tEXCESS")
	for _, g := range r.groups {
		if byClass {
			fmt.Fprintf(tw, "%s\t%s\t", g.File, g.Class)
		} else {
			fmt.Fprintf(tw, "%s\t", g.File)
		}
		fmt.Fprintf(tw, "%d\t%d\t%d\t%.1f\t%d\t%d\t%.1f\t%d\t%d\t%d\t%d\n", g.Functions, g.NLOC,
			g.CCNSum, g.CCNAvg, g.CCNMax, g.CogSum, g.CogAvg, g.CogMax,
			complexity.DensityPercent(g.CommentDensity), g.OverThreshold, g.Excess)
	}
	_ = tw.Flush()
	r.writeSummaryLine(w)
	if r.s.filtered() {
		noun := "files"
		if byClass {
			noun = "classes"
		}
		fmt.Fprintf(w, "shown: %d of %d %s (%s)\n", len(r.groups), r.total, noun, r.s.groupFilterText())
	}
	if l := r.skipped.line(); l != "" {
		fmt.Fprintln(w, l)
	}
}

// writeBriefText prints the --brief rendering (#2938): one aligned line per
// group kept — path, class (per class only), funcs, nloc, the CCN and
// cognitive sums, max CCN / max cognitive and the comment density — with no
// header, summary, shown or skipped line.
func writeBriefText(w io.Writer, r groupReport) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, g := range r.groups {
		if r.by == complexity.ByClass {
			fmt.Fprintf(tw, "%s\t%s\t", g.File, g.Class)
		} else {
			fmt.Fprintf(tw, "%s\t", g.File)
		}
		fmt.Fprintf(tw, "funcs=%d\tnloc=%d\tccn=%d\tcog=%d\tmax=%d/%d\tcmt=%d%%\n", g.Functions, g.NLOC,
			g.CCNSum, g.CogSum, g.CCNMax, g.CogMax, complexity.DensityPercent(g.CommentDensity))
	}
	_ = tw.Flush()
}

// groupFilterText describes the active group filter: the sums for --min,
// the maxima and the aggregate density for the thresholds.
func (s settings) groupFilterText() string {
	if s.args.Min > 0 {
		return fmt.Sprintf("ΣCCN or Σcog ≥ %d", s.args.Min)
	}
	t := fmt.Sprintf("max CCN ≥ %d or max cog ≥ %d", s.ccnWarn, s.cogWarn)
	if s.cmtWarn > 0 {
		t += fmt.Sprintf(" or cmt ≥ %d%%", s.cmtWarn)
	}
	return t
}

// filterText describes the active row filter.
func (s settings) filterText() string {
	if s.args.Min > 0 {
		return fmt.Sprintf("CCN or cog ≥ %d", s.args.Min)
	}
	return s.thresholdText()
}

// thresholdText names the warning thresholds: "CCN ≥ 15 or cog ≥ 15 or
// cmt ≥ 40%", the comment density left out while it is off.
func (s settings) thresholdText() string {
	t := fmt.Sprintf("CCN ≥ %d or cog ≥ %d", s.ccnWarn, s.cogWarn)
	if s.cmtWarn > 0 {
		t += fmt.Sprintf(" or cmt ≥ %d%%", s.cmtWarn)
	}
	return t
}

// writeDiffText prints the changed / added / removed sections, the totals
// and the verdict line.
func writeDiffText(w io.Writer, r diffReport) {
	fmt.Fprintf(w, "complexity vs %s (%s)\n", r.rev, shortSha(r.sha))
	changed, added, removed := r.sections()
	if len(changed)+len(added)+len(removed) == 0 {
		fmt.Fprintln(w, "\nno function changed its complexity")
	}
	if len(changed) > 0 {
		fmt.Fprintf(w, "\nchanged (%d):\n", len(changed))
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, c := range changed {
			b, a := c.ch.Before, c.ch.After
			bc, ac := complexity.DensityPercent(b.CommentDensity), complexity.DensityPercent(a.CommentDensity)
			fmt.Fprintf(tw, "  %s\t%s\tL%d\tCCN %d→%d (%s)\tcog %d→%d (%s)\tNLOC %d→%d (%s)\tcmt %d%%→%d%% (%s)\n",
				c.file, a.Name, a.Line+1,
				b.CCN, a.CCN, signed(c.ch.DeltaCCN()),
				b.Cognitive, a.Cognitive, signed(c.ch.DeltaCognitive()),
				b.NLOC, a.NLOC, signed(c.ch.DeltaNLOC()),
				bc, ac, signed(ac-bc))
		}
		_ = tw.Flush()
	}
	section := func(title string, rows []row) {
		if len(rows) == 0 {
			return
		}
		fmt.Fprintf(w, "\n%s (%d):\n", title, len(rows))
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, rw := range rows {
			f := rw.fn
			fmt.Fprintf(tw, "  %s\t%s\tL%d\tCCN %d\tcog %d\tNLOC %d\tcmt %d%%\n", rw.file, f.Name, f.Line+1, f.CCN, f.Cognitive, f.NLOC,
				complexity.DensityPercent(f.CommentDensity))
		}
		_ = tw.Flush()
	}
	section("added", added)
	section("removed", removed)
	b, h := r.diff.Base, r.diff.Head
	fmt.Fprintf(w, "\ntotals: before %d functions, ΣCCN %d, Σcog %d, excess %d → after %d functions, ΣCCN %d, Σcog %d, excess %d\n",
		b.Functions, b.CCNSum, b.CogSum, b.Excess, h.Functions, h.CCNSum, h.CogSum, h.Excess)
	fmt.Fprintf(w, "verdict: %s (excess %s, Σcog %s, ΣCCN %s)\n", verdictText(r.diff.Verdict),
		signed(r.diff.DeltaExcess()), signed(r.diff.DeltaCognitive()), signed(r.diff.DeltaCCN()))
	if l := r.skipped.line(); l != "" {
		fmt.Fprintln(w, l)
	}
}

// verdictText is the upper-case verdict of the text report.
func verdictText(v complexity.Verdict) string {
	switch v {
	case complexity.MoreComplex:
		return "MORE COMPLEX"
	case complexity.LessComplex:
		return "LESS COMPLEX"
	}
	return "UNCHANGED"
}

// signed renders n with an explicit sign: +4, -2, +0.
func signed(n int) string { return fmt.Sprintf("%+d", n) }

func shortSha(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
