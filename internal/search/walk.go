package search

import (
	"bufio"
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// walk.go is the pure-Go fallback backend for machines without ripgrep: a
// directory walker + regexp matcher producing the identical Match shape. Like
// rg's defaults (and the explorer's ignore rules) it skips hidden dot-entries,
// .git, gitignored paths, and binary files.

// maxFileSize bounds how much of a single file the fallback reads; bigger
// files are skipped (rg has an internal notion of the same guard).
const maxFileSize = 4 << 20 // 4 MiB

// scanGo walks q.Root, matching every kept file line by line.
func scanGo(ctx context.Context, q Query, c *collector) error {
	re, err := compileQuery(q)
	if err != nil {
		return err
	}
	ig := newIgnoreStack(q.Root)
	stop := filepath.WalkDir(q.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entry: skip, keep scanning
		}
		if ctx.Err() != nil {
			return filepath.SkipAll
		}
		rel, rerr := filepath.Rel(q.Root, path)
		if rerr != nil || rel == "." {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || isHiddenName(d.Name()) || ig.ignored(rel, true) {
				return filepath.SkipDir
			}
			ig.push(path, rel)
			return nil
		}
		if isHiddenName(d.Name()) || ig.ignored(rel, false) || !globsKeep(q, rel) {
			return nil
		}
		if !matchFile(ctx, path, re, c) {
			return filepath.SkipAll // result bound hit
		}
		return nil
	})
	_ = stop
	return ctx.Err() // nil unless cancelled; run() clears cancellation
}

// compileQuery turns the query into one regexp: literal patterns are quoted,
// whole-word wraps \b, case-insensitivity is the (?i) flag — the same
// semantics the rg flags select, so the backends agree.
func compileQuery(q Query) (*regexp.Regexp, error) {
	pat := q.Pattern
	if !q.Regex {
		pat = regexp.QuoteMeta(pat)
	}
	if q.WholeWord {
		pat = `\b(?:` + pat + `)\b`
	}
	if !q.CaseSensitive {
		pat = `(?i)` + pat
	}
	return regexp.Compile(pat)
}

// matchFile scans one file line by line, reporting every submatch. It returns
// false once the collector refuses more results.
func matchFile(ctx context.Context, path string, re *regexp.Regexp, c *collector) bool {
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxFileSize {
		return true
	}
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()

	// Binary sniff: a NUL byte in the head means "not text" (rg's heuristic).
	head := make([]byte, 1024)
	n, _ := f.Read(head)
	if bytes.IndexByte(head[:n], 0) >= 0 {
		return true
	}
	if _, err := f.Seek(0, 0); err != nil {
		return true
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxFileSize)
	line := 0
	for sc.Scan() {
		line++
		if ctx.Err() != nil {
			return false
		}
		text := strings.TrimRight(sc.Text(), "\r")
		for _, loc := range re.FindAllStringIndex(text, -1) {
			m := Match{
				Path:     path,
				Line:     line,
				Text:     text,
				StartCol: runeCol(text, loc[0]),
				EndCol:   runeCol(text, loc[1]),
			}
			if !c.add(m) {
				return false
			}
		}
	}
	return true
}

// globsKeep applies the query's include/exclude globs to a root-relative
// path; exclude wins. A glob matches against the full relative path and
// against the basename, so "*.go" and "sub/*.go" both behave.
func globsKeep(q Query, rel string) bool {
	for _, g := range q.Exclude {
		if globMatch(g, rel) {
			return false
		}
	}
	if len(q.Include) == 0 {
		return true
	}
	for _, g := range q.Include {
		if globMatch(g, rel) {
			return true
		}
	}
	return false
}

func globMatch(glob, rel string) bool {
	rel = filepath.ToSlash(rel)
	if ok, _ := filepath.Match(glob, rel); ok {
		return true
	}
	if ok, _ := filepath.Match(glob, filepath.Base(rel)); ok {
		return true
	}
	return false
}

// isHiddenName mirrors the explorer's hidden rule: dot-prefixed entries.
func isHiddenName(name string) bool { return strings.HasPrefix(name, ".") }
