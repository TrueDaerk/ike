// Package pathcomplete is the shared filesystem path completion engine
// (#541): given a partially typed path it returns the matching directory
// entries and the longest unambiguous extension of the input — the pieces a
// caller needs for shell-style tab completion plus a suggestion list. The
// engine is pure presentation-agnostic logic; rendering and key handling stay
// with the hosting input (settings inputs, the project picker, the ex
// cmdline).
//
// Inputs are completed in the user's own notation: a query written with a
// leading "~" keeps the "~" in every candidate. Hidden entries are offered
// only when the typed base name explicitly starts with a dot. Matching is
// case-sensitive first and falls back to case-insensitive, so "~/dev" still
// finds "~/Development".
package pathcomplete

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MaxCandidates caps the candidate list: completion beyond this many matches
// is not helpful as a suggestion list and the common-prefix extension already
// covers the typing help.
const MaxCandidates = 50

// Result is one completion query's outcome.
type Result struct {
	// Candidates are the full completed inputs, one per matching entry, in
	// the notation the input used (a leading ~ stays a ~). Directories carry
	// a trailing separator so accepting one and completing again descends.
	Candidates []string
	// Completed is the input extended by the longest prefix shared by every
	// candidate — what a tab press should replace the input with. It equals
	// the input when nothing matches or the input is already unambiguous.
	Completed string
}

// Complete returns the completion of input against the filesystem, offering
// files and directories.
func Complete(input string) Result { return complete(input, false) }

// Dirs returns the completion of input offering directories only — the
// project picker's flavor.
func Dirs(input string) Result { return complete(input, true) }

// Expand resolves a leading "~" or "~/" against the home directory. It is
// the single tilde-expansion helper; callers should not keep local copies.
func Expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return home + p[1:]
		}
	}
	return p
}

func complete(input string, dirsOnly bool) Result {
	res := Result{Completed: input}
	if input == "" {
		return res
	}
	dir, base := split(input)
	real := Expand(dir)
	if real == "" {
		real = "."
	}
	entries, err := os.ReadDir(real)
	if err != nil {
		return res
	}
	names := match(entries, real, base, dirsOnly)
	if len(names) == 0 {
		return res
	}
	for _, n := range names {
		res.Candidates = append(res.Candidates, dir+n)
		if len(res.Candidates) == MaxCandidates {
			break
		}
	}
	res.Completed = dir + commonPrefix(names)
	return res
}

// split separates input into the directory part (kept verbatim, including
// its trailing separator) and the partial base name being completed.
// "~/Dev" -> ("~/", "Dev"); "/usr" -> ("/", "usr"); "foo" -> ("", "foo").
func split(input string) (dir, base string) {
	i := strings.LastIndexByte(input, filepath.Separator)
	if i < 0 {
		return "", input
	}
	return input[:i+1], input[i+1:]
}

// match filters the entries of realDir against the partial base name: hidden
// entries only on an explicit leading dot, exact-case prefix matches when any
// exist, case-insensitive ones otherwise. Directory names (including symlinks
// to directories) get the trailing separator. The result is sorted.
func match(entries []os.DirEntry, realDir, base string, dirsOnly bool) []string {
	var exact, folded []string
	lower := strings.ToLower(base)
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(base, ".") {
			continue
		}
		isDir := e.IsDir()
		if !isDir && e.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(filepath.Join(realDir, name)); err == nil && st.IsDir() {
				isDir = true
			}
		}
		if dirsOnly && !isDir {
			continue
		}
		completed := name
		if isDir {
			completed += string(filepath.Separator)
		}
		switch {
		case strings.HasPrefix(name, base):
			exact = append(exact, completed)
		case strings.HasPrefix(strings.ToLower(name), lower):
			folded = append(folded, completed)
		}
	}
	out := exact
	if len(out) == 0 {
		out = folded
	}
	sort.Strings(out)
	return out
}

// commonPrefix returns the longest prefix shared by all names. With mixed
// case among the candidates the shared prefix uses the first name's casing
// and stops where the fold diverges.
func commonPrefix(names []string) string {
	first := names[0]
	for _, n := range names[1:] {
		first = first[:sharedLen(first, n)]
		if first == "" {
			return ""
		}
	}
	return first
}

// sharedLen is the byte length of the case-insensitively shared prefix of a
// and b, never splitting a multi-byte rune.
func sharedLen(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	n := 0
	for i := 0; i < len(ra) && i < len(rb); i++ {
		if ra[i] != rb[i] && !strings.EqualFold(string(ra[i]), string(rb[i])) {
			break
		}
		n += len(string(ra[i]))
	}
	return n
}
