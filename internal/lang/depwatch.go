// depwatch.go is the dependency-update declaration seam (#2613): a language
// names the cheap *toolchain marker paths* whose change means "the dependency
// tree moved" — go.mod/go.sum, package.json and its lock files,
// composer.json/composer.lock, the venv's site-packages directory — plus
// whether its language servers need a restart to notice.
//
// The markers exist because the recursive project watch deliberately prunes
// dependency trees (internal/watch's vendorNoiseDirs and dot directories): a
// `pip install -U` under .venv/lib/python3.12/site-packages, an `npm install`
// under node_modules or a `composer update` under vendor produce no watcher
// event at all, so the running server keeps serving its stale index. Watching
// those trees recursively is not an option (thousands of files); watching a
// handful of marker paths is.
//
// The list is *data on the language*, not a central switch: a language plugin
// declares Language.Deps, and the app registers exactly what the registered
// languages ask for.
package lang

import (
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// DepWatch declares how one language notices dependency updates (#2613).
type DepWatch struct {
	// Files are the marker paths relative to the workspace root, in slash
	// form: "go.mod", "vendor/composer/installed.json". The last segment may
	// carry a `*`/`?`/`[…]` pattern ("requirements*.txt"), which matches
	// events but registers no watch of its own — a glob names no file, and
	// everything inside the root is already covered by the recursive watch.
	Files []string

	// Dirs resolves marker *directories* that only a toolchain probe can name
	// — Python's `<venv>/lib/python3.12/site-packages`, whose `*.dist-info`
	// entries appear and disappear on every install, uninstall and upgrade.
	// The returned paths are absolute and watched **non-recursively**: the
	// directory's own entries are the signal, never its subtree. Nil — the
	// normal case — means the language's markers are all static Files.
	Dirs func(root string) []string

	// Restart marks a language whose servers do not re-scan their dependency
	// index on workspace/didChangeWatchedFiles alone: after a marker change
	// they are restarted for the affected root on top of the notification.
	// Python sets it (pyright resolves site-packages once and keeps the
	// result); Go, TypeScript and PHP notify only.
	Restart bool
}

// DepWatchPaths resolves the concrete marker paths every registered language
// declares at root — the static Files entries that name an actual file, plus
// whatever each language's Dirs probe finds. Returned as path → language id,
// deduplicated; a path two languages claim keeps the alphabetically first
// owner so the result is deterministic.
//
// Glob entries are deliberately absent: they name no path to register. Use
// DepWatchMatch for the matching side, which covers both.
func DepWatchPaths(root string) map[string]string {
	out := map[string]string{}
	for _, l := range depWatchLangs() {
		for _, f := range l.Deps.Files {
			if hasGlobMeta(f) {
				continue
			}
			addMarker(out, l.ID, filepath.Join(root, filepath.FromSlash(f)))
		}
		if l.Deps.Dirs != nil {
			for _, d := range l.Deps.Dirs(root) {
				addMarker(out, l.ID, d)
			}
		}
	}
	return out
}

// addMarker records path → langID unless an alphabetically earlier language already
// claimed it.
func addMarker(out map[string]string, langID, p string) {
	if p == "" {
		return
	}
	if prev, ok := out[p]; ok && prev <= langID {
		return
	}
	out[p] = langID
}

// DepWatchMatch reports the language whose markers cover path, given the
// already-resolved registrations from DepWatchPaths. It is pure matching —
// no filesystem access, no toolchain probe — because it runs on every watcher
// event:
//
//   - path is a registered marker itself (go.mod, site-packages);
//   - path's parent directory is a registered marker directory, which is how
//     a `*.dist-info` entry inside site-packages reports;
//   - path matches a glob marker relative to root ("requirements*.txt").
func DepWatchMatch(root, p string, registered map[string]string) (string, bool) {
	abs := p
	if a, err := filepath.Abs(p); err == nil {
		abs = a
	}
	if l, ok := registered[abs]; ok {
		return l, true
	}
	if l, ok := registered[filepath.Dir(abs)]; ok {
		return l, true
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	slashRel := filepath.ToSlash(rel)
	for _, l := range depWatchLangs() {
		for _, f := range l.Deps.Files {
			if !hasGlobMeta(f) {
				continue
			}
			if ok, _ := path.Match(f, slashRel); ok {
				return l.ID, true
			}
		}
	}
	return "", false
}

// DepWatchRestart reports whether langID's servers must be restarted after a
// dependency-marker change, rather than merely notified.
func DepWatchRestart(langID string) bool {
	l, ok := ByID(langID)
	return ok && l.Deps != nil && l.Deps.Restart
}

// depWatchLangs returns the registered languages declaring dependency
// markers, ordered by id so every derived list is deterministic.
func depWatchLangs() []Language {
	mu.RLock()
	var out []Language
	for _, l := range byID {
		if l.Deps != nil {
			out = append(out, l)
		}
	}
	mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// hasGlobMeta reports whether a marker entry is a pattern rather than a name.
func hasGlobMeta(s string) bool { return strings.ContainsAny(s, "*?[") }
