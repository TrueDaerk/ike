package complexity

// trend.go is the trend history of the project (Roadmap 0551, #2962): the
// Summary of the whole project at each of the last N first-parent commits.
// The walk is incremental: the oldest point measures the tree at its sha,
// every later point applies the blob diff against the previous one and
// re-measures only the files that changed, so N points cost one tree plus
// N−1 small diffs. Measurements are cached per blob — a file untouched
// between two commits keeps its blob — and the per-point file maps per
// commit sha, so a second walk in the session (a new head, a changed
// window, 'r') re-reads nothing it already saw. The git calls go through
// vcscore; the Trend tab of the Complexity window and `ike complexity
// --trend` both run this off the UI loop.

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"ike/internal/lang"
	"ike/internal/safego"
	"ike/internal/vcs/vcscore"
)

// TrendPoint is the project at one commit: the commit's identity and the
// Summary of every measurable file of its tree under the thresholds the
// walk was given. Files counts the measured files.
type TrendPoint struct {
	Sha     string    `json:"sha"`
	Short   string    `json:"short"`
	Time    time.Time `json:"time"`
	Subject string    `json:"subject"`
	Files   int       `json:"files"`
	Summary Summary   `json:"summary"`
}

// Trend is the walk's result: Points oldest first. Measured counts the
// files the walk read and measured (the rest came from the cache), Git the
// commits it listed or diffed.
type Trend struct {
	Points   []TrendPoint `json:"points"`
	Measured int          `json:"-"`
}

// Newest is the last point (the head), the zero point for an empty trend.
func (t Trend) Newest() TrendPoint {
	if len(t.Points) == 0 {
		return TrendPoint{}
	}
	return t.Points[len(t.Points)-1]
}

// Oldest is the first point, the zero point for an empty trend.
func (t Trend) Oldest() TrendPoint {
	if len(t.Points) == 0 {
		return TrendPoint{}
	}
	return t.Points[0]
}

// DeltaExcess is the excess of the newest point minus the oldest's.
func (t Trend) DeltaExcess() int { return t.Newest().Summary.Excess - t.Oldest().Summary.Excess }

// trendFile is one measured file of a point: its blob and its functions.
type trendFile struct {
	blob string
	fns  []Function
}

// TrendCache remembers measurements across walks: functions per blob (and
// language, so the same bytes under another extension measure again) and
// the file map per commit sha. Safe for concurrent use; the zero value is
// ready. Reset drops everything ('r', a changed exclude list).
type TrendCache struct {
	mu     sync.Mutex
	blobs  map[string][]Function
	points map[string]map[string]trendFile
}

// NewTrendCache returns an empty cache.
func NewTrendCache() *TrendCache { return &TrendCache{} }

// Reset forgets every cached measurement and point.
func (c *TrendCache) Reset() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.blobs, c.points = nil, nil
	c.mu.Unlock()
}

// Blobs reports how many blob measurements the cache holds (tests).
func (c *TrendCache) Blobs() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.blobs)
}

func (c *TrendCache) blob(key string) ([]Function, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	fns, ok := c.blobs[key]
	return fns, ok
}

func (c *TrendCache) putBlob(key string, fns []Function) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.blobs == nil {
		c.blobs = map[string][]Function{}
	}
	c.blobs[key] = fns
	c.mu.Unlock()
}

func (c *TrendCache) point(sha string) (map[string]trendFile, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	files, ok := c.points[sha]
	return files, ok
}

func (c *TrendCache) putPoint(sha string, files map[string]trendFile) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.points == nil {
		c.points = map[string]map[string]trendFile{}
	}
	c.points[sha] = files
	c.mu.Unlock()
}

// TrendJob is everything a walk reads.
type TrendJob struct {
	// RepoRoot is the repository's top level; Root the project root the
	// exclude rules and the thresholds are relative to ("" = RepoRoot). A
	// project that is a subdirectory of the repository measures only its
	// own files.
	RepoRoot string
	Root     string
	// Exclude are the complexity.exclude globs; Keep, when set, gates a
	// file further by its absolute path (the CLI's path selection and
	// test-file rule).
	Exclude []string
	Keep    func(abs string) bool
	// Res resolves a file's thresholds (nil: the defaults).
	Res Resolver
	// Cache serves and receives measurements (nil: none).
	Cache *TrendCache
	// Measure is the measurement seam (nil: Measure through the language
	// registry); it takes the path the language is read from and the text.
	Measure func(path, text string) []Function
	// Progress, when set, is told after every finished point.
	Progress func(done, total int)
	// Workers bounds the concurrent measurements (0: 4).
	Workers int
}

// ListTrendCommits lists the last n first-parent commits of rev ("" =
// HEAD) oldest first — the points of a walk.
func ListTrendCommits(repoRoot, rev string, n int) ([]vcscore.Commit, error) {
	commits, err := vcscore.FirstParentLog(repoRoot, rev, n)
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(commits)-1; i < j; i, j = i+1, j-1 {
		commits[i], commits[j] = commits[j], commits[i]
	}
	return commits, nil
}

// WalkTrend measures the project at every commit, oldest first, and
// returns one point per commit in that order. A git failure ends the walk
// with the error; the points finished so far are returned with it.
func WalkTrend(job TrendJob, commits []vcscore.Commit) (Trend, error) {
	var t Trend
	if job.Root == "" {
		job.Root = job.RepoRoot
	}
	if job.Res == nil {
		job.Res = Uniform(Thresholds{CCN: 15, Cog: 15})
	}
	if job.Measure == nil {
		job.Measure = measureText
	}
	var files map[string]trendFile
	prev := ""
	for i, c := range commits {
		cached, ok := job.Cache.point(c.Sha)
		switch {
		case ok:
			files = cached
		case files == nil:
			tree, err := vcscore.TreeBlobs(job.RepoRoot, c.Sha)
			if err != nil {
				return t, err
			}
			files = map[string]trendFile{}
			var todo []vcscore.TreeEntry
			for _, e := range tree {
				if job.measurable(e.Path) {
					todo = append(todo, e)
				}
			}
			if err := job.measureInto(files, todo, &t.Measured); err != nil {
				return t, err
			}
			job.Cache.putPoint(c.Sha, files)
		default:
			changes, err := vcscore.ChangedBlobsBetween(job.RepoRoot, prev, c.Sha)
			if err != nil {
				return t, err
			}
			next := make(map[string]trendFile, len(files))
			for k, v := range files {
				next[k] = v
			}
			var todo []vcscore.TreeEntry
			for _, ch := range changes {
				// A rename that keeps the blob carries its measurement over
				// without a cache.
				var moved trendFile
				if ch.OldPath != "" {
					moved = next[ch.OldPath]
					delete(next, ch.OldPath)
				}
				if ch.Status == vcscore.StatusDeleted || ch.Blob == "" {
					delete(next, ch.Path)
					continue
				}
				entry := vcscore.TreeEntry{Path: ch.Path, Blob: ch.Blob}
				switch cur := next[ch.Path]; {
				case !job.measurable(ch.Path):
					delete(next, ch.Path)
				case moved.blob != "" && moved.blob == ch.Blob && blobKey(vcscore.TreeEntry{Path: ch.OldPath, Blob: ch.Blob}) == blobKey(entry):
					next[ch.Path] = moved
				case cur.blob != "" && cur.blob == ch.Blob:
					// The content stayed (a mode flip): nothing to measure.
				default:
					todo = append(todo, entry)
				}
			}
			if err := job.measureInto(next, todo, &t.Measured); err != nil {
				return t, err
			}
			files = next
			job.Cache.putPoint(c.Sha, files)
		}
		prev = c.Sha
		t.Points = append(t.Points, TrendPoint{
			Sha: c.Sha, Short: c.Short, Time: c.Time, Subject: c.Subject,
			Files:   len(files),
			Summary: job.summarize(files),
		})
		if job.Progress != nil {
			job.Progress(i+1, len(commits))
		}
	}
	return t, nil
}

// abs maps a repo-relative slash path to its absolute form.
func (job TrendJob) abs(rel string) string {
	return filepath.Join(job.RepoRoot, filepath.FromSlash(rel))
}

// measurable gates a repo path into the walk: inside the project root, of
// a language with a complexity spec, not excluded, and kept by Keep.
func (job TrendJob) measurable(rel string) bool {
	abs := job.abs(rel)
	if job.Root != job.RepoRoot {
		if r, err := filepath.Rel(job.Root, abs); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			return false
		}
	}
	l, ok := lang.ByPath(abs)
	if !ok || l.Complexity == nil {
		return false
	}
	if Excluded(job.Root, job.Exclude, abs) {
		return false
	}
	return job.Keep == nil || job.Keep(abs)
}

// measureInto measures the given blobs — from the cache when it holds them,
// else from one batched git read — and stores them in files under their
// paths. measured counts the blobs read and measured.
func (job TrendJob) measureInto(files map[string]trendFile, todo []vcscore.TreeEntry, measured *int) error {
	var fetch []vcscore.TreeEntry
	for _, e := range todo {
		if fns, ok := job.Cache.blob(blobKey(e)); ok {
			files[e.Path] = trendFile{blob: e.Blob, fns: fns}
			continue
		}
		fetch = append(fetch, e)
	}
	if len(fetch) == 0 {
		return nil
	}
	// One blob can sit under several paths; read it once.
	byBlob := map[string][]vcscore.TreeEntry{}
	var ids []string
	for _, e := range fetch {
		if _, seen := byBlob[e.Blob]; !seen {
			ids = append(ids, e.Blob)
		}
		byBlob[e.Blob] = append(byBlob[e.Blob], e)
	}
	workers := job.Workers
	if workers <= 0 {
		workers = 4
	}
	type item struct {
		entry   vcscore.TreeEntry
		content string
	}
	queue := make(chan item, workers)
	var mu sync.Mutex
	var wg sync.WaitGroup
	count := 0
	for i := 0; i < workers; i++ {
		wg.Add(1)
		safego.Go("complexity.WalkTrend.measure", func() {
			defer wg.Done()
			for it := range queue {
				fns := job.Measure(it.entry.Path, it.content)
				job.Cache.putBlob(blobKey(it.entry), fns)
				mu.Lock()
				files[it.entry.Path] = trendFile{blob: it.entry.Blob, fns: fns}
				count++
				mu.Unlock()
			}
		})
	}
	err := vcscore.BlobContents(job.RepoRoot, ids, func(blob, content string, missing bool) {
		for _, e := range byBlob[blob] {
			if missing {
				mu.Lock()
				files[e.Path] = trendFile{blob: e.Blob}
				mu.Unlock()
				continue
			}
			queue <- item{entry: e, content: content}
		}
	})
	close(queue)
	wg.Wait()
	*measured += count
	return err
}

// blobKey keys a measurement: the blob and the language its path resolves
// to.
func blobKey(e vcscore.TreeEntry) string {
	id := ""
	if l, ok := lang.ByPath(e.Path); ok {
		id = l.ID
	}
	return e.Blob + "\x00" + id
}

// summarize folds the point's files into one Summary, each file under the
// thresholds the resolver gives its absolute path.
func (job TrendJob) summarize(files map[string]trendFile) Summary {
	var a Summer
	for rel, f := range files {
		if len(f.fns) == 0 {
			continue
		}
		t := job.Res(job.abs(rel))
		for _, fn := range f.fns {
			a.Add(fn, t)
		}
	}
	return a.Summary()
}

// measureText measures text as the language of path; an unmeasurable
// text is nothing.
func measureText(path, text string) []Function {
	l, ok := lang.ByPath(path)
	if !ok || l.Complexity == nil {
		return nil
	}
	fns, err := Measure(l.ID, SourceLines(path, text))
	if err != nil {
		return nil
	}
	return fns
}

// TrendPaths lists the files of a cached point, sorted (tests).
func (c *TrendCache) TrendPaths(sha string) []string {
	files, ok := c.point(sha)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(files))
	for p := range files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
