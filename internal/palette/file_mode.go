package palette

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"ike/internal/fuzzy"
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
// The walk is cached per-root for the lifetime of one root: Results filters the
// cached snapshot on every keystroke instead of re-walking the disk.
type FileMode struct {
	// walk lists project-relative file paths under root. Injectable for tests;
	// defaults to walkProject.
	walk func(root string) []string

	cachedRoot string
	cached     []string
	haveCache  bool
}

// NewFileMode builds the "@" mode using the default on-disk project walk.
func NewFileMode() *FileMode { return &FileMode{walk: walkProject} }

// Prefix implements Mode.
func (f *FileMode) Prefix() rune { return '@' }

// Placeholder implements Mode.
func (f *FileMode) Placeholder() string { return "Find a file…" }

// Results implements Mode. With an empty query it lists files in path order; with
// a query it fuzzy-matches the relative path and ranks by score then path.
func (f *FileMode) Results(query string, cx Context) []Item {
	files := f.files(cx.Root)
	type scored struct {
		path  string
		score int
		spans []int
	}
	out := make([]scored, 0, len(files))
	for _, p := range files {
		m, ok := fuzzy.Match(query, p)
		if !ok {
			continue
		}
		out = append(out, scored{path: p, score: m.Score, spans: m.Positions})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
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
	return items
}

// files returns the cached file list for root, walking once per root.
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
