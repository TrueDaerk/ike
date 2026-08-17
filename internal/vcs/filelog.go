package vcs

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// File history (#1916): `git log --follow -- <file>` behind the same async,
// timeout-bounded shape as the 0330 log reads — the git half of the per-file
// Timeline view. Each commit also carries the path the file had *in that
// commit*, so `git show <hash>:<path>` resolves across renames (which is the
// whole point of --follow).

// FileLogEntry is one commit that touched the queried file.
type FileLogEntry struct {
	LogEntry
	// Path is the file's repo-relative path in this commit — it differs from
	// the queried path for commits older than a rename.
	Path string
}

// FileLogMsg carries one window of a file's commit history, newest-first.
// HasMore reports that older commits exist past Offset+len(Entries), which is
// what drives the Timeline's incremental loading.
type FileLogMsg struct {
	Path    string // repo-relative path the query ran against
	Offset  int
	Entries []FileLogEntry
	HasMore bool
	Err     error
}

// FileLogCmd loads the limit commits touching path (repo-relative) that follow
// the offset newest ones, and asks for one commit past the window to learn
// whether older history remains. Records are separated by \x1e ahead of the
// header line so the --name-only paths can be split off safely.
//
// The window is cut in-process rather than with `--skip`: git's --skip and
// --follow do not compose — rename following re-anchors on the walked commits,
// so a skip past the rename returns nothing at all. Walking from HEAD every
// time costs a re-walk of the already-shown prefix, which is cheap next to
// getting a file's history wrong at its rename.
func FileLogCmd(root, path string, offset, limit int) tea.Cmd {
	return func() tea.Msg {
		out, err := runGit(root, "log", "--follow", "--name-only",
			"--max-count="+strconv.Itoa(offset+limit+1),
			"--pretty=format:%x1e"+logFormat, "--", path)
		msg := FileLogMsg{Path: path, Offset: offset}
		if err != nil {
			msg.Err = err
			return msg
		}
		entries := parseFileLog(out, path)
		if offset >= len(entries) {
			return msg // the history ended inside an earlier window
		}
		entries = entries[offset:]
		msg.Entries = entries
		if len(entries) > limit {
			msg.Entries = entries[:limit]
			msg.HasMore = true
		}
		return msg
	}
}

// parseFileLog splits the record-separated output into commits: the first line
// of each record is the logFormat header, the remaining non-empty lines are
// the --name-only paths. fallback names the path for a commit git printed no
// name for (merge commits list none).
func parseFileLog(out []byte, fallback string) []FileLogEntry {
	var entries []FileLogEntry
	for _, rec := range strings.Split(string(out), "\x1e") {
		if strings.TrimSpace(rec) == "" {
			continue
		}
		header, names, _ := strings.Cut(rec, "\n")
		parsed := parseLog([]byte(header))
		if len(parsed) != 1 {
			continue
		}
		entry := FileLogEntry{LogEntry: parsed[0], Path: fallback}
		for _, name := range strings.Split(names, "\n") {
			if name = strings.TrimSpace(name); name != "" {
				entry.Path = name
				break
			}
		}
		entries = append(entries, entry)
	}
	return entries
}
