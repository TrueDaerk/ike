package vcs

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Range history (#1430): `git log -L <start>,<end>:<path>` behind the same
// async, timeout-bounded shape as the 0330 log reads. Git tracks the line
// range across edits and renames itself, so each returned commit carries the
// patch git computed for exactly that range.

// rangeLogMax caps the walked history so a hot range in an old file cannot
// produce unbounded output; the app hints when the cap was hit.
const rangeLogMax = 200

// RangeLogEntry is one commit that touched the tracked range, with the patch
// git computed for the range in that commit.
type RangeLogEntry struct {
	LogEntry
	Patch string
}

// RangeLogMsg carries the range history for Path's StartLine..EndLine
// (1-based, inclusive). Truncated reports that rangeLogMax commits were
// returned and older history may exist.
type RangeLogMsg struct {
	Path      string // repo-relative path the query ran against
	StartLine int
	EndLine   int
	Entries   []RangeLogEntry
	Truncated bool
	Err       error
}

// RangeLogCmd loads the commits that touched path's start..end lines
// (1-based, inclusive; path is repo-relative). Each record is separated by
// \x1e ahead of the header line so the patch text can be split off safely.
func RangeLogCmd(root, path string, start, end int) tea.Cmd {
	return func() tea.Msg {
		out, err := runGit(root, "log",
			"--max-count="+strconv.Itoa(rangeLogMax),
			"--pretty=format:%x1e"+logFormat,
			"-L", strconv.Itoa(start)+","+strconv.Itoa(end)+":"+path)
		msg := RangeLogMsg{Path: path, StartLine: start, EndLine: end}
		if err != nil {
			msg.Err = err
			return msg
		}
		msg.Entries = parseRangeLog(out)
		msg.Truncated = len(msg.Entries) == rangeLogMax
		return msg
	}
}

// parseRangeLog splits the record-separated output into commits: the first
// line of each record is the logFormat header, the rest is the range patch.
func parseRangeLog(out []byte) []RangeLogEntry {
	var entries []RangeLogEntry
	for _, rec := range strings.Split(string(out), "\x1e") {
		if strings.TrimSpace(rec) == "" {
			continue
		}
		header, patch, _ := strings.Cut(rec, "\n")
		parsed := parseLog([]byte(header))
		if len(parsed) != 1 {
			continue
		}
		entries = append(entries, RangeLogEntry{
			LogEntry: parsed[0],
			Patch:    strings.Trim(patch, "\n"),
		})
	}
	return entries
}
