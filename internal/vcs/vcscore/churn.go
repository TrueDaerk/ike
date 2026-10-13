package vcscore

import (
	"strings"
	"time"
)

// churn.go is the change-frequency half of the complexity hotspots (Roadmap
// 0551, #2957): how often each file was committed in a window, one `git log`
// for the whole tree. The Complexity window multiplies it with a file's
// complexity mass; `ike complexity --churn` prints it.

// Churn counts the commits per path that touched it since since, by
// `git log --format= --name-only --relative --since=<since> -- .` run in
// root: the keys are slash paths relative to root (the repository root's
// own paths when root is the top level), only paths below root are listed.
// Renames are not followed — `--name-only` names each path as of its
// commit, so a renamed file's history splits between its old and new name;
// `--follow` works per path only and would cost one git call per file.
// Merge commits list no files and count for nothing. A zero since counts
// the whole history. Running inside a tea.Cmd is the caller's job; the
// call carries the package Timeout.
func Churn(root string, since time.Time) (map[string]int, error) {
	args := []string{"log", "-z", "--format=", "--name-only", "--relative"}
	if !since.IsZero() {
		args = append(args, "--since="+since.Format(time.RFC3339))
	}
	out, err := RunGit(root, append(args, "--", ".")...)
	if err != nil {
		return nil, err
	}
	return parseChurn(out), nil
}

// parseChurn counts the NUL-separated paths of a `git log -z --format=
// --name-only` run; the empty format line of each commit leaves stray
// newlines between the records.
func parseChurn(out []byte) map[string]int {
	counts := map[string]int{}
	for _, p := range strings.Split(string(out), "\x00") {
		p = strings.Trim(p, "\n")
		if p != "" {
			counts[p]++
		}
	}
	return counts
}
