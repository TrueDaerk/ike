package vcs

import "strings"

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
	out, err := runGit(root, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	if err != nil {
		// --quiet suppresses git's diagnostic, so a bad ref arrives as a
		// bare exit status: say what was asked for.
		if !strings.HasPrefix(err.Error(), "git: ") {
			return "", gitError(err, "unknown revision: "+rev)
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
	out, err := runGit(root, "diff", "--name-status", "-M", rev, "--")
	if err != nil {
		return nil, err
	}
	return parseNameStatus(out), nil
}
