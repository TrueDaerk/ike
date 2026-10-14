package terminal

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testShell is the shell every interactive pty test drives (#3000). The
// tests type into a readline: cursor motion (ctrl+a, ESC b), history (Up for
// RerunLast), completion edits mid-line — and assume the shell answers, the
// way macOS' /bin/sh (bash in POSIX mode) does. On Debian and Ubuntu /bin/sh
// is dash, which has no line editing at all: the keys are swallowed and each
// such test waits out its deadline. So the tests drive /bin/sh where that is
// bash, and otherwise bash from PATH — through a symlink named `sh`, so bash
// starts in POSIX mode like the macOS binary: no /etc/bash.bashrc and no
// ~/.bashrc, hence the stock "sh-5.2$ " prompt instead of the distribution's
// `user@host:/cwd$`, whose length wraps the echoed lines the tests look for
// (and whose path confuses the command-line parser). With neither bash,
// requirePTY skips the pty tests with that reason. Command sessions
// (`/bin/sh -c script`) need no editing and keep /bin/sh.
var testShell = pickTestShell()

// testShellErr says why no editing shell was found; "" when testShell has one.
var testShellErr string

// testShellDir holds the `sh` symlink when bash comes from PATH; TestMain
// removes it.
var testShellDir string

func pickTestShell() string {
	out, err := exec.Command("/bin/sh", "-c", "echo $BASH_VERSION").Output()
	if err == nil && strings.TrimSpace(string(out)) != "" {
		return "/bin/sh"
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		testShellErr = "/bin/sh is not bash and no bash on PATH: the pty tests drive readline line editing"
		return "/bin/sh"
	}
	dir, err := os.MkdirTemp("", "ike-terminal-test-sh")
	if err != nil {
		testShellErr = "cannot create the bash-as-sh symlink dir: " + err.Error()
		return "/bin/sh"
	}
	link := filepath.Join(dir, "sh")
	if err := os.Symlink(bash, link); err != nil {
		os.RemoveAll(dir)
		testShellErr = "cannot symlink bash as sh: " + err.Error()
		return "/bin/sh"
	}
	testShellDir = dir
	return link
}

// TestMain gives the spawned shells an empty HOME and no $ENV (#3000), so
// even a POSIX-mode bash reads no start-up file of the developer's or the CI
// image's: the tests count prompt columns and rely on the stock prompt.
func TestMain(m *testing.M) {
	os.Unsetenv("ENV")
	if home, err := os.MkdirTemp("", "ike-terminal-test-home"); err == nil {
		os.Setenv("HOME", home)
		defer os.RemoveAll(home)
	}
	if testShellDir != "" {
		defer os.RemoveAll(testShellDir)
	}
	code := m.Run()
	os.Exit(code)
}
