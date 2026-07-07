// Package clipboard provides a system-clipboard implementation for the
// editor's `"+`/`"*` registers, backed by the platform's clipboard utility:
// pbcopy/pbpaste on macOS, wl-copy/xclip/xsel on Linux/BSD, clip/PowerShell on
// Windows. System returns nil when no utility is available, keeping the
// editor's built-in no-op clipboard in place.
package clipboard

import (
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// tool is one copy/paste command pair candidate.
type tool struct {
	copyCmd  []string
	pasteCmd []string
}

// candidates lists the clipboard utilities to probe for the current platform,
// in preference order.
func candidates() []tool {
	switch runtime.GOOS {
	case "darwin":
		return []tool{{[]string{"pbcopy"}, []string{"pbpaste"}}}
	case "windows":
		return []tool{{
			[]string{"clip"},
			[]string{"powershell", "-NoProfile", "-Command", "Get-Clipboard"},
		}}
	default:
		return []tool{
			{[]string{"wl-copy"}, []string{"wl-paste", "--no-newline"}},
			{[]string{"xclip", "-selection", "clipboard"}, []string{"xclip", "-selection", "clipboard", "-o"}},
			{[]string{"xsel", "--clipboard", "--input"}, []string{"xsel", "--clipboard", "--output"}},
		}
	}
}

// Clipboard shells out to the resolved platform utility. It satisfies the
// editor's register.Clipboard interface.
type Clipboard struct{ t tool }

var (
	once   sync.Once
	system *Clipboard
)

// System returns the platform clipboard, or nil when no known utility is on
// PATH. The probe runs once per process.
func System() *Clipboard {
	once.Do(func() { system = probe() })
	return system
}

// probe resolves the first candidate whose copy utility is on PATH.
func probe() *Clipboard {
	for _, t := range candidates() {
		if _, err := exec.LookPath(t.copyCmd[0]); err == nil {
			return &Clipboard{t: t}
		}
	}
	return nil
}

// Write puts text on the system clipboard.
func (c *Clipboard) Write(text string) error {
	cmd := exec.Command(c.t.copyCmd[0], c.t.copyCmd[1:]...)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// Read returns the system clipboard's contents.
func (c *Clipboard) Read() (string, error) {
	out, err := exec.Command(c.t.pasteCmd[0], c.t.pasteCmd[1:]...).Output()
	if err != nil {
		return "", err
	}
	// PowerShell's Get-Clipboard appends a trailing CRLF; drop CRs uniformly.
	return strings.ReplaceAll(string(out), "\r", ""), nil
}
