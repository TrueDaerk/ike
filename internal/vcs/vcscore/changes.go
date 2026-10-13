package vcscore

import (
	"fmt"
	"strings"
)

// changes.go is the git plumbing behind the Complexity window's Changes tab
// (Roadmap 0550, #2918): resolving the base revision the user typed and
// listing the files whose working tree differs from it. Both shell out and
// therefore run inside a tea.Cmd, never in Update.

// ResolveRev resolves rev — a branch, a tag, a sha, "HEAD~3", anything
// `git rev-parse` accepts — to the full commit sha, or returns git's own
// error for a ref that does not exist. An empty rev means HEAD. Running
// inside a tea.Cmd is the caller's job.
func ResolveRev(root, rev string) (string, error) {
	rev = strings.TrimSpace(rev)
	if rev == "" {
		rev = "HEAD"
	}
	out, err := RunGit(root, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	if err != nil {
		// --quiet suppresses git's diagnostic, so a bad ref arrives as a
		// bare exit status: say what was asked for.
		if !strings.HasPrefix(err.Error(), "git: ") {
			return "", GitError(err, "unknown revision: "+rev)
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ChangedFiles lists every tracked file whose working tree differs from
// rev (staged and unstaged edits alike), with renames detected, by
// `git diff --name-status <rev>` — the same records ShowCmd decodes, so a
// rename carries its OldPath. Untracked files are not listed: the caller
// adds them from the status snapshot. Running inside a tea.Cmd is the
// caller's job.
func ChangedFiles(root, rev string) ([]CommitFile, error) {
	out, err := RunGit(root, "diff", "--name-status", "-M", rev, "--")
	if err != nil {
		return nil, err
	}
	return ParseNameStatus(out), nil
}

// ParseRevRange splits a revision spec the Changes tab or `--diff` takes
// (#2961): a single ref ("HEAD", "main", "HEAD~3") compares the working
// tree against it (ranged false, base the trimmed spec, head ""); "A..B"
// compares two revisions (ranged true). A range with a missing end ("a..",
// "..b") or git's symmetric difference ("a...b") is an error with a
// message. Neither end is resolved here — ResolveRev does that.
func ParseRevRange(spec string) (base, head string, ranged bool, err error) {
	spec = strings.TrimSpace(spec)
	if strings.Contains(spec, "...") {
		return "", "", false, fmt.Errorf("%q: the symmetric difference A...B is not supported, use A..B", spec)
	}
	a, b, ok := strings.Cut(spec, "..")
	if !ok {
		return spec, "", false, nil
	}
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" || b == "" || strings.Contains(b, "..") {
		return "", "", false, fmt.Errorf("%q: a revision range needs two refs, A..B", spec)
	}
	return a, b, true, nil
}

// ChangedFilesBetween lists every file that differs between two commits,
// with renames detected, by `git diff --name-status -M <base> <head>` —
// the CommitFile records ChangedFiles returns. The working tree and the
// untracked files play no part (#2961). Running inside a tea.Cmd is the
// caller's job.
func ChangedFilesBetween(root, baseSha, headSha string) ([]CommitFile, error) {
	out, err := RunGit(root, "diff", "--name-status", "-M", baseSha, headSha, "--")
	if err != nil {
		return nil, err
	}
	return ParseNameStatus(out), nil
}
