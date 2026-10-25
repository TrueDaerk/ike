package complexitycli

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"time"

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
	// TestsHidden is set under --no-tests (#2950).
	TestsHidden bool `json:"tests_hidden,omitempty"`
	// ChurnDays is the --churn window in days (#2957), absent without it.
	ChurnDays int `json:"churn_days,omitempty"`
	// Accepted counts the over-threshold functions the baseline accepts
	// (#2958); OverThreshold counts only the new and worsened ones.
	Accepted int `json:"accepted"`
	// Hidden counts the functions of the hidden set (#3031) — left out of
	// `functions` and every total unless --show-hidden listed them; absent
	// when nothing is hidden.
	Hidden int `json:"hidden,omitempty"`
	// The nearest-rank percentiles and the 1–5 / 6–10 / 11–15 / 16–25 /
	// 26+ histogram of both metrics (#2953).
	CCNP50    int           `json:"ccn_p50"`
	CCNP90    int           `json:"ccn_p90"`
	CCNP99    int           `json:"ccn_p99"`
	CogP50    int           `json:"cog_p50"`
	CogP90    int           `json:"cog_p90"`
	CogP99    int           `json:"cog_p99"`
	Histogram jsonHistogram `json:"histogram"`
}

// jsonHistogram is the summary's per-metric bucket counts.
type jsonHistogram struct {
	CCN       [complexity.HistBucketCount]int `json:"ccn"`
	Cognitive [complexity.HistBucketCount]int `json:"cognitive"`
}

func toJSONSummary(s complexity.Summary, files int, st settings, hidden int) jsonSummary {
	return jsonSummary{
		Files: files, Functions: s.Functions, NLOC: s.NLOC,
		CCNSum: s.CCNSum, CCNAvg: round2(s.CCNAvg), CCNMax: s.CCNMax,
		CogSum: s.CogSum, CogAvg: round2(s.CogAvg), CogMax: s.CogMax,
		OverThreshold: s.OverThreshold, Excess: s.Excess, CCNWarn: st.ccnWarn, CogWarn: st.cogWarn,
		CommentLines: s.CommentLines, CommentDensity: ratio(s.CommentDensity), CmtWarn: st.cmtWarn,
		TestsHidden: st.args.NoTests,
		ChurnDays:   st.churnShown(),
		Accepted:    s.Accepted,
		Hidden:      hidden,
		CCNP50:      s.CCNP50, CCNP90: s.CCNP90, CCNP99: s.CCNP99,
		CogP50: s.CogP50, CogP90: s.CogP90, CogP99: s.CogP99,
		Histogram: jsonHistogram{CCN: s.CCNHist, Cognitive: s.CogHist},
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
	// Sites is the per-site breakdown (#2946), present under --sites only;
	// Candidates the extraction candidates (#2948), under the same flag.
	Sites      []jsonSite      `json:"sites,omitempty"`
	Candidates []jsonCandidate `json:"candidates,omitempty"`
	// Commits is the file's commit count in the --churn window (#2957),
	// absent without the flag.
	Commits *int `json:"commits,omitempty"`
	// Baseline is "accepted", "worsened" or "new" against the baseline in
	// effect (#2958), absent without one.
	Baseline string `json:"baseline,omitempty"`
	// Pinned is the function's pin (#2959): the metrics it was pinned at
	// and when; absent on an unpinned function.
	Pinned *jsonPinned `json:"pinned,omitempty"`
	// Hidden marks a function of the hidden set listed by --show-hidden
	// (#3031); absent otherwise.
	Hidden bool `json:"hidden,omitempty"`
}

// jsonPinned is a pin's recorded side.
type jsonPinned struct {
	CCN       int       `json:"ccn"`
	Cognitive int       `json:"cognitive"`
	PinnedAt  time.Time `json:"pinned_at"`
}

// toJSONPinned converts a pin; nil (omitted) for none.
func toJSONPinned(p *complexity.Pin) *jsonPinned {
	if p == nil {
		return nil
	}
	return &jsonPinned{CCN: p.CCN, Cognitive: p.Cognitive, PinnedAt: p.PinnedAt}
}

// jsonSite is one complexity.Site; line numbers are 1-based, the column
// 0-based bytes like the engine's.
type jsonSite struct {
	Line      int    `json:"line"`
	Col       int    `json:"col"`
	EndLine   int    `json:"end_line"`
	Kind      string `json:"kind"`
	CCN       int    `json:"ccn"`
	Cognitive int    `json:"cognitive"`
	Nesting   int    `json:"nesting"`
	Level     int    `json:"level"`
}

// toJSONSites converts f's sites when wanted; nil (omitted) otherwise.
func toJSONSites(f complexity.Function, wanted bool) []jsonSite {
	if !wanted || len(f.Sites) == 0 {
		return nil
	}
	out := make([]jsonSite, len(f.Sites))
	for i, s := range f.Sites {
		out[i] = jsonSite{Line: s.Line + 1, Col: s.Col, EndLine: s.EndLine + 1, Kind: s.Kind,
			CCN: s.CCN, Cognitive: s.Cognitive, Nesting: s.Nesting, Level: s.Level}
	}
	return out
}

// jsonCandidate is one complexity.Candidate (#2948); line numbers are
// 1-based.
type jsonCandidate struct {
	Line      int    `json:"line"`
	EndLine   int    `json:"end_line"`
	Kind      string `json:"kind"`
	CogSaving int    `json:"cog_saving"`
	NewCCN    int    `json:"new_ccn"`
	NewCog    int    `json:"new_cog"`
	RestCCN   int    `json:"rest_ccn"`
	RestCog   int    `json:"rest_cog"`
}

// toJSONCandidates converts f's extraction candidates when wanted; nil
// (omitted) otherwise.
func toJSONCandidates(f complexity.Function, wanted bool) []jsonCandidate {
	if !wanted || len(f.Candidates) == 0 {
		return nil
	}
	out := make([]jsonCandidate, len(f.Candidates))
	for i, c := range f.Candidates {
		out[i] = jsonCandidate{Line: c.Line + 1, EndLine: c.EndLine + 1, Kind: c.Kind, CogSaving: c.CogSaving,
			NewCCN: c.NewCCN, NewCog: c.NewCog, RestCCN: c.RestCCN, RestCog: c.RestCog}
	}
	return out
}

func toJSONFunction(file string, f complexity.Function, sites bool) jsonFunction {
	return jsonFunction{File: file, Name: f.Name, Kind: f.Kind, Line: f.Line + 1, EndLine: f.EndLine + 1,
		CCN: f.CCN, Cognitive: f.Cognitive, NLOC: f.NLOC, Params: f.Params, Depth: f.Depth,
		CommentLines: f.CommentLines, CommentDensity: ratio(f.CommentDensity),
		Sites: toJSONSites(f, sites), Candidates: toJSONCandidates(f, sites)}
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
	// Sites is the after side's breakdown (#2946) and Candidates its
	// extraction candidates (#2948), under --sites only.
	Sites      []jsonSite      `json:"sites,omitempty"`
	Candidates []jsonCandidate `json:"candidates,omitempty"`
}

func toJSONMetrics(f complexity.Function, sites bool) jsonMetrics {
	return jsonMetrics{CCN: f.CCN, Cognitive: f.Cognitive, NLOC: f.NLOC, Params: f.Params, Depth: f.Depth,
		CommentLines: f.CommentLines, CommentDensity: ratio(f.CommentDensity),
		Sites: toJSONSites(f, sites), Candidates: toJSONCandidates(f, sites)}
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
	// Pinned is the head side's pin (#2959), absent when unpinned.
	Pinned *jsonPinned `json:"pinned,omitempty"`
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
	// Commits and Hot (commits × excess, × ΣCCN without excess) are the
	// --churn hotspot numbers (#2957), absent without the flag.
	Commits *int `json:"commits,omitempty"`
	Hot     *int `json:"hot,omitempty"`
	// Baseline is the group's status against the baseline in effect
	// (#2958, complexity.Summary.BaselineStatus): "worsened" when any
	// function worsened, "accepted" when every over-threshold function is
	// accepted, "new" when one is not; absent without a baseline and on a
	// group with nothing over threshold.
	Baseline string `json:"baseline,omitempty"`
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
	Rev string `json:"rev"`
	Sha string `json:"sha"`
	// HeadRev / HeadSha name the head end (#2961): B of --diff=A..B, or
	// "working tree" with an empty sha.
	HeadRev string         `json:"head_rev"`
	HeadSha string         `json:"head_sha"`
	Base    jsonSummary    `json:"base"`
	Head    jsonSummary    `json:"head"`
	Delta   jsonDelta      `json:"delta"`
	Changed []jsonChange   `json:"changed"`
	Added   []jsonFunction `json:"added"`
	Removed []jsonFunction `json:"removed"`
	// Files is the per-file fold under --by file (#2955), absent otherwise.
	Files   *[]jsonFileChange `json:"files,omitempty"`
	Verdict string            `json:"verdict"`
	Skipped jsonSkipped       `json:"skipped"`
}

// jsonFileChange is one file of the --by file diff (#2955): the counts and
// both sides of every sum with the delta.
type jsonFileChange struct {
	File         string `json:"file"`
	Changed      int    `json:"changed"`
	Added        int    `json:"added"`
	Removed      int    `json:"removed"`
	CCNBefore    int    `json:"ccn_before"`
	CCNAfter     int    `json:"ccn_after"`
	CCNDelta     int    `json:"ccn_delta"`
	CogBefore    int    `json:"cog_before"`
	CogAfter     int    `json:"cog_after"`
	CogDelta     int    `json:"cog_delta"`
	NLOCBefore   int    `json:"nloc_before"`
	NLOCAfter    int    `json:"nloc_after"`
	ExcessBefore int    `json:"excess_before"`
	ExcessAfter  int    `json:"excess_after"`
	ExcessDelta  int    `json:"excess_delta"`
}

func toJSONFileChange(fc fileChange) jsonFileChange {
	return jsonFileChange{File: fc.file, Changed: fc.Changed, Added: fc.Added, Removed: fc.Removed,
		CCNBefore: fc.Before.CCNSum, CCNAfter: fc.After.CCNSum, CCNDelta: fc.DeltaCCN(),
		CogBefore: fc.Before.CogSum, CogAfter: fc.After.CogSum, CogDelta: fc.DeltaCognitive(),
		NLOCBefore: fc.Before.NLOC, NLOCAfter: fc.After.NLOC,
		ExcessBefore: fc.Before.Excess, ExcessAfter: fc.After.Excess, ExcessDelta: fc.DeltaExcess()}
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func writeProjectJSON(w io.Writer, r projectReport) error {
	out := jsonProject{
		Summary:   toJSONSummary(r.summary, r.files, r.s, r.hidden),
		Functions: make([]jsonFunction, 0, len(r.rows)),
		Skipped:   toJSONSkipped(r.skipped),
	}
	for _, rw := range r.rows {
		jf := toJSONFunction(rw.file, rw.fn, r.s.args.Sites)
		if r.s.baseline != nil {
			jf.Baseline = rw.fn.Baseline.String()
		}
		jf.Pinned = toJSONPinned(r.s.pinOf(rw.file, rw.fn, rw.ordinal))
		jf.Hidden = rw.hidden
		if r.s.churn != nil {
			n := r.s.churn[rw.file]
			jf.Commits = &n
		}
		out.Functions = append(out.Functions, jf)
	}
	return writeJSON(w, out)
}

func writeGroupsJSON(w io.Writer, r groupReport) error {
	out := jsonGroups{
		Summary: toJSONSummary(r.summary, r.files, r.s, r.hidden),
		Groups:  make([]jsonGroup, 0, len(r.groups)),
		Skipped: toJSONSkipped(r.skipped),
	}
	for _, g := range r.groups {
		jg := toJSONGroup(g)
		if r.s.baseline != nil {
			if st, ok := g.BaselineStatus(); ok {
				jg.Baseline = st.String()
			}
		}
		if r.s.churn != nil {
			commits, hot := r.s.hot(g)
			jg.Commits, jg.Hot = &commits, &hot
		}
		out.Groups = append(out.Groups, jg)
	}
	return writeJSON(w, out)
}

func writeDiffJSON(w io.Writer, r diffReport) error {
	changed, added, removed := r.sections()
	out := jsonDiff{
		Rev: r.rev, Sha: r.sha, HeadRev: r.headRev, HeadSha: r.headSha,
		Base: toJSONSummary(r.diff.Base, r.baseFiles, r.s, 0),
		Head: toJSONSummary(r.diff.Head, r.headFiles, r.s, 0),
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
			Before: toJSONMetrics(c.ch.Before, false), After: toJSONMetrics(c.ch.After, r.s.args.Sites),
			Delta: jsonDelta{CCN: c.ch.DeltaCCN(), Cognitive: c.ch.DeltaCognitive(), NLOC: c.ch.DeltaNLOC(),
				Excess:       r.s.th(c.file).Excess(c.ch.After) - r.s.th(c.file).Excess(c.ch.Before),
				CommentLines: c.ch.DeltaCommentLines(), CommentDensity: ratio(c.ch.DeltaCommentDensity())},
			Pinned: toJSONPinned(c.pin),
		})
	}
	for _, rw := range added {
		jf := toJSONFunction(rw.file, rw.fn, r.s.args.Sites)
		jf.Pinned = toJSONPinned(r.s.pinOf(rw.file, rw.fn, rw.ordinal))
		out.Added = append(out.Added, jf)
	}
	for _, rw := range removed {
		out.Removed = append(out.Removed, toJSONFunction(rw.file, rw.fn, false))
	}
	if r.by == complexity.ByFile {
		files := []jsonFileChange{}
		for _, fc := range r.files() {
			files = append(files, toJSONFileChange(fc))
		}
		out.Files = &files
	}
	return writeJSON(w, out)
}

// --- text ---

// writeProjectText prints the table (CCN descending), the summary line and
// the skipped line.
func writeProjectText(w io.Writer, r projectReport) {
	tw := r.s.newTable(w, colPath, colName)
	head := "FILE\tFUNCTION\tLINE\tCCN\tCOG\tNLOC\tPARAMS\tCMT%" + r.s.churnHeader("\tCOMMITS")
	if r.s.args.Pins {
		head += "\tPINNED_CCN\tPINNED_COG\tΔCCN\tΔCOG"
	}
	fmt.Fprintln(tw, head)
	for _, rw := range r.rows {
		f := rw.fn
		name := f.Name
		if f.Baseline == complexity.BaselineAccepted {
			name += acceptedMark
		}
		pin := r.s.pinOf(rw.file, f, rw.ordinal)
		if pin != nil {
			name += pinMark
		}
		if rw.hidden {
			name += hiddenMark // #3031, --show-hidden
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d", rw.file, name, f.Line+1, f.CCN, f.Cognitive, f.NLOC, f.Params,
			complexity.DensityPercent(f.CommentDensity))
		if r.s.churn != nil {
			fmt.Fprintf(tw, "\t%d", r.s.churn[rw.file])
		}
		if r.s.args.Pins && pin != nil {
			dc, dg := pin.SincePin(f)
			fmt.Fprintf(tw, "\t%d\t%d\t%s\t%s", pin.CCN, pin.Cognitive, complexity.SignedDelta(dc), complexity.SignedDelta(dg))
		}
		fmt.Fprintln(tw)
	}
	_ = tw.Flush()
	r.writeSummaryLine(w)
	if r.s.args.Pins {
		r.writePinsLine(w)
	}
	if r.s.filtered() {
		fmt.Fprintf(w, "shown: %d of %d functions (%s%s)\n", len(r.rows), r.summary.Functions, r.s.filterText(), r.s.testsNote())
	}
	if l := r.skipped.line(); l != "" {
		fmt.Fprintln(w, l)
	}
}

// acceptedMark follows the name of a function the baseline accepts (#2958)
// in the text table, as the tool window marks it.
const acceptedMark = " ✓"

// pinMark follows the name of a pinned function (#2959), as the tool
// window marks it.
const pinMark = " ★"

// hiddenMark follows the name of a hidden function --show-hidden lists
// (#3031), as the Complexity window's hidden:yes view marks it.
const hiddenMark = " ✕"

// writePinsLine is --pins' trailer: how many pins the table shows and how
// many name a function the report no longer measures (gone).
func (r projectReport) writePinsLine(w io.Writer) {
	n := r.s.pins.Len()
	if n == 0 {
		fmt.Fprintf(w, "pins: none (%s)\n", r.s.rel(r.s.pinsPath))
		return
	}
	seen := map[complexity.EntryKey]bool{}
	for _, rw := range r.all {
		seen[complexity.EntryKey{File: rw.file, Function: rw.fn.Name, Ordinal: rw.ordinal}] = true
	}
	gone := 0
	for _, p := range r.s.pins.Pinned {
		if !seen[p.Key()] {
			gone++
		}
	}
	fmt.Fprintf(w, "pins: %d pinned, %d shown, %d gone (%s)\n", n, len(r.rows), gone, r.s.rel(r.s.pinsPath))
}

// writeSummaryLine prints the blank line and the whole-project summary
// both project tables end with.
func (r projectReport) writeSummaryLine(w io.Writer) {
	s := r.summary
	fmt.Fprintf(w, "\nsummary: %d functions in %d files · ΣCCN %d (avg %.1f, p50 %d, p90 %d, p99 %d, max %d) · Σcog %d (avg %.1f, p50 %d, p90 %d, p99 %d, max %d) · NLOC %d · cmt %d%% · %d over threshold (%s) · excess %d\n",
		s.Functions, r.files, s.CCNSum, s.CCNAvg, s.CCNP50, s.CCNP90, s.CCNP99, s.CCNMax,
		s.CogSum, s.CogAvg, s.CogP50, s.CogP90, s.CogP99, s.CogMax, s.NLOC,
		complexity.DensityPercent(s.CommentDensity), s.OverThreshold, r.s.thresholdText(), s.Excess)
	if r.s.args.NoTests {
		fmt.Fprintf(w, "tests hidden: %d %s (--no-tests)\n", r.tests, plural(r.tests, "file", "files"))
	}
	if r.s.churn != nil {
		fmt.Fprintf(w, "churn: commits in the last %d %s (git log, renames not followed)\n", r.s.churnDays, plural(r.s.churnDays, "day", "days"))
	}
	if r.s.baseline != nil {
		fmt.Fprintf(w, "baseline: %d accepted, %d worsened (%s)\n", s.Accepted, s.Worsened, r.s.rel(r.s.baselinePath))
	}
	if r.hidden > 0 {
		// The hidden set (#3031): what the report left out, or lists ✕.
		verb := "left out"
		if r.s.args.ShowHidden {
			verb = "shown (--show-hidden)"
		}
		fmt.Fprintf(w, "hidden: %d %s %s (%s)\n", r.hidden, plural(r.hidden, "function", "functions"), verb, r.s.rel(r.s.baselinePath))
	}
}

// testsNote is the shown line's --no-tests suffix (#2950).
func (s settings) testsNote() string {
	if s.args.NoTests {
		return "; tests hidden"
	}
	return ""
}

// plural picks the noun for n.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// writeGroupsText prints the group table (#2937, CCN sum descending), the
// summary line and the skipped line; the CLASS column only per class.
func writeGroupsText(w io.Writer, r groupReport) {
	tw := r.s.newTable(w, colPath, r.classKind())
	byClass := r.by == complexity.ByClass
	if byClass {
		fmt.Fprint(tw, "FILE\tCLASS\t")
	} else {
		fmt.Fprint(tw, "FILE\t")
	}
	fmt.Fprintln(tw, "FUNCS\tNLOC\tCCN\tCCN_AVG\tCCN_MAX\tCOG\tCOG_AVG\tCOG_MAX\tCMT%\tOVER\tEXCESS"+r.s.churnHeader("\tCOMMITS\tHOT"))
	for _, g := range r.groups {
		if byClass {
			fmt.Fprintf(tw, "%s\t%s\t", g.File, g.Class)
		} else {
			fmt.Fprintf(tw, "%s\t", g.File)
		}
		fmt.Fprintf(tw, "%d\t%d\t%d\t%.1f\t%d\t%d\t%.1f\t%d\t%d\t%d\t%d", g.Functions, g.NLOC,
			g.CCNSum, g.CCNAvg, g.CCNMax, g.CogSum, g.CogAvg, g.CogMax,
			complexity.DensityPercent(g.CommentDensity), g.OverThreshold, g.Excess)
		if r.s.churn != nil {
			commits, hot := r.s.hot(g)
			fmt.Fprintf(tw, "\t%d\t%d", commits, hot)
		}
		fmt.Fprintln(tw)
	}
	_ = tw.Flush()
	r.writeSummaryLine(w)
	if r.s.filtered() {
		noun := "files"
		if byClass {
			noun = "classes"
		}
		fmt.Fprintf(w, "shown: %d of %d %s (%s%s)\n", len(r.groups), r.total, noun, r.s.groupFilterText(), r.s.testsNote())
	}
	if l := r.skipped.line(); l != "" {
		fmt.Fprintln(w, l)
	}
}

// classKind is the group tables' second column: the class name per class,
// a number per file (#2992).
func (r groupReport) classKind() colKind {
	if r.by == complexity.ByClass {
		return colName
	}
	return colNum
}

// writeBriefText prints the --brief rendering (#2938): one aligned line per
// group kept — path, class (per class only), funcs, nloc, the CCN and
// cognitive sums, max CCN / max cognitive and the comment density — with no
// header, summary, shown or skipped line.
func writeBriefText(w io.Writer, r groupReport) {
	tw := r.s.newTable(w, colPath, r.classKind())
	for _, g := range r.groups {
		if r.by == complexity.ByClass {
			fmt.Fprintf(tw, "%s\t%s\t", g.File, g.Class)
		} else {
			fmt.Fprintf(tw, "%s\t", g.File)
		}
		fmt.Fprintf(tw, "funcs=%d\tnloc=%d\tccn=%d\tcog=%d\tmax=%d/%d\tcmt=%d%%", g.Functions, g.NLOC,
			g.CCNSum, g.CogSum, g.CCNMax, g.CogMax, complexity.DensityPercent(g.CommentDensity))
		if r.s.churn != nil {
			commits, hot := r.s.hot(g)
			fmt.Fprintf(tw, "\tcommits=%d\thot=%d", commits, hot)
		}
		fmt.Fprintln(tw)
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
// cmt ≥ 40%", the comment density left out while it is off, and how many
// complexity.overrides entries judge some files differently (#2960):
// "… · 2 overrides".
func (s settings) thresholdText() string {
	t := fmt.Sprintf("CCN ≥ %d or cog ≥ %d", s.ccnWarn, s.cogWarn)
	if s.cmtWarn > 0 {
		t += fmt.Sprintf(" or cmt ≥ %d%%", s.cmtWarn)
	}
	switch {
	case s.overrides == 0, s.args.CCN > 0 && s.args.Cog > 0 && s.args.Cmt > 0:
	case s.overrides == 1:
		t += " · 1 override"
	default:
		t += fmt.Sprintf(" · %d overrides", s.overrides)
	}
	return t
}

// writeDiffText prints the changed / added / removed sections — the
// per-file table under --by file (#2955) — the totals and the verdict line.
func writeDiffText(w io.Writer, r diffReport) {
	if r.headSha != "" {
		fmt.Fprintf(w, "complexity %s (%s) → %s (%s)\n", r.rev, shortSha(r.sha), r.headRev, shortSha(r.headSha))
	} else {
		fmt.Fprintf(w, "complexity vs %s (%s)\n", r.rev, shortSha(r.sha))
	}
	changed, added, removed := r.sections()
	if len(changed)+len(added)+len(removed) == 0 {
		fmt.Fprintln(w, "\nno function changed its complexity")
	}
	if r.by == complexity.ByFile {
		writeDiffFilesText(w, r)
		writeDiffTotals(w, r)
		return
	}
	if len(changed) > 0 {
		fmt.Fprintf(w, "\nchanged (%d):\n", len(changed))
		tw := r.s.newTable(w, colPath, colName)
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
		tw := r.s.newTable(w, colPath, colName)
		for _, rw := range rows {
			f := rw.fn
			fmt.Fprintf(tw, "  %s\t%s\tL%d\tCCN %d\tcog %d\tNLOC %d\tcmt %d%%\n", rw.file, f.Name, f.Line+1, f.CCN, f.Cognitive, f.NLOC,
				complexity.DensityPercent(f.CommentDensity))
		}
		_ = tw.Flush()
	}
	section("added", added)
	section("removed", removed)
	writeDiffTotals(w, r)
}

// writeDiffFilesText prints the --by file table (#2955): one row per file,
// ΔCCN descending, the counts and every sum as before→after (Δ).
func writeDiffFilesText(w io.Writer, r diffReport) {
	files := r.files()
	if len(files) == 0 {
		return
	}
	fmt.Fprintf(w, "\nfiles (%d):\n", len(files))
	tw := r.s.newTable(w, colPath)
	fmt.Fprintln(tw, "  FILE\tCHANGED\tADDED\tREMOVED\tCCN\tCOG\tNLOC\tEXCESS")
	for _, fc := range files {
		b, a := fc.Before, fc.After
		fmt.Fprintf(tw, "  %s\t%d\t%d\t%d\t%d→%d (%s)\t%d→%d (%s)\t%d→%d\t%d→%d (%s)\n",
			fc.file, fc.Changed, fc.Added, fc.Removed,
			b.CCNSum, a.CCNSum, signed(fc.DeltaCCN()),
			b.CogSum, a.CogSum, signed(fc.DeltaCognitive()),
			b.NLOC, a.NLOC,
			b.Excess, a.Excess, signed(fc.DeltaExcess()))
	}
	_ = tw.Flush()
}

// writeDiffTotals prints the totals, the verdict and the trailing notes of
// the diff report.
func writeDiffTotals(w io.Writer, r diffReport) {
	b, h := r.diff.Base, r.diff.Head
	fmt.Fprintf(w, "\ntotals: before %d functions, ΣCCN %d, Σcog %d, excess %d → after %d functions, ΣCCN %d, Σcog %d, excess %d\n",
		b.Functions, b.CCNSum, b.CogSum, b.Excess, h.Functions, h.CCNSum, h.CogSum, h.Excess)
	fmt.Fprintf(w, "verdict: %s (excess %s, Σcog %s, ΣCCN %s)\n", verdictText(r.diff.Verdict),
		signed(r.diff.DeltaExcess()), signed(r.diff.DeltaCognitive()), signed(r.diff.DeltaCCN()))
	if r.s.args.NoTests {
		fmt.Fprintln(w, "tests hidden (--no-tests)")
	}
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
