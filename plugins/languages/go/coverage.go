package langgo

// coverage.go implements the coverage seam (#2081) for Go: `go test
// -coverprofile=<tmp>` writes a per-statement-block profile
// ("file.go:startLine.startCol,endLine.endCol numStmts count", one "mode:"
// header line), and parseCoverProfile maps those blocks to the neutral
// per-line model. Block file paths are import-qualified
// ("example.com/mod/pkg/file.go"), so resolution strips the module path read
// from the nearest go.mod above the run's working directory and joins the
// rest onto the module root.

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"ike/internal/lang"
)

// coverArgs is the lang.TestSpec.CoverArgs hook: write the profile. The flag
// does not change which tests run, matching the seam's contract.
func coverArgs(profile string) []string {
	return []string{"-coverprofile=" + profile}
}

// parseCoverProfile is the lang.TestSpec.ParseCover hook. A line touched only
// by executed blocks is covered, only by unexecuted ones uncovered, by both
// partial (two statements sharing a line, one reached). Entries whose path
// cannot be resolved to a file on disk are skipped — coverage of a dependency
// outside the module is not renderable anyway.
func parseCoverProfile(profile, dir string) ([]lang.FileCoverage, error) {
	f, err := os.Open(profile)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	modPath, modRoot := moduleAt(dir)
	type lineState struct{ covered, uncovered bool }
	files := map[string]map[int]*lineState{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		colon := strings.LastIndex(line, ":")
		if colon <= 0 {
			continue
		}
		path := resolveCoverPath(line[:colon], modPath, modRoot, dir)
		if path == "" {
			continue
		}
		// "startLine.startCol,endLine.endCol numStmts count"
		rest := strings.Fields(line[colon+1:])
		if len(rest) != 3 {
			continue
		}
		startLine, endLine, ok := blockLines(rest[0])
		if !ok {
			continue
		}
		count, err := strconv.Atoi(rest[2])
		if err != nil {
			continue
		}
		m := files[path]
		if m == nil {
			m = map[int]*lineState{}
			files[path] = m
		}
		for l := startLine; l <= endLine; l++ {
			st := m[l]
			if st == nil {
				st = &lineState{}
				m[l] = st
			}
			if count > 0 {
				st.covered = true
			} else {
				st.uncovered = true
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	out := make([]lang.FileCoverage, 0, len(files))
	for path, states := range files {
		lines := make(map[int]lang.CoverKind, len(states))
		for l, st := range states {
			switch {
			case st.covered && st.uncovered:
				lines[l] = lang.CoverPartial
			case st.covered:
				lines[l] = lang.CoverCovered
			default:
				lines[l] = lang.CoverUncovered
			}
		}
		out = append(out, lang.FileCoverage{Path: path, Lines: lines})
	}
	return out, nil
}

// blockLines extracts the line span from a "startLine.startCol,endLine.endCol"
// block position.
func blockLines(pos string) (start, end int, ok bool) {
	from, to, found := strings.Cut(pos, ",")
	if !found {
		return 0, 0, false
	}
	parse := func(p string) (int, bool) {
		l, _, found := strings.Cut(p, ".")
		if !found {
			return 0, false
		}
		n, err := strconv.Atoi(l)
		return n, err == nil && n > 0
	}
	if start, ok = parse(from); !ok {
		return 0, 0, false
	}
	if end, ok = parse(to); !ok || end < start {
		return 0, 0, false
	}
	return start, end, true
}

// resolveCoverPath turns a profile entry's file path absolute: an
// import-qualified path is stripped of the module path and joined onto the
// module root; an already absolute path passes through; anything else is
// tried relative to the run's working directory. "" means unresolvable.
func resolveCoverPath(p, modPath, modRoot, dir string) string {
	if modPath != "" && strings.HasPrefix(p, modPath+"/") {
		return filepath.Join(modRoot, filepath.FromSlash(strings.TrimPrefix(p, modPath+"/")))
	}
	if filepath.IsAbs(p) {
		return p
	}
	if abs := filepath.Join(dir, filepath.FromSlash(p)); fileExists(abs) {
		return abs
	}
	return ""
}

// moduleAt walks up from dir to the nearest go.mod and returns its module
// path and directory; empty strings when none is found (GOPATH mode).
func moduleAt(dir string) (modPath, modRoot string) {
	for d := dir; ; {
		data, err := os.ReadFile(filepath.Join(d, "go.mod"))
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if rest, ok := strings.CutPrefix(line, "module"); ok {
					if rest = strings.TrimSpace(rest); rest != "" {
						return strings.Trim(rest, `"`), d
					}
				}
			}
			return "", ""
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", ""
		}
		d = parent
	}
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
