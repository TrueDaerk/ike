package palette

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ike/internal/frecency"
	"ike/internal/fuzzy"
	"ike/internal/pathcomplete"
)

// maxFiles caps how many paths the file walk collects, so a huge tree never
// stalls the palette. The cap is generous; very large repos rely on the query to
// narrow results rather than on listing everything.
const maxFiles = 10000

// shortQueryLen is the query length up to which frecency leads the ranking
// (#2155). With no query — or one or two characters, where the fuzzy score
// barely discriminates anyway — the files one actually works on belong on top;
// from the third character the typed text is a real signal and match quality
// takes the lead again, with frecency demoted to a tiebreak.
const shortQueryLen = 2

// maxFsFallback caps the filesystem rows appended below the project matches
// (#1775), per anchor (project root, home). The fallback is a reachability
// affordance, not a browser — a long list would bury the project hits.
const maxFsFallback = 8

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

	// home resolves the home directory the second filesystem fallback anchor
	// is taken from (#1775). Injectable for tests — a test that returns ""
	// keeps the fallback project-root-only and therefore deterministic.
	// Defaults to os.UserHomeDir.
	home func() string

	// usage is the optional per-file selection counter (#1419), keyed by the
	// same path the emitted OpenFileMsg carries; nil-safe. Among equal fuzzy
	// scores, more-often-chosen files rank first — match quality still wins.
	usage *Usage

	// frecency scores project files by how often and how recently they were
	// opened (#2155), keyed by frecency.Key of the path the emitted
	// OpenFileMsg carries — the root model bumps it under the same key, so
	// finder and opener agree however the path was spelled; nil-safe. Unlike
	// usage it counts every open of the file, not only palette confirmations:
	// the point is to know what one is working on, however the file was
	// reached.
	frecency *frecency.Store

	// scratchList returns the scratch store's paths newest-first (#1812),
	// mirroring ScratchMode's injection: the palette core owns no store, the
	// root model wires internal/scratch.List in. Nil-safe — a finder built
	// without SetScratchList never offers scratch rows.
	scratchList func() []string

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

// SetFrecency installs the file-open frecency store (#2155). The root model
// owns it and bumps it whenever a file is opened or re-activated; the finder
// only reads it.
func (f *FileMode) SetFrecency(s *frecency.Store) { f.frecency = s }

// SetScratchList installs the scratch-store source (#1812), so a query that
// hints at "scratch" can offer scratch files inline without switching to
// ScratchMode. Mirrors NewScratchMode's injection.
func (f *FileMode) SetScratchList(list func() []string) { f.scratchList = list }

// Prefix implements Mode.
func (f *FileMode) Prefix() rune { return '@' }

// CodePreview implements PreviewMode (#2053): the centered file picker
// (project.goToFile) shows the head of the selected file beside the list, so
// one can tell two same-named files apart before opening either. Only a
// locked, centered open splits — the anchored "@" finder over an editor pane
// and the file rows composed into Search Everywhere keep one column.
func (f *FileMode) CodePreview() bool { return true }

// Placeholder implements Mode.
func (f *FileMode) Placeholder() string { return "Find a file… (tab completes; /, ~/ any path)" }

// Results implements Mode. With an empty or very short query (up to
// shortQueryLen) it ranks by frecency (#2155) — the files opened most often
// and most recently first — so the finder opens on what one is working on
// instead of on the alphabetical head of the tree. From the third character
// the fuzzy score of the relative path leads and frecency is only a tiebreak
// above the usage count (#1419), then path. A query typed as a filesystem path
// (#1433: leading /, ~/, ./ or ../) is served by the shared pathcomplete engine
// instead — the same candidates the ';' picker produces — so '@' also reaches
// files outside the project. Below the project matches every non-empty query
// also offers filesystem candidates for the same text (#1775), so a file like
// ~/notes.txt is reachable by typing a fragment of its name instead of its
// whole path.
func (f *FileMode) Results(query string, cx Context) []Item {
	if isPathQuery(query) {
		return pathItems(query, '@')
	}
	files := f.files(cx.Root)
	type scored struct {
		path  string
		score int
		usage int
		frec  float64
		spans []int
	}
	// The frecency key prefix is resolved once per call, not per candidate:
	// frecency.Key consults the working directory, and the walk holds up to
	// maxFiles paths re-ranked on every keystroke. An empty root means the
	// working directory, the same file the joined OpenFileMsg path names.
	frecRoot := ""
	if f.frecency != nil {
		if root := cx.Root; root != "" {
			frecRoot = frecency.Key(root)
		} else {
			frecRoot = frecency.Key(".")
		}
	}
	out := make([]scored, 0, len(files))
	for _, p := range files {
		m, ok := fuzzy.Match(query, p)
		if !ok {
			continue
		}
		frec := 0.0
		if f.frecency != nil {
			frec = f.frecency.Score(filepath.Join(frecRoot, p))
		}
		out = append(out, scored{
			path:  p,
			score: m.Score,
			usage: f.usage.Count(filepath.Join(cx.Root, p)),
			frec:  frec,
			spans: m.Positions,
		})
	}
	// Frecency leads while the query is too short to discriminate, and is a
	// tiebreak below the fuzzy score once it is not (#2155).
	frecencyLeads := len([]rune(strings.TrimSpace(query))) <= shortQueryLen
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if frecencyLeads && !sameFrecency(a.frec, b.frec) {
			return a.frec > b.frec
		}
		if a.score != b.score {
			return a.score > b.score
		}
		if !sameFrecency(a.frec, b.frec) {
			return a.frec > b.frec
		}
		if a.usage != b.usage {
			return a.usage > b.usage
		}
		return a.path < b.path
	})
	items := make([]Item, len(out))
	seen := make(map[string]bool, len(out))
	for i, s := range out {
		abs := filepath.Join(cx.Root, s.path)
		seen[expandedAbs(abs)] = true
		items[i] = Item{
			Title:   s.path,
			Spans:   s.spans,
			Score:   s.score,
			Msg:     OpenFileMsg{Path: abs},
			Preview: PreviewTarget{Path: abs, Line: 1},
		}
	}
	if q := strings.TrimSpace(query); q != "" {
		items = append(items, f.scratchItems(q)...)
		items = append(items, f.fsFallbackItems(cx.Root, query, seen)...)
	}
	return items
}

// sameFrecency reports whether two frecency scores are close enough to count
// as tied. Scores are decayed floats, so an exact comparison would let
// rounding noise — two files opened in the same instant, decayed a fortnight
// later — decide an order the usage and path tiebreaks should decide.
func sameFrecency(a, b float64) bool {
	d := a - b
	return d < 1e-9 && d > -1e-9
}

// scratchItems offers the scratch store's files inline in the '@' finder
// (#1812) when the query hints at "scratch" — fuzzy-matched against the
// literal word "scratch" itself, not against each scratch file's own name.
// That is the simplest disambiguation that satisfies "typing scratch lists
// the scratch files" while leaving every query unrelated to that word
// unaffected, at the cost of not surfacing a scratch file by its own name
// (e.g. a scratch named "notes.go" needs the '~' mode or the word "scratch"
// typed). Results are newest-first (the store's order), like ScratchMode,
// and tagged with a Detail chip so they read as scratch, not project, files.
func (f *FileMode) scratchItems(query string) []Item {
	if f.scratchList == nil {
		return nil
	}
	if _, ok := fuzzy.Match(query, "scratch"); !ok {
		return nil
	}
	paths := f.scratchList()
	items := make([]Item, 0, len(paths))
	for _, p := range paths {
		items = append(items, Item{
			Title:   filepath.Base(p),
			Detail:  "scratch",
			Msg:     OpenFileMsg{Path: p},
			Preview: PreviewTarget{Path: p, Line: 1},
		})
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
// shared engine, exactly like the ';' picker. A fuzzy query has no textual
// completion of its own — it is completed from the selected row instead
// (CompleteItem, #1775).
func (f *FileMode) Complete(query string) string {
	if isPathQuery(query) {
		return pathcomplete.Complete(query).Completed
	}
	return query
}

// CompleteItem implements ItemCompleter (#1775): on a fuzzy query tab adopts
// the selected candidate as the new query — a project hit by its relative
// path, a filesystem hit by the path it is titled with. A directory keeps its
// trailing separator, so the completed query is a path query again and the
// next tab (or keystroke) descends into it. Path queries decline: their
// common-prefix completion in Complete is the shell-like behavior users expect
// there.
func (f *FileMode) CompleteItem(query string, sel Item) (string, bool) {
	if isPathQuery(query) {
		return query, false
	}
	switch sel.Msg.(type) {
	case OpenPathDescendMsg, OpenFileMsg:
		return sel.Title, true
	}
	return query, false
}

// fsFallbackItems (#1433, widened in #1775) offers filesystem candidates for a
// non-path query below the project matches: the same prefix completion the
// path queries use, anchored both at the project root and at the home
// directory, so out-of-project files are reachable by name fragment instead of
// full path. Root hits are shown by absolute path so they stay visually
// distinct from the project-relative fuzzy matches; home hits keep the "~/"
// notation. seen collects the absolute paths already listed (project matches
// included) so nothing is offered twice.
func (f *FileMode) fsFallbackItems(root, query string, seen map[string]bool) []Item {
	items := fsCandidates(filepath.Join(root, query), "", seen)
	if home := f.homeDir(); home != "" {
		items = append(items, fsCandidates(filepath.Join(home, query), home, seen)...)
	}
	return items
}

// fsCandidates turns one pathcomplete input into fallback rows, capped at
// maxFsFallback and skipping absolute paths already in seen. A non-empty home
// titles the rows in "~/" notation — shorter, and a valid path query when tab
// adopts it; otherwise rows are titled by absolute path.
func fsCandidates(input, home string, seen map[string]bool) []Item {
	res := pathcomplete.Complete(input)
	items := make([]Item, 0, len(res.Candidates))
	for _, c := range res.Candidates {
		if len(items) >= maxFsFallback {
			break
		}
		isDir := strings.HasSuffix(c, string(filepath.Separator))
		abs := expandedAbs(c)
		if seen[abs] {
			continue
		}
		seen[abs] = true
		title := abs
		if home != "" {
			if rel, err := filepath.Rel(home, abs); err == nil && !strings.HasPrefix(rel, "..") {
				title = "~" + string(filepath.Separator) + rel
			}
		}
		if isDir {
			title += string(filepath.Separator)
			items = append(items, Item{Title: title, Msg: OpenPathDescendMsg{Query: title, Prefix: '@'}})
			continue
		}
		items = append(items, Item{Title: title, Msg: OpenFileMsg{Path: abs}, Preview: PreviewTarget{Path: abs, Line: 1}})
	}
	return items
}

// homeDir resolves the home anchor of the filesystem fallback; "" disables it.
func (f *FileMode) homeDir() string {
	if f.home != nil {
		return f.home()
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
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
