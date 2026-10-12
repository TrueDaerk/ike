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

// settings is the resolved run: directory, exclusions and thresholds.
type settings struct {
	args    cli.ComplexityArgs
	dir     string // absolute project directory
	exclude []string
	ccnWarn int
	cogWarn int
}

// Run executes the report for a and returns the exit code. Output goes to
// stdout, diagnostics to stderr.
func Run(a cli.ComplexityArgs, stdout, stderr io.Writer) int {
	if a.Help {
		fmt.Fprintln(stdout, cli.ComplexityUsage)
		return ExitOK
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

// resolve validates the directory and reads the [complexity] settings of
// the user and project config layers (defaults when there are none); the
// flags override the thresholds.
func resolve(a cli.ComplexityArgs) (settings, error) {
	dir := a.Dir
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return settings{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return settings{}, err
	}
	if !info.IsDir() {
		return settings{}, fmt.Errorf("%s is not a directory", dir)
	}
	c, _ := config.Load(config.Discover(abs))
	s := settings{args: a, dir: abs, exclude: c.Complexity.Exclude,
		ccnWarn: c.Complexity.CCNWarn, cogWarn: c.Complexity.CognitiveWarn}
	if a.CCN > 0 {
		s.ccnWarn = a.CCN
	}
	if a.Cog > 0 {
		s.cogWarn = a.Cog
	}
	if s.ccnWarn <= 0 {
		s.ccnWarn = defaultWarn
	}
	if s.cogWarn <= 0 {
		s.cogWarn = defaultWarn
	}
	return s, nil
}

// keep reports whether a row with these metrics passes the row filter:
// --min N keeps CCN or cognitive ≥ N; an explicit --ccn / --cog keeps rows
// at or above either threshold; no filter flag keeps everything.
func (s settings) keep(ccn, cog int) bool {
	switch {
	case s.args.Min > 0:
		return ccn >= s.args.Min || cog >= s.args.Min
	case s.args.CCN > 0 || s.args.Cog > 0:
		return ccn >= s.ccnWarn || cog >= s.cogWarn
	}
	return true
}

// filtered reports whether any row filter is active.
func (s settings) filtered() bool {
	return s.args.Min > 0 || s.args.CCN > 0 || s.args.Cog > 0
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

// runProject walks the directory and measures every file.
func runProject(s settings) projectReport {
	r := projectReport{s: s}
	var paths []string
	_ = complexity.WalkProject(s.dir, s.exclude, func(path string, l lang.Language, measurable bool) {
		if measurable {
			paths = append(paths, path)
			return
		}
		r.skipped.add(l.ID)
	})
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
		if s.keep(rw.fn.CCN, rw.fn.Cognitive) {
			r.rows = append(r.rows, rw)
		}
	}
	r.summary = complexity.Summarize(fns, s.ccnWarn, s.cogWarn)
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
	// The project directory may sit under a symlink (macOS /var → /private):
	// compare paths against the resolved form git reports.
	if ev, err := filepath.EvalSymlinks(s.dir); err == nil {
		s.dir = ev
		r.s = s
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
	// gate reports whether a file takes part, counting a claimed but
	// unmeasurable one as skipped.
	gate := func(path string) bool {
		if !inside(s.dir, path) {
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
	r.diff = complexity.DiffProject(base, head, s.ccnWarn, s.cogWarn)
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
			if r.s.keep(c.Before.CCN, c.Before.Cognitive) || r.s.keep(c.After.CCN, c.After.Cognitive) {
				changed = append(changed, changeRow{file: rel, ch: c})
			}
		}
		for _, f := range fd.Added {
			if r.s.keep(f.CCN, f.Cognitive) {
				added = append(added, row{file: rel, fn: f})
			}
		}
		for _, f := range fd.Removed {
			if r.s.keep(f.CCN, f.Cognitive) {
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
