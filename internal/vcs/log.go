package vcs

import (
	"strconv"
	"strings"
	"time"

	"errors"

	tea "charm.land/bubbletea/v2"
)

// errMalformedShow reports git show output that did not match the requested
// format (should not happen with a healthy git).
var errMalformedShow = errors.New("git: unexpected show output")

// History read side (Roadmap 0330, #481): windowed `git log`, one commit's
// details, and blob content at a revision — the data layer behind the VCS
// tool window's Log view. Async and timeout-bounded like the 0320 ops.

// LogEntry is one commit in the log list.
type LogEntry struct {
	Hash      string // full sha
	ShortHash string
	Author    string
	Time      time.Time
	Subject   string
}

// LogMsg carries one log window. HasMore reports that older commits exist
// past Offset+len(Entries).
type LogMsg struct {
	Entries []LogEntry
	Offset  int
	HasMore bool
	Err     error
}

// logFormat is machine-parseable: unit-separator fields, one commit per line
// (subjects never contain \x1f or newlines).
const logFormat = "%H\x1f%h\x1f%an\x1f%at\x1f%s"

// LogCmd loads limit commits starting at offset (0 = HEAD). It asks for one
// extra row to learn whether older history remains.
func LogCmd(root string, offset, limit int) tea.Cmd {
	return func() tea.Msg {
		out, err := runGit(root, "log",
			"--skip="+strconv.Itoa(offset),
			"--max-count="+strconv.Itoa(limit+1),
			"--pretty=format:"+logFormat)
		if err != nil {
			return LogMsg{Offset: offset, Err: err}
		}
		entries := parseLog(out)
		msg := LogMsg{Entries: entries, Offset: offset}
		if len(entries) > limit {
			msg.Entries = entries[:limit]
			msg.HasMore = true
		}
		return msg
	}
}

// parseLog decodes the unit-separator log format.
func parseLog(out []byte) []LogEntry {
	var entries []LogEntry
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(line, "\x1f")
		if len(f) != 5 || f[0] == "" {
			continue
		}
		e := LogEntry{Hash: f[0], ShortHash: f[1], Author: f[2], Subject: f[4]}
		if sec, err := strconv.ParseInt(f[3], 10, 64); err == nil {
			e.Time = time.Unix(sec, 0)
		}
		entries = append(entries, e)
	}
	return entries
}

// CommitFile is one file touched by a commit.
type CommitFile struct {
	Path   string
	Status FileStatus
	// OldPath is set for renames: the pre-rename path (the parent side of
	// the file's diff).
	OldPath string
}

// ShowMsg carries one commit's details for the expanded log row.
type ShowMsg struct {
	Entry LogEntry
	Body  string // full commit message body (subject excluded), trimmed
	Files []CommitFile
	Err   error
}

// emptyTree is git's well-known empty-tree object id: the diff base for a
// root commit, which has no parent to diff against.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// ShowCmd loads one commit's metadata and changed files. The file list diffs
// against the FIRST parent explicitly — `git show --name-status` prints no
// files at all for merge commits (#489), which read as a dead expansion in
// the log view.
func ShowCmd(root, hash string) tea.Cmd {
	return func() tea.Msg {
		meta, err := runGit(root, "show", "-s", "--format="+logFormat+"%x1e%b", hash)
		if err != nil {
			return ShowMsg{Err: err}
		}
		msg := parseShowMeta(meta)
		if msg.Err != nil {
			return msg
		}
		base := hash + "^"
		if _, err := runGit(root, "rev-parse", "--verify", "--quiet", base+"^{commit}"); err != nil {
			base = emptyTree // root commit
		}
		files, err := runGit(root, "diff", "--name-status", "-M", base, hash)
		if err != nil {
			return ShowMsg{Err: err}
		}
		msg.Files = parseNameStatus(files)
		return msg
	}
}

// parseShowMeta decodes the header + record-separated body of ShowCmd.
func parseShowMeta(out []byte) ShowMsg {
	parts := strings.SplitN(string(out), "\x1e", 2)
	if len(parts) != 2 {
		return ShowMsg{Err: errMalformedShow}
	}
	entries := parseLog([]byte(parts[0]))
	if len(entries) != 1 {
		return ShowMsg{Err: errMalformedShow}
	}
	return ShowMsg{Entry: entries[0], Body: strings.TrimSpace(parts[1])}
}

// parseNameStatus decodes `git diff --name-status` lines.
func parseNameStatus(out []byte) []CommitFile {
	var files []CommitFile
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(f) < 2 || f[0] == "" {
			continue
		}
		cf := CommitFile{Path: f[1], Status: statusFromLetter(f[0][0])}
		if cf.Status == StatusRenamed && len(f) >= 3 {
			// name-status renames list "R<score>\told\tnew".
			cf.OldPath, cf.Path = f[1], f[2]
		}
		files = append(files, cf)
	}
	return files
}

// statusFromLetter maps one name-status letter onto the shared FileStatus.
func statusFromLetter(l byte) FileStatus {
	switch l {
	case 'A':
		return StatusAdded
	case 'D':
		return StatusDeleted
	case 'R', 'C':
		return StatusRenamed
	case 'U':
		return StatusConflicted
	default:
		return StatusModified
	}
}

// FileAtMsg carries one file's content at a commit and at its parent, for
// the log view's per-file diff. A side missing from the revision (added or
// deleted files, root commits) resolves to the empty string.
type FileAtMsg struct {
	Hash    string
	Path    string
	Parent  string // content at <hash>^
	Content string // content at <hash>
	Err     error
}

// FileAtCmd loads path's blob at hash and at hash's parent. oldPath names
// the parent-side path for renames; empty means same path.
func FileAtCmd(root, hash, path, oldPath string) tea.Cmd {
	return func() tea.Msg {
		if oldPath == "" {
			oldPath = path
		}
		msg := FileAtMsg{Hash: hash, Path: path}
		if out, err := runGit(root, "show", hash+":"+path); err == nil {
			msg.Content = string(out)
		}
		if out, err := runGit(root, "show", hash+"^:"+oldPath); err == nil {
			msg.Parent = string(out)
		}
		if msg.Content == "" && msg.Parent == "" {
			// Both sides empty means the revision itself is bad — a real
			// added/deleted file always has one side.
			// ^{commit} forces object existence; a bare 40-hex sha would
			// pass --verify syntactically.
			if _, err := runGit(root, "rev-parse", "--verify", hash+"^{commit}"); err != nil {
				msg.Err = err
			}
		}
		return msg
	}
}
