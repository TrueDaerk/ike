package palette

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"ike/internal/fuzzy"
	"ike/internal/pathcomplete"
)

// maxFiles caps how many paths the file walk collects, so a huge tree never
// stalls the palette. The cap is generous; very large repos rely on the query to
// narrow results rather than on listing everything.
const maxFiles = 10000

// FileMode is the "@" mode: a fuzzy file finder over the project tree. It matches
// the query against each file's path relative to the root (directory segments
// included), so "@app/app" finds internal/app/app.go the way Claude Code's file
// picker does. The chosen item carries an OpenFileMsg the root model opens.
//
// The walk is cached per-root for the lifetime of one palette open: Results
// filters the cached snapshot on every keystroke instead of re-walking the
// disk, and the palette drops the cache via Refresh each time it opens (#1372)
// so files created or deleted since the last open are reflected.
type FileMode struct {
	// walk lists project-relative file paths under root. Injectable for tests;
	// defaults to walkProject.
	walk func(root string) []string

	// usage is the optional per-file selection counter (#1419), keyed by the
	// same path the emitted OpenFileMsg carries; nil-safe. Among equal fuzzy
	// scores, more-often-chosen files rank first — match quality still wins.
	usage *Usage

	cachedRoot string
	cached     []string
	haveCache  bool
}

// NewFileMode builds the "@" mode using the default on-disk project walk.
func NewFileMode() *FileMode { return &FileMode{walk: walkProject} }

// SetUsage installs the per-file selection counter (#1419), mirroring
// CommandMode.SetUsage (#773): only selections confirmed from the Run a
// Command / Search Everywhere windows bump it (the root model increments on a
// CountUsage-marked OpenFileMsg).
func (f *FileMode) SetUsage(u *Usage) { f.usage = u }

// Prefix implements Mode.
func (f *FileMode) Prefix() rune { return '@' }

// Placeholder implements Mode.
func (f *FileMode) Placeholder() string { return "Find a file… (/, ~/ for any path)" }

// Results implements Mode. With an empty query it lists files in path order;
// with a query it fuzzy-matches the relative path and ranks by score, then
// usage count (#1419), then path. A query typed as a filesystem path (#1433:
// leading /, ~/, ./ or ../) is served by the shared pathcomplete engine
// instead — the same candidates the ';' picker produces — so '@' also reaches
// files outside the project; a non-path query with no project match falls
// back to filesystem candidates shown by absolute path.
func (f *FileMode) Results(query string, cx Context) []Item {
	if isPathQuery(query) {
		return pathItems(query, '@')
	}
	files := f.files(cx.Root)
	type scored struct {
		path  string
		score int
		usage int
		spans []int
	}
	out := make([]scored, 0, len(files))
	for _, p := range files {
		m, ok := fuzzy.Match(query, p)
		if !ok {
			continue
		}
		out = append(out, scored{
			path:  p,
			score: m.Score,
			usage: f.usage.Count(filepath.Join(cx.Root, p)),
			spans: m.Positions,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		if out[i].usage != out[j].usage {
			return out[i].usage > out[j].usage
		}
		return out[i].path < out[j].path
	})
	items := make([]Item, len(out))
	for i, s := range out {
		items[i] = Item{
			Title: s.path,
			Spans: s.spans,
			Score: s.score,
			Msg:   OpenFileMsg{Path: filepath.Join(cx.Root, s.path)},
		}
	}
	if len(items) == 0 && strings.TrimSpace(query) != "" {
		return fsFallbackItems(cx.Root, query)
	}
	return items
}

// isPathQuery reports whether the '@' query is written as a filesystem path
// (#1433) — absolute, home-relative, or explicitly cwd-relative — and should
// be served by pathcomplete instead of the project fuzzy walk.
func isPathQuery(q string) bool {
	return strings.HasPrefix(q, "/") || q == "~" || strings.HasPrefix(q, "~/") ||
		strings.HasPrefix(q, "./") || strings.HasPrefix(q, "../")
}

// Complete implements Completer (#1433): tab extends a path query through the
// shared engine, exactly like the ';' picker; fuzzy queries stay inert.
func (f *FileMode) Complete(query string) string {
	if isPathQuery(query) {
		return pathcomplete.Complete(query).Completed
	}
	return query
}

// fsFallbackItems (#1433) serves a non-path query that matched nothing in the
// project walk: the same prefix completion the path queries use, anchored at
// root, with every hit shown by absolute path so out-of-project results stay
// visually distinct from the project-relative fuzzy matches. Project matches
// always rank above these — the fallback only exists when there are none.
func fsFallbackItems(root, query string) []Item {
	res := pathcomplete.Complete(filepath.Join(root, query))
	items := make([]Item, 0, len(res.Candidates))
	for _, c := range res.Candidates {
		if strings.HasSuffix(c, string(filepath.Separator)) {
			abs := expandedAbs(c) + string(filepath.Separator)
			items = append(items, Item{Title: abs, Msg: OpenPathDescendMsg{Query: abs, Prefix: '@'}})
			continue
		}
		abs := expandedAbs(c)
		items = append(items, Item{Title: abs, Msg: OpenFileMsg{Path: abs}})
	}
	return items
}

// Refresh implements Refresher (#1372): it drops the cached walk so the next
// Results call re-walks the project and sees files created or deleted since
// the cache filled.
func (f *FileMode) Refresh() {
	f.haveCache = false
	f.cached = nil
	f.cachedRoot = ""
}

// files returns the cached file list for root, walking once per palette open.
func (f *FileMode) files(root string) []string {
	if f.haveCache && f.cachedRoot == root {
		return f.cached
	}
	if f.walk == nil {
		f.walk = walkProject
	}
	f.cached = f.walk(root)
	f.cachedRoot = root
	f.haveCache = true
	return f.cached
}

// skipDirs are directory names never descended into during the file walk.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
}

// walkProject lists files under root (relative to it), skipping hidden entries
// and known heavy directories, capped at maxFiles. Paths use forward slashes for
// stable, platform-independent matching and display.
func walkProject(root string) []string {
	if root == "" {
		root = "."
	}
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if path == root {
			return nil
		}
		if d.IsDir() {
			if skipDirs[name] || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		out = append(out, filepath.ToSlash(rel))
		if len(out) >= maxFiles {
			return filepath.SkipAll
		}
		return nil
	})
	return out
}
