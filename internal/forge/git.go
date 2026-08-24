package forge

// git.go holds the package's own small git runner plus shared error helpers.
// internal/vcs keeps its runner unexported on purpose; the forge needs only a
// handful of plumbing calls (remote URL, status, fetch, checkout -b), so a
// local copy of the pattern beats widening the vcs API.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// gitQuickTimeout bounds the local plumbing calls (status, remote, checkout);
// gitFetchTimeout gives the network-bound fetch more room.
const (
	gitQuickTimeout = 5 * time.Second
	gitFetchTimeout = 30 * time.Second
)

// errTimeout is the shared subprocess-deadline error.
var errTimeout = errors.New("command timed out")

// itoa keeps the call sites short.
func itoa(n int) string { return strconv.Itoa(n) }

// runGitTimeout executes one git command in dir under the given deadline.
func runGitTimeout(dir string, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, errTimeout
		}
		return nil, cliError("git", err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// runGitQuick executes one local git plumbing call.
func runGitQuick(dir string, args ...string) ([]byte, error) {
	return runGitTimeout(dir, gitQuickTimeout, args...)
}

// cliError reduces a subprocess failure to its first stderr line, mirroring
// internal/vcs.gitError: the first line names the problem, the rest is usage.
func cliError(tool string, err error, stderr string) error {
	msg := strings.TrimSpace(stderr)
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	msg = strings.TrimPrefix(msg, "fatal: ")
	msg = strings.TrimPrefix(msg, "error: ")
	if msg == "" {
		msg = err.Error()
	}
	return fmt.Errorf("%s: %s", tool, msg)
}
