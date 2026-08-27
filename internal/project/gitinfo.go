package project

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// gitinfo.go enriches the recent-projects picker rows with each project's git
// context (#2178): the current branch plus a dirty marker for uncommitted
// changes. Choosing a project needs that context — "which of my checkouts is
// on the feature branch, and where did I leave work behind?" — but reading it
// means one subprocess per row, so it must never sit in the open path.
//
// The flow is therefore fully asynchronous: opening the picker lists the
// history instantly and fires one bounded probe per entry as a tea.Cmd; each
// result lands as a GitInfoMsg, the root model files it in the shared GitCache
// and refreshes the palette, and the row fills in. Anything that is not a
// clean answer — a non-git root, a missing git binary, a slow or failing
// invocation — degrades to the plain row it already was.

const (
	// gitProbeTimeout bounds one probe. Deliberately tighter than vcs's 5s
	// gitTimeout: a picker row that has not answered within a second is
	// better left plain than filled in long after the eye moved on.
	gitProbeTimeout = time.Second
	// gitProbeParallel caps how many git subprocesses run at once. The
	// probes are independent tea.Cmds, so without a bound a long history
	// would fork thirty gits into the machine the IDE is running on.
	gitProbeParallel = 4
	// maxGitProbes caps how many history entries are probed per open. The
	// list is newest-first and the visible window is far shorter, so the
	// tail of a long history is simply not worth a subprocess.
	maxGitProbes = 24
	// maxBranchWidth clips long branch names in the badge, mid-word like
	// the status line's branch segment: the row's name is the primary
	// information and must not be crowded out.
	maxBranchWidth = 20
)

// gitProbeSem enforces gitProbeParallel across all in-flight probes.
var gitProbeSem = make(chan struct{}, gitProbeParallel)

// GitInfo is one project's git context for the picker (#2178). The zero value
// — Repo false — is the "nothing to show" case every failure resolves to:
// not a repository, no git binary, timeout, unparsable output.
type GitInfo struct {
	// Path is the project root the probe ran for, as listed in the history;
	// it keys the GitCache and need not be the repository top level.
	Path string
	// Branch is the current branch name, or the short commit hash on a
	// detached HEAD. Empty on a repository without commits.
	Branch string
	// Dirty reports uncommitted changes — staged, unstaged or untracked.
	Dirty bool
	// Repo marks a successful probe of an actual git repository. False
	// leaves the row plain.
	Repo bool
}

// DirtyMarker is appended to the branch name of a project with uncommitted
// changes (#2178) — the shell-prompt convention, one cell, no color needed.
const DirtyMarker = "*"

// Badge renders the git context as the picker row's badge suffix (#2178):
// "⎇ main" for a clean checkout, "⎇ main*" with uncommitted changes, and ""
// whenever there is nothing to say — the row then renders exactly as before.
func (g GitInfo) Badge() string {
	if !g.Repo || g.Branch == "" {
		return ""
	}
	branch := g.Branch
	if len([]rune(branch)) > maxBranchWidth {
		branch = string([]rune(branch)[:maxBranchWidth-1]) + "…"
	}
	if g.Dirty {
		branch += DirtyMarker
	}
	return "⎇ " + branch
}

// GitInfoMsg carries one finished probe back into Update (#2178). The root
// model files it in the picker's GitCache and refreshes the palette; a msg for
// a project that is no longer listed is simply cached and never rendered.
type GitInfoMsg struct{ Info GitInfo }

// GitCache holds the probe results keyed by project root, shared by the
// switch and peek flavours of the picker so one probe fills both lists. It is
// written from the Update loop and read during render — the same
// single-threaded discipline every other picker cache follows; the probes
// themselves run off-loop and communicate only by msg.
type GitCache struct{ m map[string]GitInfo }

// NewGitCache builds an empty cache.
func NewGitCache() *GitCache { return &GitCache{m: map[string]GitInfo{}} }

// Set records a finished probe. A nil cache is a no-op, so the picker works
// unwired (tests, embeddings).
func (c *GitCache) Set(info GitInfo) {
	if c == nil {
		return
	}
	if c.m == nil {
		c.m = map[string]GitInfo{}
	}
	c.m[info.Path] = info
}

// Get returns the cached info for path; ok is false while the probe is still
// in flight (or was never started), which renders as a plain row.
func (c *GitCache) Get(path string) (GitInfo, bool) {
	if c == nil || c.m == nil {
		return GitInfo{}, false
	}
	info, ok := c.m[path]
	return info, ok
}

// EnrichCmd probes the picker's projects (#2178): one bounded git invocation
// per history entry, capped at maxGitProbes and run at most gitProbeParallel
// at a time, each answering with its own GitInfoMsg so rows fill in as
// results arrive. Returns nil when there is nothing to probe.
func EnrichCmd(history []Entry) tea.Cmd {
	if len(history) > maxGitProbes {
		history = history[:maxGitProbes]
	}
	cmds := make([]tea.Cmd, 0, len(history))
	seen := map[string]bool{}
	for _, e := range history {
		if e.Path == "" || seen[e.Path] {
			continue
		}
		seen[e.Path] = true
		cmds = append(cmds, GitInfoCmd(e.Path))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// GitInfoCmd probes one project root off the Update loop. It never fails
// loudly: every error resolves to the zero GitInfo for path, i.e. a plain row.
func GitInfoCmd(path string) tea.Cmd {
	return func() tea.Msg {
		gitProbeSem <- struct{}{}
		defer func() { <-gitProbeSem }()
		return GitInfoMsg{Info: probeGit(path, gitProbeTimeout)}
	}
}

// probeGit runs the single git invocation behind a row: porcelain v2 status
// with the branch header, which answers both questions — branch name and
// whether anything is uncommitted — in one process. Untracked files count as
// dirty (they are work left behind just as much as an unstaged edit), but
// ignored files are not requested: unlike vcs.Load, which needs them for
// explorer dimming, listing them here would only cost time.
func probeGit(path string, timeout time.Duration) GitInfo {
	if path == "" {
		return GitInfo{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", path, "status", "--porcelain=v2", "--branch", "-z")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		// Not a repository, no git binary, or the timeout fired — all of
		// them mean "nothing to show" for this row.
		return GitInfo{Path: path}
	}
	info := parseGitProbe(stdout.Bytes())
	info.Path = path
	return info
}

// parseGitProbe decodes `git status --porcelain=v2 --branch -z` down to what
// a picker row shows: the branch (the commit hash on a detached HEAD) and
// whether any change record is present at all. Every record is
// NUL-terminated, so a split on NUL is enough — no record needs its fields.
func parseGitProbe(out []byte) GitInfo {
	info := GitInfo{Repo: true}
	oid := ""
	for _, tok := range bytes.Split(out, []byte{0}) {
		line := string(tok)
		switch {
		case line == "":
		case strings.HasPrefix(line, "# branch.head "):
			info.Branch = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "# branch.oid "):
			oid = strings.TrimPrefix(line, "# branch.oid ")
		case strings.HasPrefix(line, "# "):
			// Any other header (branch.upstream, branch.ab) — not a change.
		default:
			// "1 ", "2 ", "u ", "? " — one uncommitted change is enough;
			// the rename record's trailing path field needs no skipping
			// because the marker is boolean.
			info.Dirty = true
		}
	}
	if info.Branch == "(detached)" {
		info.Branch = ""
		if len(oid) >= 7 && oid != "(initial)" {
			info.Branch = oid[:7]
		}
	}
	return info
}
