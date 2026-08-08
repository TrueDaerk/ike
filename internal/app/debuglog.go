package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/debugpanel"
)

// debuglog.go is the slow-operation diagnostic (#125), motivated by the #123
// freeze: anything that stalls the Update loop is invisible until the UI
// hangs. Every Update pass over slowUpdateThreshold leaves a line in the
// per-project state log naming the message type and duration, so a stall is
// attributable after the fact. Logging is best-effort — a failed write never
// affects the editor.

// slowUpdateThreshold flags Update passes that noticeably stall the UI.
const slowUpdateThreshold = 200 * time.Millisecond

// debugLogFile mirrors layoutFile's discovery: IKE_CONFIG_DIR overrides the
// per-project ".ike" state directory.
func debugLogFile() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "debug.log")
	}
	return filepath.Join(".ike", "debug.log")
}

// logSlowUpdate appends one entry for a slow Update pass.
func logSlowUpdate(msg tea.Msg, took time.Duration) {
	logDiagnostic(fmt.Sprintf("slow update: %T took %s", msg, took.Round(time.Millisecond)))
}

// logMouseNavButton records that the terminal delivered one of the dedicated
// mouse navigation buttons (#816). The buttons degrade silently where they are
// not reported, which makes "my terminal sends nothing" and "the binding is
// broken" look identical from the outside — this line separates the two: an
// entry means the event arrived and the keymap had its say.
func logMouseNavButton(base string) {
	logDiagnostic("mouse: navigation button delivered as " + base)
}

// logDiagnostic appends a timestamped line to the state debug log.
func logDiagnostic(text string) {
	path := debugLogFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), text)
}

// debugSessionLogFile is the per-project transcript of a debug session's
// output, a sibling of debug.log (#624). IKE_CONFIG_DIR overrides the ".ike"
// state directory, matching debugLogFile.
func debugSessionLogFile() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "debug-session.log")
	}
	return filepath.Join(".ike", "debug-session.log")
}

// logDebugOutput appends a debuggee output chunk to the session log. ANSI
// escape sequences are stripped (#637) so the transcript stays readable in
// any pager; other than that the text is kept as printed (\r/\t included —
// only the panel normalizes those for rendering). stderr chunks are prefixed
// so the two streams stay distinguishable in the file. Best-effort.
func logDebugOutput(stderr bool, text string) {
	text = debugpanel.StripANSI(text)
	if stderr {
		text = prefixLines(text, "[stderr] ")
	}
	appendDebugSessionLog(text)
}

// logDebugOutputAt is logDebugOutput for a parked workspace's debuggee
// (#1523): the transcript lands under the owning project's root, not the
// active one's. With IKE_CONFIG_DIR set both resolve to the same redirected
// file, matching debugSessionLogFile.
func logDebugOutputAt(root string, stderr bool, text string) {
	if os.Getenv("IKE_CONFIG_DIR") != "" || root == "" {
		logDebugOutput(stderr, text)
		return
	}
	text = debugpanel.StripANSI(text)
	if stderr {
		text = prefixLines(text, "[stderr] ")
	}
	appendDebugSessionLogTo(filepath.Join(root, ".ike", "debug-session.log"), text)
}

// logDebugSessionStart writes a delimiter line so consecutive sessions stay
// distinguishable in the transcript (#637).
func logDebugSessionStart(name string) {
	appendDebugSessionLog("──── debug session: " + name + " · " + time.Now().Format(time.RFC3339) + " ────\n")
}

// appendDebugSessionLog appends text to debug-session.log, best-effort.
func appendDebugSessionLog(text string) {
	appendDebugSessionLogTo(debugSessionLogFile(), text)
}

// appendDebugSessionLogTo appends text to the given transcript file,
// best-effort.
func appendDebugSessionLogTo(path, text string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(text)
}

// prefixLines prefixes every non-empty line of s, preserving the trailing
// newline structure so streamed partial writes concatenate correctly.
func prefixLines(s, prefix string) string {
	if s == "" {
		return s
	}
	trailing := ""
	body := s
	if strings.HasSuffix(s, "\n") {
		trailing = "\n"
		body = s[:len(s)-1]
	}
	parts := strings.Split(body, "\n")
	for i, p := range parts {
		parts[i] = prefix + p
	}
	return strings.Join(parts, "\n") + trailing
}
