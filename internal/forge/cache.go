package forge

// cache.go is the persistent listing cache (#2108): the last successful open
// listing (issues + pull requests + the fetch timestamp), persisted per
// project in .ike/forgecache.json (IKE_CONFIG_DIR overrides the directory,
// like every other state store), so a freshly started IKE can show the
// last-known listing instantly while the real fetch runs.
//
// The cache also backs the incremental refresh: while the snapshot is younger
// than resyncAfter, a background poll (and the pane's on-open fetch) asks the
// forge only for the issues updated since the snapshot's timestamp and merges
// them in, instead of re-listing everything. Consistency rule: an incremental
// merge can only see what the forge reports as updated — an issue that was
// deleted or transferred lingers until the next full resync. Full resyncs are
// therefore never rare: every manual 'r', every mutation refetch, every
// snapshot older than resyncAfter, any incremental error and any possibly
// truncated incremental page all take the full path.
//
// The cache is keyed to the origin remote URL: a repository or backend switch
// changes the remote, the stored key stops matching, and the stale snapshot
// is ignored (and overwritten by the next successful fetch). A corrupt or
// unreadable file reads as "no cache" — never an error.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
)

// cacheVersion is the on-disk schema version; anything else reads as empty.
const cacheVersion = 1

// resyncAfter bounds how old a snapshot may be and still take the
// incremental path; anything older gets a full resync, which is also what
// re-converges a listing after incremental drift (deleted or transferred
// issues never arrive as "updated").
const resyncAfter = 30 * time.Minute

// sinceOverlap rewinds the incremental cutoff below the snapshot's
// timestamp, so clock skew between IKE and the forge cannot drop an update
// that landed while the recorded fetch ran. The merge is idempotent, so
// re-receiving an issue costs nothing.
const sinceOverlap = time.Minute

// cacheOff is the settings toggle (forge.cache), pushed in by the app; the
// zero value keeps caching on so the forge package works without a config.
var cacheOff atomic.Bool

// SetCacheEnabled applies the forge.cache setting.
func SetCacheEnabled(on bool) { cacheOff.Store(!on) }

// cacheEnabled reports whether the persistent cache is in use.
func cacheEnabled() bool { return !cacheOff.Load() }

// Cache is the persisted snapshot: the open issue listing and every pull
// request, exactly as the IssuesMsg carried them, plus when the fetch that
// produced them started and the origin remote it was fetched from.
type Cache struct {
	Version   int       `json:"version"`
	Remote    string    `json:"remote"`
	FetchedAt time.Time `json:"fetched_at"`
	Issues    []Issue   `json:"issues"`
	PRs       []PR      `json:"prs"`
}

// CacheFile returns the path of the persisted snapshot for dir.
func CacheFile(dir string) string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "forgecache.json")
	}
	return filepath.Join(dir, ".ike", "forgecache.json")
}

// remoteCache memoizes the origin remote per absolute dir, so cache reads and
// writes do not re-run git on every poll. ResetDetection drops it together
// with the backend, keeping the two invalidation seams aligned.
var (
	remoteMu    sync.Mutex
	remoteCache = map[string]string{}
)

// cacheRemote resolves the origin remote URL the cache is keyed to, "" when
// there is none (then nothing is cached). A package variable so the cache
// tests run without a git repository.
var cacheRemote = func(dir string) string {
	key, err := filepath.Abs(dir)
	if err != nil {
		key = dir
	}
	remoteMu.Lock()
	remote, ok := remoteCache[key]
	remoteMu.Unlock()
	if ok {
		return remote
	}
	remote = ""
	if out, err := runGitQuick(dir, "remote", "get-url", "origin"); err == nil {
		remote = strings.TrimSpace(string(out))
	}
	remoteMu.Lock()
	remoteCache[key] = remote
	remoteMu.Unlock()
	return remote
}

// dropCachedRemote forgets the memoized remote for dir (ResetDetection).
func dropCachedRemote(dir string) {
	key, err := filepath.Abs(dir)
	if err != nil {
		key = dir
	}
	remoteMu.Lock()
	delete(remoteCache, key)
	remoteMu.Unlock()
}

// LoadCache reads the persisted snapshot for dir. ok is false when caching is
// off, the file is missing, unreadable or corrupt, the schema version moved
// on, or the snapshot belongs to another remote (repository or backend
// switch) — every one of those simply means "fetch fresh", never an error.
func LoadCache(dir string) (Cache, bool) {
	if !cacheEnabled() {
		return Cache{}, false
	}
	raw, err := os.ReadFile(CacheFile(dir))
	if err != nil {
		return Cache{}, false
	}
	var c Cache
	if json.Unmarshal(raw, &c) != nil || c.Version != cacheVersion {
		return Cache{}, false
	}
	if c.Remote == "" || c.Remote != cacheRemote(dir) {
		return Cache{}, false
	}
	return c, true
}

// storeCache persists one successful open listing, stamped with the moment
// its fetch started (so the next incremental cutoff covers updates that
// landed mid-fetch). Anything that is not a clean open listing — another
// state, a setup or fetch error — is not a snapshot and is skipped; a partial
// result (PRErr) keeps the previously cached PRs, mirroring what the poll
// snapshot does in memory. Write errors are dropped: the cache is a
// convenience, never a failure mode.
func storeCache(dir string, msg IssuesMsg, fetchedAt time.Time) {
	if !cacheEnabled() || msg.State != IssuesOpen || msg.Setup != "" || msg.Err != nil {
		return
	}
	remote := cacheRemote(dir)
	if remote == "" {
		return
	}
	c := Cache{Version: cacheVersion, Remote: remote, FetchedAt: fetchedAt, Issues: msg.Issues, PRs: msg.PRs}
	if msg.PRErr != nil {
		c.PRs = nil
		if old, ok := LoadCache(dir); ok {
			c.PRs = old.PRs
		}
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	path := CacheFile(dir)
	if d := filepath.Dir(path); d != "." {
		if os.MkdirAll(d, 0o755) != nil {
			return
		}
	}
	_ = os.WriteFile(path, data, 0o644)
}

// mergeOpenIssues folds an updated-since listing (fetched in every state)
// into a cached open listing: an updated issue that is open replaces its
// cached row or is inserted in front of the listing; one that is no longer
// open drops out. The base order is kept — the pane sorts client-side anyway
// — and merging the same update twice is a no-op, which is what lets the
// incremental cutoff overlap the previous fetch.
func mergeOpenIssues(base, updates []Issue) []Issue {
	if len(updates) == 0 {
		return base
	}
	drop := map[int]bool{}
	replace := map[int]Issue{}
	var fresh []Issue
	known := map[int]bool{}
	for _, is := range base {
		known[is.Number] = true
	}
	for _, u := range updates {
		open := strings.EqualFold(u.State, "OPEN")
		switch {
		case known[u.Number] && open:
			replace[u.Number] = u
		case known[u.Number]:
			drop[u.Number] = true
		case open:
			fresh = append(fresh, u)
		}
	}
	out := make([]Issue, 0, len(base)+len(fresh))
	out = append(out, fresh...)
	for _, is := range base {
		if drop[is.Number] {
			continue
		}
		if u, ok := replace[is.Number]; ok {
			out = append(out, u)
			continue
		}
		out = append(out, is)
	}
	return out
}

// issuesSince is the optional incremental half of a Forge: listing only the
// issues updated at or after since, in every state (a cached open issue that
// closed must arrive as CLOSED so the merge can drop it). complete is false
// when the answer may be truncated — the caller then resyncs fully rather
// than merging a partial diff. Both bindings implement it; a backend without
// it simply always takes the full path.
type issuesSince interface {
	IssuesSince(since time.Time) (updates []Issue, complete bool, err error)
}

// fetchIncremental attempts one incremental refresh of the open listing:
// cached snapshot + updated-since diff + a fresh PR listing (neither forge
// takes a since filter for pull requests, and the PR tab and the poll diff
// need current states). ok is false whenever the full path must run instead —
// no usable cache, a snapshot past resyncAfter, a backend without the
// incremental lister, an incremental error or a possibly truncated page.
func fetchIncremental(dir string) (IssuesMsg, bool) {
	c, ok := LoadCache(dir)
	if !ok || c.FetchedAt.IsZero() || time.Since(c.FetchedAt) > resyncAfter {
		return IssuesMsg{}, false
	}
	f, setup := Detect(dir)
	if setup != "" {
		return IssuesMsg{}, false
	}
	inc, ok := f.(issuesSince)
	if !ok {
		return IssuesMsg{}, false
	}
	started := time.Now()
	updates, complete, err := inc.IssuesSince(c.FetchedAt.Add(-sinceOverlap))
	if err != nil || !complete {
		return IssuesMsg{}, false
	}
	msg := IssuesMsg{State: IssuesOpen, Issues: mergeOpenIssues(c.Issues, updates)}
	if prs, err := f.PRs(); err != nil {
		msg.PRErr = err
	} else {
		msg.PRs = prs
	}
	storeCache(dir, msg, started)
	return msg, true
}

// CachedListingMsg seeds the issues window with the persisted snapshot
// (#2108) while the real fetch runs: the pane renders it immediately, marked
// stale, and the next fresh listing replaces it through the ordinary
// SetResult path.
type CachedListingMsg struct {
	Issues []Issue
	PRs    []PR
}

// LoadCacheCmd reads the persisted snapshot off the Update loop (the read
// includes a git call resolving the remote key), resolving to a
// CachedListingMsg — or to nothing when there is no usable cache.
func LoadCacheCmd(dir string) tea.Cmd {
	return func() tea.Msg {
		c, ok := LoadCache(dir)
		if !ok {
			return nil
		}
		return CachedListingMsg{Issues: c.Issues, PRs: c.PRs}
	}
}
