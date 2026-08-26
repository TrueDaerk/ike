// Package clipboard provides a system-clipboard implementation for the
// editor's `"+`/`"*` registers, backed by the platform's clipboard utility:
// pbcopy/pbpaste on macOS, wl-copy/xclip/xsel on Linux/BSD, clip/PowerShell on
// Windows. System returns nil when no utility is available, keeping the
// editor's built-in no-op clipboard in place.
package clipboard

import (
	"context"
	"encoding/hex"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// cmdTimeout bounds every clipboard subprocess (#2163). These run on the
// bubbletea update loop (a yank with clipboard sync, a `"+p`), and the
// fallbacks can block arbitrarily long — osascript in particular hangs when
// the Apple Events subsystem is contended or a TCC consent prompt appears.
// Without a deadline that wedges the whole IDE; with one, the worst case is
// a bounded stall and a failed clipboard op. A var so tests can shrink it.
var cmdTimeout = 3 * time.Second

// command builds a deadline-bounded clipboard subprocess; the caller must
// invoke cancel once the command finished.
func command(name string, args ...string) (*exec.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	return exec.CommandContext(ctx, name, args...), cancel
}

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
	cmd, cancel := command(c.t.copyCmd[0], c.t.copyCmd[1:]...)
	defer cancel()
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// Read returns the system clipboard's contents.
func (c *Clipboard) Read() (string, error) {
	cmd, cancel := command(c.t.pasteCmd[0], c.t.pasteCmd[1:]...)
	defer cancel()
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	// PowerShell's Get-Clipboard appends a trailing CRLF; drop CRs uniformly.
	text := strings.ReplaceAll(string(out), "\r", "")
	if text == "" {
		if url, ok := readURLFlavor(); ok {
			return url, nil
		}
	}
	return text, nil
}

// readURLFlavor recovers the pasteboard's raw `public.url` bytes on macOS.
// A URL copied as a *link* (browser context menus, drag sources) can land on
// the pasteboard as a bare URL flavor with no plain-text counterpart —
// pbpaste prints nothing for those. Reading the flavor's raw bytes keeps the
// URL byte-verbatim; asking the system to *convert* it to text instead would
// re-serialize it through Foundation's URL type, whose lenient parser
// percent-encodes the whole string again when it contains characters like
// `[` (#1601: `[` → `%5B` and, worse, `%22` → `%2522`). Overridable seam for
// tests.
var readURLFlavor = func() (string, bool) {
	if runtime.GOOS != "darwin" {
		return "", false
	}
	// osascript renders the flavor as raw AppleScript data: «data url 68…».
	cmd, cancel := command("osascript", "-e", "the clipboard as «class url »")
	defer cancel()
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return parseURLFlavorData(string(out))
}

// parseURLFlavorData decodes osascript's raw-data rendering of the
// pasteboard's URL flavor, `«data url <hex bytes>»`, into the URL string.
func parseURLFlavorData(out string) (string, bool) {
	s := strings.TrimSpace(out)
	const prefix, suffix = "«data url ", "»"
	if !strings.HasPrefix(s, prefix) || !strings.HasSuffix(s, suffix) {
		return "", false
	}
	raw, err := hex.DecodeString(s[len(prefix) : len(s)-len(suffix)])
	if err != nil || len(raw) == 0 {
		return "", false
	}
	return string(raw), true
}
