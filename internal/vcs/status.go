package vcs

import (
	"bytes"
	"fmt"
	"path"
	"strconv"
	"strings"
)

// gitError folds a subprocess failure and its stderr into one error whose
// message is the decisive git line, not Go's generic "exit status 128".
func gitError(err error, stderr string) error {
	msg := strings.TrimSpace(stderr)
	if msg == "" {
		return err
	}
	// git prefixes diagnostics with "fatal: "/"error: "; the first line is
	// the decisive one.
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	msg = strings.TrimPrefix(msg, "fatal: ")
	msg = strings.TrimPrefix(msg, "error: ")
	return fmt.Errorf("git: %s", msg)
}

// parseStatus decodes `git status --porcelain=v2 --branch -z` output. With -z
// every record is NUL-terminated and rename records carry the original path
// as one extra NUL-terminated field.
func parseStatus(out []byte) *Snapshot {
	snap := &Snapshot{
		Files: map[string]FileStatus{},
		dirs:  map[string]FileStatus{},
	}
	oid := ""
	tokens := bytes.Split(out, []byte{0})
	for i := 0; i < len(tokens); i++ {
		line := string(tokens[i])
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			snap.Branch = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "# branch.oid "):
			oid = strings.TrimPrefix(line, "# branch.oid ")
		case strings.HasPrefix(line, "# branch.ab "):
			ab := strings.Fields(strings.TrimPrefix(line, "# branch.ab "))
			if len(ab) == 2 {
				snap.Ahead, _ = strconv.Atoi(strings.TrimPrefix(ab[0], "+"))
				behind, _ := strconv.Atoi(strings.TrimPrefix(ab[1], "-"))
				if behind < 0 {
					behind = -behind
				}
				snap.Behind = behind
			}
		case strings.HasPrefix(line, "1 "):
			xy, p, ok := ordinaryEntry(line)
			if ok {
				snap.addEntry(p, statusFromXY(xy), xy)
			}
		case strings.HasPrefix(line, "2 "):
			xy, p, ok := renameEntry(line)
			if ok {
				snap.addEntry(p, statusFromXY(xy), xy)
			}
			i++ // skip the trailing "original path" field of the -z record
		case strings.HasPrefix(line, "u "):
			if xy, p, ok := unmergedEntry(line); ok {
				snap.addEntry(p, StatusConflicted, xy)
			}
		case strings.HasPrefix(line, "? "):
			snap.addEntry(strings.TrimPrefix(line, "? "), StatusUntracked, "??")
		}
	}
	if snap.Branch == "(detached)" {
		snap.Detached = true
		snap.Branch = ""
		if len(oid) >= 7 && oid != "(initial)" {
			snap.Branch = oid[:7]
		}
	}
	return snap
}

// addEntry records one parsed status record: the coloring map plus the
// commit-UI entry carrying the staged/worktree letters.
func (s *Snapshot) addEntry(p string, st FileStatus, xy string) {
	if p == "" || st == StatusNone {
		return
	}
	e := FileEntry{Path: p, Status: st}
	if len(xy) == 2 {
		e.X, e.Y = xy[0], xy[1]
	}
	s.Entries = append(s.Entries, e)
	s.add(p, st)
}

// add records one changed file and tints every ancestor directory with the
// dominant status of its subtree (#1053): a directory holding only untracked
// files reads untracked, not modified, so parent and child hues agree.
func (s *Snapshot) add(p string, st FileStatus) {
	if p == "" || st == StatusNone {
		return
	}
	s.Files[p] = st
	for dir := path.Dir(p); ; dir = path.Dir(dir) {
		if dir == "." || dir == "/" {
			dir = ""
		}
		if statusRank(st) > statusRank(s.dirs[dir]) {
			s.dirs[dir] = st
		}
		if dir == "" {
			return
		}
	}
}

// statusRank orders statuses for directory propagation: the strongest signal
// wins — conflicts over content changes over additions over untracked.
func statusRank(st FileStatus) int {
	switch st {
	case StatusConflicted:
		return 5
	case StatusModified, StatusRenamed, StatusDeleted:
		return 4
	case StatusAdded:
		return 3
	case StatusUntracked:
		return 2
	case StatusNone:
		return 0
	}
	return 1
}

// statusFromXY folds the two-letter staged/unstaged pair into one badge.
// Any A dominates (new file), then D, then R; everything else is a change.
func statusFromXY(xy string) FileStatus {
	switch {
	case strings.ContainsAny(xy, "A"):
		return StatusAdded
	case strings.ContainsAny(xy, "D"):
		return StatusDeleted
	case strings.ContainsAny(xy, "RC"):
		return StatusRenamed
	default:
		return StatusModified
	}
}

// ordinaryEntry parses "1 XY sub mH mI mW hH hI path" (8 header fields).
func ordinaryEntry(line string) (xy, p string, ok bool) {
	return entryAt(line, 8)
}

// renameEntry parses "2 XY sub mH mI mW hH hI Xscore path" (9 header fields).
func renameEntry(line string) (xy, p string, ok bool) {
	return entryAt(line, 9)
}

// unmergedEntry parses "u XY sub m1 m2 m3 mW h1 h2 h3 path" (10 header fields).
func unmergedEntry(line string) (xy, p string, ok bool) {
	return entryAt(line, 10)
}

// entryAt splits an entry into its first nFields space-separated header
// fields and returns the XY pair (field 1) plus the remainder as the path.
// The path is everything after the header, so spaces in filenames survive.
func entryAt(line string, nFields int) (xy, p string, ok bool) {
	rest := line
	for i := 0; i < nFields; i++ {
		j := strings.IndexByte(rest, ' ')
		if j < 0 {
			return "", "", false
		}
		if i == 1 {
			xy = rest[:j]
		}
		rest = rest[j+1:]
	}
	return xy, rest, rest != ""
}
