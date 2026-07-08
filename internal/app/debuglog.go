package app

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
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
