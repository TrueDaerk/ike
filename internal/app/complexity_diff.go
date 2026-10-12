package app

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
	"ike/internal/complexitypanel"
	"ike/internal/config"
	"ike/internal/lang"
	"ike/internal/pane"
	"ike/internal/safego"
	"ike/internal/vcs"
)

// complexity_diff.go wires "changes vs HEAD" (Roadmap 0550, #2918) into the
// app: the Complexity window's Changes tab compares the functions of every
// changed file at a base revision against the working tree. The git calls —
// resolving the base, listing the files that differ from it, reading each
// base blob — and both measurements run inside one tea.Cmd with bounded
// concurrency; Update only books generations and hands the finished
// complexitypanel.Changes to the pane.
//
// The diff is pulled, not pushed: once per settled Update pass, while the
// Changes tab is on screen (or the status segment is on, #2921), the model compares the key of the diff the pane
// shows — (VCS snapshot generation, complexity index generation, base
// revision) — with the current one and, when they differ, arms a debounce
// tick that launches the computation. A snapshot refresh, a re-measured
// save or a new base revision therefore recompute within one debounce
// window; a hidden tab costs nothing; one computation runs at a time and a
// stale result is followed by a fresh run.

// cxDiffDebounce collapses the triggers that land together — the VCS
// snapshot refresh and the index's re-measure after a save are both settled
// at 250 ms — into one computation.
const cxDiffDebounce = 300 * time.Millisecond

// cxDiffWorkers bounds the concurrent `git show` + measure jobs.
const cxDiffWorkers = 4

// cxDiffCacheMax bounds the base-side cache before it is dropped whole.
const cxDiffCacheMax = 4096

// cxDiffKey identifies the inputs a diff was computed from.
type cxDiffKey struct {
	snapGen  int    // bumped per distinct VCS snapshot
	indexGen int    // the last ComplexityChangedMsg generation
	rev      string // the base revision as typed ("" = HEAD)
}

// complexityDiffState is the session state behind the Changes tab, shared
// by pointer across value-model copies like vcsState.
type complexityDiffState struct {
	rev       string // the base revision the user wants; "" = HEAD
	snapGen   int
	indexGen  int
	key       cxDiffKey // the inputs of the result the pane shows
	computing bool
	tickArmed bool
	result    complexitypanel.Changes
	// baseCache holds measured base blobs by "sha\x00repo-relative path":
	// a blob at a commit never changes, so a second run against the same
	// base skips the git call for every file it already measured.
	baseCache map[string][]complexity.Function
}

// complexityDiffTickMsg wakes the model to launch the debounced diff.
type complexityDiffTickMsg struct{}

// complexityDiffMsg carries one finished computation (or its failure).
type complexityDiffMsg struct {
	key     cxDiffKey
	changes complexitypanel.Changes
	// err is the git message of a base revision that did not resolve or a
	// listing that failed; the pane keeps the previous diff and base.
	err string
	// base holds the newly measured base blobs for the cache.
	base map[string][]complexity.Function
}

// currentCxDiffKey is the key a diff computed now would carry.
func (m Model) currentCxDiffKey() cxDiffKey {
	return cxDiffKey{snapGen: m.cxDiff.snapGen, indexGen: m.cxDiff.indexGen, rev: m.cxDiff.rev}
}

// noteVCSSnapshotForComplexity bumps the snapshot generation the Changes
// tab keys on: the VCS state stored a snapshot that differs from the last.
func (m Model) noteVCSSnapshotForComplexity() {
	if m.cxDiff != nil {
		m.cxDiff.snapGen++
	}
}

// noteComplexityGen records the index generation the Changes tab keys on.
func (m Model) noteComplexityGen(gen int) {
	if m.cxDiff != nil {
		m.cxDiff.indexGen = gen
	}
}

// complexityDiffWanted reports whether anything consumes the diff: the
// Changes tab on screen, or the status segment (#2921) — the segment is what
// keeps the computation armed on VCS refreshes while the tab is closed.
func (m Model) complexityDiffWanted() bool {
	if m.cxDiff == nil {
		return false
	}
	if p := m.complexityPanel(); p != nil && p.Tab() == complexitypanel.TabChanges {
		return true
	}
	c := config.Get()
	return c != nil && c.Complexity.Enabled && c.Complexity.StatusSegment
}

// complexityDiffSyncCmd runs once per settled Update pass: while the
// Changes tab is on screen and the pane's diff is behind the current inputs,
// it arms the debounce tick (once). Outside a repository the tab is told so
// and nothing runs.
func (m *Model) complexityDiffSyncCmd() tea.Cmd {
	if !m.complexityDiffWanted() {
		return nil
	}
	p := m.complexityPanel()
	st := m.cxDiff
	if m.vcs.snap == nil {
		if !st.result.NotGit {
			st.result = complexitypanel.Changes{NotGit: true, Rev: st.rev}
			st.key = cxDiffKey{}
			if p != nil {
				p.SetChanges(st.result)
			}
		}
		return nil
	}
	if st.result.NotGit {
		// Back inside a repository (a project switch, git init): forget the
		// marker so the next run shows a diff.
		st.result = complexitypanel.Changes{Rev: st.rev}
		if p != nil {
			p.SetChanges(st.result)
		}
	}
	if st.computing || st.tickArmed || m.currentCxDiffKey() == st.key {
		return nil
	}
	st.tickArmed = true
	return tea.Tick(cxDiffDebounce, func(time.Time) tea.Msg { return complexityDiffTickMsg{} })
}

// startComplexityDiff runs on the debounce tick: it gathers the inputs the
// computation needs — the snapshot, the index view, the dirty buffers, the
// settings — and launches the tea.Cmd. Nothing here parses or runs git.
func (m *Model) startComplexityDiff() tea.Cmd {
	st := m.cxDiff
	st.tickArmed = false
	if !m.complexityDiffWanted() || m.vcs.snap == nil || st.computing {
		return nil
	}
	key := m.currentCxDiffKey()
	if key == st.key {
		return nil
	}
	st.computing = true
	st.result.Computing = true
	st.result.Err = ""
	if p := m.complexityPanel(); p != nil {
		p.SetChanges(st.result)
	}
	job := m.complexityDiffJob(key)
	return func() tea.Msg { return runComplexityDiff(job) }
}

// cxDiffJob is everything the off-loop computation reads.
type cxDiffJob struct {
	key      cxDiffKey
	repoRoot string
	projRoot string // "" = whole repository
	exclude  []string
	snap     *vcs.Snapshot
	index    complexity.View
	dirty    map[string]string // absolute path -> unsaved buffer text
	cache    map[string][]complexity.Function
	ccnWarn  int
	cogWarn  int
}

// complexityDiffJob snapshots the model state the computation needs.
func (m Model) complexityDiffJob(key cxDiffKey) cxDiffJob {
	job := cxDiffJob{key: key, repoRoot: m.vcs.snap.Root, snap: m.vcs.snap, dirty: map[string]string{}, cache: m.cxDiff.baseCache}
	if m.complexityIndex != nil {
		job.projRoot = m.complexityIndex.Root()
		job.exclude = m.complexityIndex.Options().Exclude
		job.index = m.complexityIndex.Snapshot()
	}
	c := config.Get().Complexity
	job.ccnWarn, job.cogWarn = c.CCNWarn, c.CognitiveWarn
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			if ed.HasFile() && ed.Dirty() {
				job.dirty[canonicalPath(ed.Path())] = ed.Text()
			}
		}
	}
	return job
}

// cxFileJob is one file of the computation: where its base blob lives
// (baseRel "" for a file the base does not have) and where its head side is
// (headAbs "" for a deleted file); baseAbs keys the removed side of a
// rename under the old path.
type cxFileJob struct {
	baseRel string
	baseAbs string
	headAbs string
}

// runComplexityDiff is the off-loop computation: resolve the base, list the
// files that differ from it, measure both sides of each with bounded
// concurrency, and diff. Every git call carries the package timeout.
func runComplexityDiff(job cxDiffJob) tea.Msg {
	rev := job.key.rev
	if rev == "" {
		rev = "HEAD"
	}
	sha, err := vcs.ResolveRev(job.repoRoot, rev)
	if err != nil {
		return complexityDiffMsg{key: job.key, err: err.Error()}
	}
	changed, err := vcs.ChangedFiles(job.repoRoot, sha)
	if err != nil {
		return complexityDiffMsg{key: job.key, err: err.Error()}
	}
	var files []cxFileJob
	var renames []string
	seen := map[string]bool{}
	for _, f := range changed {
		abs := cxAbs(job.repoRoot, f.Path)
		if !cxMeasurable(job, abs) {
			continue
		}
		seen[f.Path] = true
		fj := cxFileJob{baseRel: f.Path, baseAbs: abs, headAbs: abs}
		switch f.Status {
		case vcs.StatusAdded:
			fj.baseRel, fj.baseAbs = "", ""
		case vcs.StatusDeleted:
			fj.headAbs = ""
		case vcs.StatusRenamed:
			if f.OldPath != "" {
				fj.baseRel, fj.baseAbs = f.OldPath, cxAbs(job.repoRoot, f.OldPath)
				renames = append(renames, cxRel(job, fj.baseAbs)+" → "+cxRel(job, abs))
			}
		}
		files = append(files, fj)
	}
	// Untracked files are not in `git diff`: every function they hold is
	// added.
	for _, e := range job.snap.Entries {
		if e.Status != vcs.StatusUntracked || seen[e.Path] {
			continue
		}
		abs := cxAbs(job.repoRoot, e.Path)
		if !cxMeasurable(job, abs) {
			continue
		}
		files = append(files, cxFileJob{headAbs: abs})
	}

	base := map[string][]complexity.Function{}
	head := map[string][]complexity.Function{}
	newBase := map[string][]complexity.Function{}
	dirty := map[string]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	queue := make(chan cxFileJob)
	for i := 0; i < cxDiffWorkers; i++ {
		wg.Add(1)
		safego.Go("app.runComplexityDiff.worker", func() {
			defer wg.Done()
			for fj := range queue {
				var bfns, hfns []complexity.Function
				var fresh bool
				if fj.baseRel != "" {
					bfns, fresh = cxMeasureBase(job, sha, fj.baseRel)
				}
				var isDirty bool
				if fj.headAbs != "" {
					hfns, isDirty = cxMeasureHead(job, fj.headAbs)
				}
				mu.Lock()
				if fj.baseAbs != "" {
					base[fj.baseAbs] = bfns
					if fresh {
						newBase[sha+"\x00"+fj.baseRel] = bfns
					}
				}
				if fj.headAbs != "" {
					head[fj.headAbs] = hfns
					if isDirty {
						dirty[fj.headAbs] = true
					}
				}
				mu.Unlock()
			}
		})
	}
	for _, fj := range files {
		queue <- fj
	}
	close(queue)
	wg.Wait()

	return complexityDiffMsg{
		key: job.key,
		changes: complexitypanel.Changes{
			Rev:     job.key.rev,
			Sha:     sha,
			Diff:    complexity.DiffProject(base, head, job.ccnWarn, job.cogWarn),
			Dirty:   dirty,
			Renames: renames,
			Ready:   true,
		},
		base: newBase,
	}
}

// cxAbs maps a repo-relative slash path to its absolute form.
func cxAbs(repoRoot, rel string) string {
	return filepath.Join(repoRoot, filepath.FromSlash(rel))
}

// cxRel renders a path relative to the project root for the rename note.
func cxRel(job cxDiffJob, abs string) string {
	root := job.projRoot
	if root == "" {
		root = job.repoRoot
	}
	if rel, err := filepath.Rel(root, abs); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(abs)
}

// cxMeasurable gates a file into the diff: inside the project root (the
// project may be a subdirectory of the repository), of a language with a
// complexity spec, and not excluded by complexity.exclude.
func cxMeasurable(job cxDiffJob, abs string) bool {
	if job.projRoot != "" {
		if rel, err := filepath.Rel(job.projRoot, abs); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return false
		}
	}
	l, ok := lang.ByPath(abs)
	if !ok || l.Complexity == nil {
		return false
	}
	return !complexity.Excluded(job.projRoot, job.exclude, abs)
}

// cxMeasureBase measures the base blob of rel at sha: from the cache when
// a previous run saw it (fresh=false), else by `git show` + Measure. A blob
// git cannot show (the base has no such file) measures as nothing.
func cxMeasureBase(job cxDiffJob, sha, rel string) ([]complexity.Function, bool) {
	if fns, ok := job.cache[sha+"\x00"+rel]; ok {
		return fns, false
	}
	content, err := vcs.RevContent(job.repoRoot, sha, rel)
	if err != nil {
		return nil, true
	}
	return cxMeasureText(rel, content), true
}

// cxMeasureHead measures the working-tree side of abs: the open buffer when
// it is dirty (dirty=true), else the index's functions, else the file on
// disk.
func cxMeasureHead(job cxDiffJob, abs string) ([]complexity.Function, bool) {
	if text, ok := job.dirty[abs]; ok {
		return cxMeasureText(abs, text), true
	}
	if fns, ok := job.index.Files[abs]; ok {
		return fns, false
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, false
	}
	return cxMeasureText(abs, string(data)), false
}

// cxMeasureText measures text as the language path names; an unmeasurable
// text is nothing.
func cxMeasureText(path, text string) []complexity.Function {
	l, ok := lang.ByPath(path)
	if !ok || l.Complexity == nil {
		return nil
	}
	fns, err := complexity.Measure(l.ID, strings.Split(text, "\n"))
	if err != nil {
		return nil
	}
	return fns
}

// applyComplexityDiff stores a finished computation: on success the pane
// shows it and the base the user asked for becomes the shown base; on a git
// error the previous diff and base stay and the message shows inline. The
// settled pass that follows re-arms the tick when the inputs moved
// meanwhile.
func (m *Model) applyComplexityDiff(msg complexityDiffMsg) {
	st := m.cxDiff
	if st == nil {
		return
	}
	st.computing = false
	st.result.Computing = false
	if msg.err != "" {
		st.result.Err = msg.err
		// A base that did not resolve: fall back to the base on screen so
		// the key settles and no second attempt runs.
		st.rev = st.result.Rev
		st.key = m.currentCxDiffKey()
	} else {
		st.result = msg.changes
		st.key = msg.key
		if len(msg.base) > 0 {
			if st.baseCache == nil || len(st.baseCache) > cxDiffCacheMax {
				st.baseCache = map[string][]complexity.Function{}
			}
			for k, v := range msg.base {
				st.baseCache[k] = v
			}
		}
	}
	if p := m.complexityPanel(); p != nil {
		p.SetChanges(st.result)
	}
}

// setComplexityBase handles the pane's 'R' + enter: the new base is wanted
// from now on; the settled pass launches the comparison and a rejected ref
// falls back in applyComplexityDiff.
func (m *Model) setComplexityBase(rev string) {
	if m.cxDiff == nil {
		return
	}
	rev = strings.TrimSpace(rev)
	if rev == "HEAD" {
		rev = ""
	}
	m.cxDiff.rev = rev
	m.cxDiff.result.Err = ""
	if p := m.complexityPanel(); p != nil {
		p.SetChanges(m.cxDiff.result)
	}
}

// seedComplexityChanges hands a (re)opened pane the Changes data the
// session holds, so a layout restore does not start from an empty tab.
func (m *Model) seedComplexityChanges(p *complexitypanel.Model) {
	if p != nil && m.cxDiff != nil {
		p.SetChanges(m.cxDiff.result)
	}
}

// ComplexityChanges exposes the Changes tab's data (tests, the status
// segment of a later step).
func (m Model) ComplexityChanges() complexitypanel.Changes {
	if m.cxDiff == nil {
		return complexitypanel.Changes{}
	}
	return m.cxDiff.result
}
