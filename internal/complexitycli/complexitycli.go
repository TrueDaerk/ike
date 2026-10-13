// Package complexitycli is the headless `ike complexity` report (Roadmap
// 0550, #2922): the project table or the diff against a git revision,
// rendered as text or JSON, for CI pipelines and coding agents. It runs the
// same walk (complexity.WalkProject), engine and diff (complexity.DiffProject)
// as the Complexity tool window, synchronously and without a terminal.
//
// The report never fails on thresholds — gating is the caller's policy (a
// `jq` expression over the JSON); the exit code is 0 for a report and 2 for
// a usage, git or build error.
package complexitycli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"ike/internal/cli"
	"ike/internal/complexity"
	"ike/internal/config"
	"ike/internal/lang"
	"ike/internal/safego"
	"ike/internal/vcs/vcscore"
)

// Exit codes.
const (
	ExitOK    = 0
	ExitError = 2
)

// defaultWarn is the threshold used when neither the flags nor the config
// name one.
const defaultWarn = 15

// available is the build seam: a variable so a test can play the !cgo
// build, where no language has a syntax tree.
var available = complexity.Available

// target is one selected path (#2936): a directory to walk or a file named
// explicitly.
type target struct {
	abs   string // absolute, symlinks resolved
	isDir bool
}

// settings is the resolved run: project root, selection, exclusions and
// thresholds.
type settings struct {
	args    cli.ComplexityArgs
	dir     string // absolute project root, symlinks resolved
	targets []target
	exclude []string
	ccnWarn int
	cogWarn int
	// cmtWarn is the comment-density threshold in percent (#2934); 0 is
	// off, as complexity.comment_density_warn = 0 configures it.
	cmtWarn int
	// churnDays is the --churn window (#2957): the flag's value, else
	// complexity.churn_days. churn holds the commits per project-relative
	// path in it, nil without --churn.
	churnDays int
	churn     map[string]int
	// baseline is the accepted-debt baseline in effect (#2958), nil
	// without one (no file, --no-baseline); baselinePath is the file it
	// came from or --write-baseline writes — the project file unless
	// --baseline=PATH named another.
	baseline     *complexity.Baseline
	baselinePath string
	// pins is the pin list (#2959), empty without a file; pinsPath is
	// where it was read from.
	pins     *complexity.Pins
	pinsPath string
}

// pinOf returns the pin of the function at (file, ordinal), nil when it
// is not pinned.
func (s settings) pinOf(file string, fn complexity.Function, ordinal int) *complexity.Pin {
	p, ok := s.pins.Lookup(complexity.EntryKey{File: file, Function: fn.Name, Ordinal: ordinal})
	if !ok {
		return nil
	}
	return &p
}

// defaultChurnDays is the --churn window when neither the flag nor the
// config names one.
const defaultChurnDays = 90

// loadChurn fetches the commits per file for --churn (#2957) —
// synchronously, like --diff; outside a repository it is an error.
func (s *settings) loadChurn(now time.Time) error {
	if _, err := vcscore.DetectRoot(s.dir); err != nil {
		return fmt.Errorf("--churn needs a git repository: %s is not inside one", s.dir)
	}
	counts, err := vcscore.Churn(s.dir, now.AddDate(0, 0, -s.churnDays))
	if err != nil {
		return err
	}
	s.churn = counts
	return nil
}

// hot is a group's --churn numbers: its file's commits and the hotspot
// score over the group's aggregate (complexity.Hot).
func (s settings) hot(g complexity.Group) (commits, hot int) {
	commits = s.churn[g.File]
	return commits, complexity.Hot(commits, g.Summary)
}

// churnShown is the summary's churn_days: the window under --churn, 0
// (omitted) without it.
func (s settings) churnShown() int {
	if s.churn == nil {
		return 0
	}
	return s.churnDays
}

// churnHeader is cols under --churn, "" without it.
func (s settings) churnHeader(cols string) string {
	if s.churn == nil {
		return ""
	}
	return cols
}

// Run executes the report for a and returns the exit code. Output goes to
// stdout, diagnostics to stderr.
func Run(a cli.ComplexityArgs, stdout, stderr io.Writer) int {
	if a.Help {
		fmt.Fprintln(stdout, cli.ComplexityUsage)
		return ExitOK
	}
	by, ok := complexity.ParseLevel(a.By)
	brief, briefOK := complexity.ParseLevel(a.Brief)
	switch {
	case !ok:
		fmt.Fprintf(stderr, "ike complexity: --by: %q is not class or file\n", a.By)
		return ExitError
	case !briefOK:
		fmt.Fprintf(stderr, "ike complexity: --brief: %q is not class or file\n", a.Brief)
		return ExitError
	case brief != complexity.ByNone && by != complexity.ByNone:
		fmt.Fprintln(stderr, "ike complexity:", cli.ErrBriefWithBy)
		return ExitError
	case brief != complexity.ByNone && a.Diff:
		fmt.Fprintln(stderr, "ike complexity:", cli.ErrBriefWithDiff)
		return ExitError
	case by == complexity.ByClass && a.Diff:
		fmt.Fprintln(stderr, "ike complexity:", cli.ErrByWithDiff)
		return ExitError
	case a.Churn && a.Diff:
		fmt.Fprintln(stderr, "ike complexity:", cli.ErrChurnWithDiff)
		return ExitError
	case a.Baseline && a.NoBaseline:
		fmt.Fprintln(stderr, "ike complexity:", cli.ErrBaselineWithNoBaseline)
		return ExitError
	case a.WriteBaseline && a.Diff:
		fmt.Fprintln(stderr, "ike complexity:", cli.ErrWriteBaselineWithDiff)
		return ExitError
	case a.WriteBaseline && a.NoBaseline:
		fmt.Fprintln(stderr, "ike complexity:", cli.ErrWriteBaselineWithNoBaseline)
		return ExitError
	case a.Pins && (a.Diff || by != complexity.ByNone || brief != complexity.ByNone):
		fmt.Fprintln(stderr, "ike complexity:", cli.ErrPinsWithDiff)
		return ExitError
	}
	if brief != complexity.ByNone {
		// --brief folds like --by: the JSON stays the full group report.
		by = brief
	}
	if !available() {
		fmt.Fprintln(stderr, "ike complexity: this build has no syntax trees (built without cgo); use a cgo build of ike")
		return ExitError
	}
	s, err := resolve(a)
	if err != nil {
		fmt.Fprintln(stderr, "ike complexity:", err)
		return ExitError
	}
	if a.Churn {
		if err := s.loadChurn(time.Now()); err != nil {
			fmt.Fprintln(stderr, "ike complexity:", err)
			return ExitError
		}
	}
	if a.Diff {
		r, err := runDiff(s)
		r.by = by
		if err != nil {
			fmt.Fprintln(stderr, "ike complexity:", err)
			return ExitError
		}
		if a.JSON {
			err = writeDiffJSON(stdout, r)
		} else {
			writeDiffText(stdout, r)
		}
		if err != nil {
			fmt.Fprintln(stderr, "ike complexity:", err)
			return ExitError
		}
		return ExitOK
	}
	r := runProject(s)
	if a.WriteBaseline {
		n, err := writeBaseline(r)
		if err != nil {
			fmt.Fprintln(stderr, "ike complexity:", err)
			return ExitError
		}
		fmt.Fprintf(stdout, "baseline: %d %s over threshold (CCN ≥ %d or cog ≥ %d) written to %s\n", n, plural(n, "function", "functions"),
			s.ccnWarn, s.cogWarn, s.rel(s.baselinePath))
		return ExitOK
	}
	if by != complexity.ByNone {
		switch {
		case a.JSON:
			if err := writeGroupsJSON(stdout, groupProject(r, by, "")); err != nil {
				fmt.Fprintln(stderr, "ike complexity:", err)
				return ExitError
			}
		case brief != complexity.ByNone:
			writeBriefText(stdout, groupProject(r, by, freeLabel))
		default:
			writeGroupsText(stdout, groupProject(r, by, ""))
		}
		return ExitOK
	}
	if a.JSON {
		if err := writeProjectJSON(stdout, r); err != nil {
			fmt.Fprintln(stderr, "ike complexity:", err)
			return ExitError
		}
	} else {
		writeProjectText(stdout, r)
	}
	return ExitOK
}

// resolve validates the paths, picks the project root and reads the
// [complexity] settings of the user and project config layers (defaults
// when there are none); the flags override the thresholds.
func resolve(a cli.ComplexityArgs) (settings, error) {
	paths := a.Paths
	if len(paths) == 0 {
		paths = []string{"."}
	}
	targets := make([]target, 0, len(paths))
	for _, p := range paths {
		t, err := resolveTarget(p)
		if err != nil {
			return settings{}, err
		}
		targets = append(targets, t)
	}
	root, err := projectRoot(a, targets[0])
	if err != nil {
		return settings{}, err
	}
	c, _ := config.Load(config.Discover(root))
	s := settings{args: a, dir: root, targets: targets, exclude: c.Complexity.Exclude,
		ccnWarn: c.Complexity.CCNWarn, cogWarn: c.Complexity.CognitiveWarn, cmtWarn: c.Complexity.CommentDensityWarn}
	if a.CCN > 0 {
		s.ccnWarn = a.CCN
	}
	if a.Cog > 0 {
		s.cogWarn = a.Cog
	}
	if a.Cmt > 0 {
		s.cmtWarn = a.Cmt
	}
	if s.ccnWarn <= 0 {
		s.ccnWarn = defaultWarn
	}
	if s.cogWarn <= 0 {
		s.cogWarn = defaultWarn
	}
	s.churnDays = c.Complexity.ChurnDays
	if a.ChurnDays > 0 {
		s.churnDays = a.ChurnDays
	}
	if s.churnDays <= 0 {
		s.churnDays = defaultChurnDays
	}
	if err := s.loadBaseline(); err != nil {
		return settings{}, err
	}
	s.pinsPath = complexity.PinsPath(root)
	if s.pins, err = complexity.LoadPins(s.pinsPath); err != nil {
		return settings{}, fmt.Errorf("pins: %w", err)
	}
	return s, nil
}

// loadBaseline resolves the baseline file (#2958): --baseline=PATH as
// typed (relative to the working directory), else the project's
// .ike/complexity-baseline.json. The project file is read when it exists;
// a file asked for by flag must exist unless --write-baseline is about to
// create it; a malformed file is an error either way; --no-baseline reads
// nothing.
func (s *settings) loadBaseline() error {
	a := s.args
	path := complexity.BaselinePath(s.dir)
	if a.BaselinePath != "" {
		p, err := filepath.Abs(a.BaselinePath)
		if err != nil {
			return err
		}
		// Symlinks resolved like s.dir (macOS /var → /private/var), so the
		// path prints relative to the project when it lies inside.
		if ev, err := filepath.EvalSymlinks(p); err == nil {
			p = ev
		} else if ev, err := filepath.EvalSymlinks(filepath.Dir(p)); err == nil {
			p = filepath.Join(ev, filepath.Base(p))
		}
		path = p
	}
	s.baselinePath = path
	if a.NoBaseline {
		return nil
	}
	b, err := complexity.LoadBaseline(path)
	if err != nil {
		return err
	}
	if b == nil && a.Baseline && !a.WriteBaseline {
		return fmt.Errorf("--baseline: no baseline file at %s (seed one with --write-baseline)", s.rel(path))
	}
	s.baseline = b
	return nil
}

// writeBaseline writes every over-threshold function of the report into
// the baseline file, replacing it (#2958): the CI / agent way to seed the
// accepted debt. Returns the entry count.
func writeBaseline(r projectReport) (int, error) {
	s := r.s
	b := complexity.NewBaseline(s.ccnWarn, s.cogWarn)
	for _, rw := range r.all {
		if rw.fn.Over(s.ccnWarn, s.cogWarn) {
			b.Accept(rw.file, rw.fn, rw.ordinal)
		}
	}
	if err := b.Save(s.baselinePath); err != nil {
		return 0, err
	}
	return b.Len(), nil
}

// resolveTarget makes p absolute (symlinks resolved, so paths compare with
// what git reports — macOS /var → /private) and classifies it; a missing
// path is an error naming it.
func resolveTarget(p string) (target, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return target{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return target{}, fmt.Errorf("%s: no such file or directory", p)
	}
	if ev, err := filepath.EvalSymlinks(abs); err == nil {
		abs = ev
	}
	return target{abs: abs, isDir: info.IsDir()}, nil
}

// projectRoot is the root for config discovery and relative output paths:
// the working directory without positional paths (as before #2936);
// otherwise the git root of the first path, else that path when it is a
// directory, else the working directory.
func projectRoot(a cli.ComplexityArgs, first target) (string, error) {
	if len(a.Paths) == 0 {
		return first.abs, nil
	}
	dir := first.abs
	if !first.isDir {
		dir = filepath.Dir(dir)
	}
	if repo, err := vcscore.DetectRoot(dir); err == nil && repo != "" {
		if ev, err := filepath.EvalSymlinks(repo); err == nil {
			repo = ev
		}
		return repo, nil
	}
	if first.isDir {
		return first.abs, nil
	}
	return resolveCwd()
}

// resolveCwd is the working directory, symlinks resolved.
func resolveCwd() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if ev, err := filepath.EvalSymlinks(wd); err == nil {
		wd = ev
	}
	return wd, nil
}

// named reports whether path is a file the user named explicitly.
func (s settings) named(path string) bool {
	for _, t := range s.targets {
		if !t.isDir && t.abs == path {
			return true
		}
	}
	return false
}

// selected reports whether path lies in the selection: a named file, or
// below a named directory.
func (s settings) selected(path string) bool {
	for _, t := range s.targets {
		if t.isDir && inside(t.abs, path) || !t.isDir && t.abs == path {
			return true
		}
	}
	return false
}

// keep reports whether a function passes the row filter: --min N keeps
// CCN or cognitive ≥ N; an explicit --ccn / --cog / --cmt keeps rows at or
// above any threshold (the comment density only while its threshold is
// on); no filter flag keeps everything.
func (s settings) keep(f complexity.Function) bool {
	switch {
	case s.args.Min > 0:
		return f.CCN >= s.args.Min || f.Cognitive >= s.args.Min
	case s.args.CCN > 0 || s.args.Cog > 0 || s.args.Cmt > 0:
		return f.CCN >= s.ccnWarn || f.Cognitive >= s.cogWarn || f.CommentsOver(s.cmtWarn)
	}
	return true
}

// hidesTest reports whether --no-tests drops path (#2950); a file named
// explicitly is kept, as the exclude globs keep it.
func (s settings) hidesTest(path string) bool {
	return s.args.NoTests && !s.named(path) && complexity.IsTestFile(s.dir, path)
}

// filtered reports whether any row filter is active.
func (s settings) filtered() bool {
	return s.args.Min > 0 || s.args.CCN > 0 || s.args.Cog > 0 || s.args.Cmt > 0
}

// rel renders abs relative to the project directory, slash-separated.
func (s settings) rel(abs string) string {
	if r, err := filepath.Rel(s.dir, abs); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(abs)
}

// skipped counts the files a language claims but the engine cannot measure.
type skipped struct {
	files int
	langs map[string]bool
}

func (k *skipped) add(langID string) {
	if k.langs == nil {
		k.langs = map[string]bool{}
	}
	k.files++
	k.langs[langID] = true
}

// languages lists the skipped language ids, sorted.
func (k skipped) languages() []string {
	out := make([]string, 0, len(k.langs))
	for l := range k.langs {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// line renders the final "skipped:" line; "" when nothing was skipped.
func (k skipped) line() string {
	if k.files == 0 {
		return ""
	}
	noun := "files"
	if k.files == 1 {
		noun = "file"
	}
	return fmt.Sprintf("skipped: %d %s (no complexity spec: %s)", k.files, noun, strings.Join(k.languages(), ", "))
}

// measureFile measures text as the language path names; ok is false when
// the engine cannot (no spec, or no tree in this build).
func measureFile(path, text string) ([]complexity.Function, bool) {
	l, ok := lang.ByPath(path)
	if !ok || l.Complexity == nil {
		return nil, false
	}
	fns, err := complexity.Measure(l.ID, strings.Split(text, "\n"))
	if err != nil {
		return nil, false
	}
	return fns, true
}

// langOf names path's language for the skipped line.
func langOf(path string) string {
	if l, ok := lang.ByPath(path); ok {
		return l.ID
	}
	return filepath.Ext(path)
}

// parallel runs fn over items with one worker per CPU.
func parallel[T any](items []T, fn func(T)) {
	workers := runtime.NumCPU()
	if workers > len(items) {
		workers = len(items)
	}
	queue := make(chan T)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		safego.Go("complexitycli.worker", func() {
			defer wg.Done()
			for it := range queue {
				fn(it)
			}
		})
	}
	for _, it := range items {
		queue <- it
	}
	close(queue)
	wg.Wait()
}

// --- project report ---

// row is one function of the project table.
type row struct {
	file string // project-relative slash path
	fn   complexity.Function
	// ordinal is the unit's index among its file's same-named units in
	// source order — the baseline's key (#2958).
	ordinal int
}

// projectReport is the measured project.
type projectReport struct {
	s       settings
	files   int
	all     []row // every function, sorted
	rows    []row // the rows the filter keeps
	summary complexity.Summary
	skipped skipped
	// tests counts the test files --no-tests dropped (#2950).
	tests int
}

// runProject walks the named directories, adds the named files and
// measures every file once.
func runProject(s settings) projectReport {
	r := projectReport{s: s}
	var paths []string
	seen := map[string]bool{}
	collect := func(path, langID string, measurable bool) {
		if seen[path] {
			return
		}
		seen[path] = true
		if s.hidesTest(path) {
			r.tests++
			return
		}
		if measurable {
			paths = append(paths, path)
			return
		}
		r.skipped.add(langID)
	}
	for _, t := range s.targets {
		if !t.isDir {
			// Named explicitly: no skip rule or exclude glob applies.
			l, ok := lang.ByPath(t.abs)
			collect(t.abs, langOf(t.abs), ok && l.Complexity != nil)
			continue
		}
		_ = complexity.WalkUnder(s.dir, t.abs, s.exclude, func(path string, l lang.Language, measurable bool) {
			collect(path, l.ID, measurable)
		})
	}
	var mu sync.Mutex
	parallel(paths, func(path string) {
		text, ok := complexity.ReadSource(path)
		if !ok {
			return
		}
		fns, ok := measureFile(path, text)
		mu.Lock()
		defer mu.Unlock()
		if !ok {
			r.skipped.add(langOf(path))
			return
		}
		r.files++
		rel := s.rel(path)
		// Stamped before the CCN sort: the baseline keys on source order.
		s.baseline.Stamp(rel, fns)
		for i, ord := range complexity.Ordinals(fns) {
			r.all = append(r.all, row{file: rel, fn: fns[i], ordinal: ord})
		}
	})
	sortRows(r.all)
	fns := make([]complexity.Function, len(r.all))
	for i, rw := range r.all {
		fns[i] = rw.fn
		if s.args.Pins && s.pinOf(rw.file, rw.fn, rw.ordinal) == nil {
			continue
		}
		if s.keep(rw.fn) {
			r.rows = append(r.rows, rw)
		}
	}
	r.summary = complexity.Summarize(fns, s.ccnWarn, s.cogWarn, s.cmtWarn)
	return r
}

// sortRows orders by CCN descending, then cognitive descending, then file,
// line, end line and name, so the output is stable: rows arrive in the
// order the parallel workers finish, and two units on one line (closures
// passed side by side) must not swap between runs (#2939).
func sortRows(rows []row) {
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.fn.CCN != b.fn.CCN {
			return a.fn.CCN > b.fn.CCN
		}
		if a.fn.Cognitive != b.fn.Cognitive {
			return a.fn.Cognitive > b.fn.Cognitive
		}
		if a.file != b.file {
			return a.file < b.file
		}
		if a.fn.Line != b.fn.Line {
			return a.fn.Line < b.fn.Line
		}
		if a.fn.EndLine != b.fn.EndLine {
			return a.fn.EndLine < b.fn.EndLine
		}
		return a.fn.Name < b.fn.Name
	})
}

// --- grouped project report ---

// groupReport is the measured project folded per class or per file (#2937).
type groupReport struct {
	projectReport
	by     complexity.Level
	total  int                // every group, before the filter
	groups []complexity.Group // the groups the filter keeps, CCN sum descending
}

// freeLabel is the class column of the per-file group --brief=class folds a
// file's free functions and lambdas into (#2938).
const freeLabel = "(free)"

// groupProject folds the project report's functions into the groups of by,
// keyed by the project-relative path, and applies the group filter. A
// non-empty free collects, per class, the class-less units of a file into
// one more group of that class name; "" leaves them out as --by class does.
func groupProject(r projectReport, by complexity.Level, free string) groupReport {
	files := map[string][]complexity.Function{}
	for _, rw := range r.all {
		fn := rw.fn
		if by == complexity.ByClass && fn.Class == "" {
			fn.Class = free
		}
		files[rw.file] = append(files[rw.file], fn)
	}
	s := r.s
	all := complexity.GroupFunctions(files, by, s.ccnWarn, s.cogWarn, s.cmtWarn)
	g := groupReport{projectReport: r, by: by, total: len(all)}
	for _, gr := range all {
		if s.keepGroup(gr) {
			g.groups = append(g.groups, gr)
		}
	}
	return g
}

// keepGroup is keep for a group row: --min N keeps a CCN or cognitive sum
// ≥ N; an explicit --ccn / --cog / --cmt keeps groups whose max CCN, max
// cognitive or aggregate comment density is at or above its threshold.
func (s settings) keepGroup(g complexity.Group) bool {
	switch {
	case s.args.Min > 0:
		return g.CCNSum >= s.args.Min || g.CogSum >= s.args.Min
	case s.args.CCN > 0 || s.args.Cog > 0 || s.args.Cmt > 0:
		return g.CCNMax >= s.ccnWarn || g.CogMax >= s.cogWarn ||
			s.cmtWarn > 0 && complexity.DensityPercent(g.CommentDensity) >= s.cmtWarn
	}
	return true
}

// --- diff report ---

// diffReport is the measured comparison against a revision.
type diffReport struct {
	s   settings
	rev string // the base as typed; "HEAD" by default
	sha string
	// headRev / headSha are the head end of --diff=A..B (#2961); headSha is
	// "" when the head side is the working tree.
	headRev string
	headSha string
	diff    complexity.Diff
	// by is the --by level: ByFile prints the per-file table (#2955).
	by      complexity.Level
	skipped skipped
	// baseFiles / headFiles count the measured files on each side.
	baseFiles, headFiles int
	// head is the working-tree measurement per absolute path — the
	// ordinals the pins key on (#2959).
	head map[string][]complexity.Function
}

// headOrdinal is a head-side unit's index among its file's same-named
// units, -1 when the head measurement does not hold it.
func (r diffReport) headOrdinal(path string, fn complexity.Function) int {
	fns := r.head[path]
	for i, ord := range complexity.Ordinals(fns) {
		if fns[i].Name == fn.Name && fns[i].Line == fn.Line {
			return ord
		}
	}
	return -1
}

// diffFile is one file of the comparison: its base blob (baseRel "" when
// the base has none) and its head side (headAbs "" when deleted; headRel
// its blob path in a ranged diff); baseAbs keys a rename's removed side
// under the old path.
type diffFile struct {
	baseRel, baseAbs, headRel, headAbs string
}

// runDiff compares the working tree under the directory against rev: the
// files git reports as changed plus the untracked ones, both sides measured.
// A range A..B (#2961) compares two commits instead: the files that differ
// between them, the head side read from B, the working tree ignored.
func runDiff(s settings) (diffReport, error) {
	rev, headRev, ranged, err := vcscore.ParseRevRange(s.args.Rev)
	if err != nil {
		return diffReport{s: s}, err
	}
	if rev == "" {
		rev = "HEAD"
	}
	r := diffReport{s: s, rev: rev, headRev: "working tree"}
	if ranged {
		r.headRev = headRev
	}
	repo, err := vcscore.DetectRoot(s.dir)
	if err != nil {
		return r, err
	}
	// The project root may sit under a symlink (macOS /var → /private):
	// compare paths against the resolved form, as resolve did for s.dir.
	if ev, err := filepath.EvalSymlinks(repo); err == nil {
		repo = ev
	}
	sha, err := vcscore.ResolveRev(repo, rev)
	if err != nil {
		return r, err
	}
	r.sha = sha
	var changed []vcscore.CommitFile
	if ranged {
		if r.headSha, err = vcscore.ResolveRev(repo, headRev); err != nil {
			return r, err
		}
		changed, err = vcscore.ChangedFilesBetween(repo, sha, r.headSha)
	} else {
		changed, err = vcscore.ChangedFiles(repo, sha)
	}
	if err != nil {
		return r, err
	}
	abs := func(rel string) string { return filepath.Join(repo, filepath.FromSlash(rel)) }
	var files []diffFile
	seen := map[string]bool{}
	// gate reports whether a file takes part — inside the selection, and
	// measurable — counting a claimed but unmeasurable one as skipped. A
	// file named explicitly ignores the exclude globs (#2936).
	gate := func(path string) bool {
		if !s.selected(path) {
			return false
		}
		if s.hidesTest(path) {
			return false
		}
		if s.named(path) {
			if l, ok := lang.ByPath(path); ok && l.Complexity != nil {
				return true
			}
			r.skipped.add(langOf(path))
			return false
		}
		if complexity.Measurable(s.dir, s.exclude, path) {
			return true
		}
		if l, ok := lang.ByPath(path); ok && !complexity.Excluded(s.dir, s.exclude, path) {
			r.skipped.add(l.ID)
		}
		return false
	}
	for _, f := range changed {
		head := abs(f.Path)
		seen[head] = true
		df := diffFile{baseRel: f.Path, baseAbs: head, headRel: f.Path, headAbs: head}
		switch f.Status {
		case vcscore.StatusAdded:
			df.baseRel, df.baseAbs = "", ""
		case vcscore.StatusDeleted:
			df.headRel, df.headAbs = "", ""
		case vcscore.StatusRenamed:
			if f.OldPath != "" {
				df.baseRel, df.baseAbs = f.OldPath, abs(f.OldPath)
			}
		}
		if !gate(head) {
			continue
		}
		files = append(files, df)
	}
	if ranged {
		// Two commits: the working tree and its untracked files play no part.
		r.measure(repo, files)
		return r, nil
	}
	// Untracked files are not in `git diff`: everything they hold is added.
	// git collapses an untracked directory to one "dir/" record.
	snap, err := vcscore.Load(s.dir)
	if err != nil {
		return r, err
	}
	addUntracked := func(path string) {
		if seen[path] || !gate(path) {
			return
		}
		seen[path] = true
		files = append(files, diffFile{headAbs: path})
	}
	for _, e := range snap.Entries {
		if e.Status != vcscore.StatusUntracked {
			continue
		}
		p := abs(strings.TrimSuffix(e.Path, "/"))
		if !strings.HasSuffix(e.Path, "/") {
			addUntracked(p)
			continue
		}
		_ = complexity.WalkProject(p, nil, func(path string, _ lang.Language, _ bool) { addUntracked(path) })
		// The walk skips hidden directories; a named file below one is
		// still untracked.
		for _, t := range s.targets {
			if !t.isDir && inside(p, t.abs) {
				addUntracked(t.abs)
			}
		}
	}
	r.measure(repo, files)
	return r, nil
}

// measure measures both sides of every file and diffs them: the base blob
// at r.sha, the head side from r.headSha's blob in a ranged diff (#2961),
// else from the working tree.
func (r *diffReport) measure(repo string, files []diffFile) {
	s := r.s
	base := map[string][]complexity.Function{}
	head := map[string][]complexity.Function{}
	var mu sync.Mutex
	parallel(files, func(df diffFile) {
		var bfns, hfns []complexity.Function
		if df.baseRel != "" {
			if content, err := vcscore.RevContent(repo, r.sha, df.baseRel); err == nil {
				bfns, _ = measureFile(df.baseRel, content)
			}
		}
		switch {
		case df.headAbs == "":
		case r.headSha != "":
			if content, err := vcscore.RevContent(repo, r.headSha, df.headRel); err == nil {
				hfns, _ = measureFile(df.headRel, content)
			}
		default:
			// No size cap here: the base blob has none either, and a file
			// over the walk's cap must not read as all-removed (the
			// Changes tab reads it uncapped too).
			if data, err := os.ReadFile(df.headAbs); err == nil {
				hfns, _ = measureFile(df.headAbs, string(data))
			}
		}
		mu.Lock()
		defer mu.Unlock()
		if df.baseAbs != "" {
			base[df.baseAbs] = bfns
			r.baseFiles++
		}
		if df.headAbs != "" {
			head[df.headAbs] = hfns
			r.headFiles++
		}
	})
	r.diff = complexity.DiffProject(base, head, s.ccnWarn, s.cogWarn, s.cmtWarn)
	r.head = head
}

// inside reports whether path lies under dir.
func inside(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// changeRow is one changed function with its file.
type changeRow struct {
	file string
	ch   complexity.Change
	pin  *complexity.Pin // #2959
}

// sections lists the changed, added and removed rows the filter keeps, by
// file path then the per-file order DiffFunctions produced.
func (r diffReport) sections() (changed []changeRow, added, removed []row) {
	for _, p := range r.diff.Paths() {
		fd := r.diff.Files[p]
		rel := r.s.rel(p)
		for _, c := range fd.Changed {
			if r.s.keep(c.Before) || r.s.keep(c.After) {
				changed = append(changed, changeRow{file: rel, ch: c, pin: r.s.pinOf(rel, c.After, r.headOrdinal(p, c.After))})
			}
		}
		for _, f := range fd.Added {
			if r.s.keep(f) {
				added = append(added, row{file: rel, fn: f, ordinal: r.headOrdinal(p, f)})
			}
		}
		for _, f := range fd.Removed {
			if r.s.keep(f) {
				removed = append(removed, row{file: rel, fn: f})
			}
		}
	}
	return changed, added, removed
}

// fileChange is one file of the --by file diff (#2955), its path made
// project-relative.
type fileChange struct {
	file string
	complexity.FileChange
}

// files folds the diff per file (complexity.Diff.ByFile, ΔCCN descending),
// keeping the files with at least one unit the row filter keeps.
func (r diffReport) files() []fileChange {
	changed, added, removed := r.sections()
	kept := map[string]bool{}
	for _, c := range changed {
		kept[c.file] = true
	}
	for _, rw := range append(added, removed...) {
		kept[rw.file] = true
	}
	var out []fileChange
	for _, fc := range r.diff.ByFile() {
		if rel := r.s.rel(fc.File); kept[rel] {
			out = append(out, fileChange{file: rel, FileChange: fc})
		}
	}
	return out
}

// verdictID is the JSON spelling of a verdict.
func verdictID(v complexity.Verdict) string {
	return strings.ReplaceAll(v.String(), " ", "_")
}
