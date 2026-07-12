package vcs

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Inline blame (Roadmap 0320, #468): whole-file `git blame --porcelain`
// parsed into per-line annotations the editor shows dimmed at the end of the
// current line, JetBrains' "Annotate with Git Blame" scaled to one line.

// BlameLine annotates one buffer line with its last commit.
type BlameLine struct {
	Author      string
	Time        time.Time
	Summary     string
	Uncommitted bool // working-tree change, no commit yet
}

// Annotation renders the end-of-line text: "author, when · summary".
func (b BlameLine) Annotation(now time.Time) string {
	if b.Uncommitted {
		return "not committed yet"
	}
	return b.Author + ", " + RelativeTime(b.Time, now) + " · " + b.Summary
}

// BlameMsg carries a file's blame map, keyed by 0-based line.
type BlameMsg struct {
	Path  string
	Lines map[int]BlameLine
	Err   error
}

// BlameCmd blames the working-tree content of path.
func BlameCmd(root, path string) tea.Cmd {
	return func() tea.Msg {
		out, err := runGit(root, "blame", "--porcelain", "--", path)
		if err != nil {
			return BlameMsg{Path: path, Err: err}
		}
		return BlameMsg{Path: path, Lines: parseBlame(out)}
	}
}

// parseBlame decodes porcelain blame: every hunk header names the sha and
// the final line number; commit metadata (author/author-time/summary) appears
// once per sha and is cached across hunks.
func parseBlame(out []byte) map[int]BlameLine {
	type commit struct {
		author  string
		time    time.Time
		summary string
	}
	commits := map[string]*commit{}
	lines := map[int]BlameLine{}

	cur := ""
	final := 0
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "\t"):
			// The content line closes one hunk entry.
			if c, ok := commits[cur]; ok && final > 0 {
				lines[final-1] = BlameLine{
					Author:      c.author,
					Time:        c.time,
					Summary:     c.summary,
					Uncommitted: strings.HasPrefix(cur, "0000000"),
				}
			}
		case strings.HasPrefix(line, "author "):
			commits[cur].author = strings.TrimPrefix(line, "author ")
		case strings.HasPrefix(line, "author-time "):
			if sec, err := strconv.ParseInt(strings.TrimPrefix(line, "author-time "), 10, 64); err == nil {
				commits[cur].time = time.Unix(sec, 0)
			}
		case strings.HasPrefix(line, "summary "):
			commits[cur].summary = strings.TrimPrefix(line, "summary ")
		default:
			// A hunk header: "<sha> <orig> <final> [<n>]" — sha is 40 hex.
			fields := strings.Fields(line)
			if len(fields) >= 3 && len(fields[0]) == 40 {
				cur = fields[0]
				if _, ok := commits[cur]; !ok {
					commits[cur] = &commit{}
				}
				final, _ = strconv.Atoi(fields[2])
			}
		}
	}
	if len(lines) == 0 {
		return nil
	}
	return lines
}

// RelativeTime renders t against now the way blame annotations read:
// "just now", "5 minutes ago", "3 days ago", "2 years ago".
func RelativeTime(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute")
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour")
	case d < 30*24*time.Hour:
		return plural(int(d.Hours()/24), "day")
	case d < 365*24*time.Hour:
		return plural(int(d.Hours()/(24*30)), "month")
	default:
		return plural(int(d.Hours()/(24*365)), "year")
	}
}

func plural(n int, unit string) string {
	if n <= 1 {
		return "1 " + unit + " ago"
	}
	return strconv.Itoa(n) + " " + unit + "s ago"
}
