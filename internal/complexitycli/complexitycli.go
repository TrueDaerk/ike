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

	"ike/internal/cli"
	"ike/internal/complexity"
	"ike/internal/config"
	"ike/internal/lang"
	"ike/internal/safego"
	"ike/internal/vcs"
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
}

// Run executes the report for a and returns the exit code. Output goes to
// stdout, diagnostics to stderr.
func Run(a cli.ComplexityArgs, stdout, stderr io.Writer) int {
	if a.Help {
		fmt.Fprintln(stdout, cli.ComplexityUsage)
		return ExitOK
	}
	by, ok := complexity.ParseLevel(a.By)
	switch {
	case !ok:
		fmt.Fprintf(stderr, "ike complexity: --by: %q is not class or file\n", a.By)
		return ExitError
	case by != complexity.ByNone && a.Diff:
		fmt.Fprintln(stderr, "ike complexity:", cli.ErrByWithDiff)
		return ExitError
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
	if a.Diff {
		r, err := runDiff(s)
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
	if by != complexity.ByNone {
		g := groupProject(r, by)
		if a.JSON {
			if err := writeGroupsJSON(stdout, g); err != nil {
				fmt.Fprintln(stderr, "ike complexity:", err)
				return ExitError
			}
		} else {
			writeGroupsText(stdout, g)
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
	return s, nil
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
	if repo, err := vcs.DetectRoot(dir); err == nil && repo != "" {
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
}

// projectReport is the measured project.
type projectReport struct {
	s       settings
	files   int
	all     []row // every function, sorted
	rows    []row // the rows the filter keeps
	summary complexity.Summary
	skipped skipped
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
		for _, f := range fns {
			r.all = append(r.all, row{file: rel, fn: f})
		}
	})
	sortRows(r.all)
	fns := make([]complexity.Function, len(r.all))
	for i, rw := range r.all {
		fns[i] = rw.fn
		if s.keep(rw.fn) {
			r.rows = append(r.rows, rw)
		}
	}
	r.summary = complexity.Summarize(fns, s.ccnWarn, s.cogWarn, s.cmtWarn)
	return r
}

// sortRows orders by CCN descending, then cognitive descending, then file
// and line, so the output is stable.
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
		return a.fn.Line < b.fn.Line
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

// groupProject folds the project report's functions into the groups of by,
// keyed by the project-relative path, and applies the group filter.
func groupProject(r projectReport, by complexity.Level) groupReport {
	files := map[string][]complexity.Function{}
	for _, rw := range r.all {
		files[rw.file] = append(files[rw.file], rw.fn)
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
	s       settings
	rev     string // as typed; "HEAD" by default
	sha     string
	diff    complexity.Diff
	skipped skipped
	// baseFiles / headFiles count the measured files on each side.
	baseFiles, headFiles int
}

// diffFile is one file of the comparison: its base blob (baseRel "" when
// the base has none) and its working-tree side (headAbs "" when deleted);
// baseAbs keys a rename's removed side under the old path.
type diffFile struct {
	baseRel, baseAbs, headAbs string
}

// runDiff compares the working tree under the directory against rev: the
// files git reports as changed plus the untracked ones, both sides measured.
func runDiff(s settings) (diffReport, error) {
	rev := s.args.Rev
	if rev == "" {
		rev = "HEAD"
	}
	r := diffReport{s: s, rev: rev}
	repo, err := vcs.DetectRoot(s.dir)
	if err != nil {
		return r, err
	}
	// The project root may sit under a symlink (macOS /var → /private):
	// compare paths against the resolved form, as resolve did for s.dir.
	if ev, err := filepath.EvalSymlinks(repo); err == nil {
		repo = ev
	}
	sha, err := vcs.ResolveRev(repo, rev)
	if err != nil {
		return r, err
	}
	r.sha = sha
	changed, err := vcs.ChangedFiles(repo, sha)
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
		df := diffFile{baseRel: f.Path, baseAbs: head, headAbs: head}
		switch f.Status {
		case vcs.StatusAdded:
			df.baseRel, df.baseAbs = "", ""
		case vcs.StatusDeleted:
			df.headAbs = ""
		case vcs.StatusRenamed:
			if f.OldPath != "" {
				df.baseRel, df.baseAbs = f.OldPath, abs(f.OldPath)
			}
		}
		if !gate(head) {
			continue
		}
		files = append(files, df)
	}
	// Untracked files are not in `git diff`: everything they hold is added.
	// git collapses an untracked directory to one "dir/" record.
	snap, err := vcs.Load(s.dir)
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
		if e.Status != vcs.StatusUntracked {
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

	base := map[string][]complexity.Function{}
	head := map[string][]complexity.Function{}
	var mu sync.Mutex
	parallel(files, func(df diffFile) {
		var bfns, hfns []complexity.Function
		if df.baseRel != "" {
			if content, err := vcs.RevContent(repo, sha, df.baseRel); err == nil {
				bfns, _ = measureFile(df.baseRel, content)
			}
		}
		if df.headAbs != "" {
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
	return r, nil
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
}

// sections lists the changed, added and removed rows the filter keeps, by
// file path then the per-file order DiffFunctions produced.
func (r diffReport) sections() (changed []changeRow, added, removed []row) {
	for _, p := range r.diff.Paths() {
		fd := r.diff.Files[p]
		rel := r.s.rel(p)
		for _, c := range fd.Changed {
			if r.s.keep(c.Before) || r.s.keep(c.After) {
				changed = append(changed, changeRow{file: rel, ch: c})
			}
		}
		for _, f := range fd.Added {
			if r.s.keep(f) {
				added = append(added, row{file: rel, fn: f})
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

// verdictID is the JSON spelling of a verdict.
func verdictID(v complexity.Verdict) string {
	return strings.ReplaceAll(v.String(), " ", "_")
}
