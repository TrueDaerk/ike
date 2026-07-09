package palette

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"ike/internal/fuzzy"
)

// MoveTargetMsg is emitted when a directory-mode item is activated: the user
// picked Dir (project-relative, "." for the root) as the destination of a
// pending file move (file.move, #175). The root model combines it with the
// path it stashed when opening the picker.
type MoveTargetMsg struct{ Dir string }

// DirMode is the directory picker behind file.move (#175): a fuzzy finder over
// the project's directories, opened locked by the root model (it has no
// user-facing prefix story; the rune only satisfies the Mode interface). Like
// FileMode the walk is cached per root and filtered per keystroke.
type DirMode struct {
	// walk lists project-relative directory paths under root. Injectable for
	// tests; defaults to walkProjectDirs.
	walk func(root string) []string

	cachedRoot string
	cached     []string
	haveCache  bool
}

// NewDirMode builds the directory mode using the default on-disk walk.
func NewDirMode() *DirMode { return &DirMode{walk: walkProjectDirs} }

// Prefix implements Mode.
func (d *DirMode) Prefix() rune { return '>' }

// Placeholder implements Mode.
func (d *DirMode) Placeholder() string { return "Move to folder…" }

// Results implements Mode: fuzzy-matched project directories, the root ("./")
// included so a nested file can move to the top level.
func (d *DirMode) Results(query string, cx Context) []Item {
	dirs := d.dirs(cx.Root)
	type scored struct {
		dir   string
		score int
		spans []int
	}
	out := make([]scored, 0, len(dirs))
	for _, p := range dirs {
		m, ok := fuzzy.Match(query, p)
		if !ok {
			continue
		}
		out = append(out, scored{dir: p, score: m.Score, spans: m.Positions})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].dir < out[j].dir
	})
	items := make([]Item, len(out))
	for i, s := range out {
		items[i] = Item{
			Title: s.dir,
			Spans: s.spans,
			Score: s.score,
			Msg:   MoveTargetMsg{Dir: s.dir},
		}
	}
	return items
}

// dirs returns the cached directory list for root, walking once per root.
func (d *DirMode) dirs(root string) []string {
	if d.haveCache && d.cachedRoot == root {
		return d.cached
	}
	if d.walk == nil {
		d.walk = walkProjectDirs
	}
	d.cached = d.walk(root)
	d.cachedRoot = root
	d.haveCache = true
	return d.cached
}

// walkProjectDirs lists directories under root (relative to it, root itself
// first as "./"), applying the same skip rules and cap as the file walk.
func walkProjectDirs(root string) []string {
	if root == "" {
		root = "."
	}
	out := []string{"./"}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == root || !d.IsDir() {
			return nil
		}
		name := d.Name()
		if skipDirs[name] || strings.HasPrefix(name, ".") {
			return filepath.SkipDir
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
