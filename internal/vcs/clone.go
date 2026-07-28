package vcs

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// clone.go implements "Clone Repository" (#1349): a repository is fetched into
// a fresh directory the caller names, and the app opens the result. Unlike the
// status/stage operations this one talks to a remote, so it gets its own
// generous timeout instead of the 5s gitTimeout, and it runs with terminal
// prompting disabled — a credential prompt would block a subprocess nobody can
// answer, so a private repository fails fast with git's own message.

// cloneTimeout bounds one clone. Large repositories over a slow link are the
// worst case; the dialog stays responsive because the clone runs in a tea.Cmd.
const cloneTimeout = 30 * time.Minute

// CloneDoneMsg reports a finished clone attempt. Dest is the absolute target
// directory; Err carries the decisive git message on failure, in which case
// nothing usable was left behind (a partial clone is removed).
type CloneDoneMsg struct {
	URL  string
	Dest string
	Err  error
}

// CloneCmd clones url into dest (which must not exist yet) and resolves to a
// CloneDoneMsg. A failed clone removes the directory git created, so a retry
// into the same name is not blocked by a half-written checkout.
func CloneCmd(url, dest string) tea.Cmd {
	return func() tea.Msg {
		err := clone(url, dest)
		if err != nil {
			// Only a directory git itself created is removed — the caller
			// validated that dest did not exist beforehand.
			_ = os.RemoveAll(dest)
		}
		return CloneDoneMsg{URL: url, Dest: dest, Err: err}
	}
}

// clone runs the git subprocess. Split out so tests can drive it directly.
func clone(url, dest string) error {
	url = strings.TrimSpace(url)
	if url == "" {
		return fmt.Errorf("repository URL is empty — enter a URL to clone")
	}
	ctx, cancel := context.WithTimeout(context.Background(), cloneTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "clone", "--", url, dest)
	// Never let git open an interactive prompt: no terminal is attached.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "SSH_ASKPASS=")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("clone timed out after %s", cloneTimeout)
		}
		return gitError(err, stderr.String())
	}
	return nil
}

// CloneName derives the default directory name for a clone of url: the last
// path segment without a trailing ".git", for both ssh ("git@host:org/repo.git")
// and URL forms ("https://host/org/repo.git", "file:///tmp/repo"). It returns
// "" when no usable name can be derived, leaving the caller to ask for one.
func CloneName(url string) string {
	s := strings.TrimSpace(url)
	if s == "" {
		return ""
	}
	// Strip a trailing separator: "…/repo/" names the same repository.
	s = strings.TrimRight(s, "/")
	// scp-style "git@host:org/repo": everything after the colon is the path.
	if i := strings.LastIndex(s, ":"); i >= 0 && !strings.Contains(s[i+1:], "//") {
		s = s[i+1:]
	}
	// A query or fragment is not part of the name.
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimRight(s, "/")
	base := path.Base(strings.ReplaceAll(s, "\\", "/"))
	base = strings.TrimSuffix(base, ".git")
	if base == "." || base == "/" || base == "" {
		return ""
	}
	return base
}
