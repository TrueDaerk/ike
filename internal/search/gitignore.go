package search

import (
	"os"
	"path/filepath"
	"strings"
)

// gitignore.go is a deliberately small .gitignore matcher for the pure-Go
// backend — enough for the common cases (names, extensions, directories,
// anchored paths, `**` prefixes). Full gitignore semantics (negation `!`,
// character classes across separators) stay with the rg backend; when the
// fallback and rg disagree on an exotic pattern, rg is right.

// ignoreRule is one parsed .gitignore line, scoped to the directory whose
// .gitignore declared it.
type ignoreRule struct {
	base    string // root-relative dir of the declaring .gitignore ("" = root)
	pattern string // slash-separated, no trailing slash
	dirOnly bool   // trailing "/" in the source: matches directories only
	rooted  bool   // leading "/" or an inner "/": anchored to base
}

// ignoreStack accumulates rules while the walker descends. Directories are
// visited parent-first, so pushing on entry keeps the active set correct for
// everything beneath.
type ignoreStack struct {
	root  string
	rules []ignoreRule
}

func newIgnoreStack(root string) *ignoreStack {
	s := &ignoreStack{root: root}
	s.push(root, "")
	return s
}

// push loads dir/.gitignore (if any) with rules scoped to relDir.
func (s *ignoreStack) push(dir, relDir string) {
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		return
	}
	if relDir == "." {
		relDir = ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue // negation unsupported: never un-ignores (documented)
		}
		r := ignoreRule{base: filepath.ToSlash(relDir)}
		r.dirOnly = strings.HasSuffix(line, "/")
		line = strings.TrimSuffix(line, "/")
		if strings.HasPrefix(line, "/") {
			line = strings.TrimPrefix(line, "/")
			r.rooted = true
		} else if strings.Contains(line, "/") {
			r.rooted = true // an inner slash anchors to the declaring dir
		}
		r.pattern = line
		if r.pattern != "" {
			s.rules = append(s.rules, r)
		}
	}
}

// ignored reports whether the root-relative path matches an active rule.
func (s *ignoreStack) ignored(rel string, isDir bool) bool {
	rel = filepath.ToSlash(rel)
	for _, r := range s.rules {
		if r.dirOnly && !isDir {
			continue
		}
		local := rel
		if r.base != "" {
			var ok bool
			local, ok = strings.CutPrefix(rel, r.base+"/")
			if !ok {
				continue // rule scoped to a directory this path is not under
			}
		}
		if r.matches(local) {
			return true
		}
	}
	return false
}

// matches tests one rule against a path relative to the rule's base.
func (r ignoreRule) matches(local string) bool {
	pat := r.pattern
	if tail, ok := strings.CutPrefix(pat, "**/"); ok {
		// `**/x` matches x at any depth: test the tail against every suffix.
		segs := strings.Split(local, "/")
		for i := range segs {
			if ok, _ := filepath.Match(tail, strings.Join(segs[i:], "/")); ok {
				return true
			}
		}
		return false
	}
	if r.rooted {
		if ok, _ := filepath.Match(pat, local); ok {
			return true
		}
		// A rooted directory pattern also ignores everything beneath it.
		return strings.HasPrefix(local, pat+"/")
	}
	// Unrooted name: match any path segment (and thereby any subtree whose
	// directory segment matches).
	for _, seg := range strings.Split(local, "/") {
		if ok, _ := filepath.Match(pat, seg); ok {
			return true
		}
	}
	return false
}
