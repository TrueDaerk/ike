package manager

import (
	"os"
	"path/filepath"
	"time"
)

// logs.go names the per-language server log files (#715): the transport tees
// each server's stderr into LogPath(lang), and the manager appends its own
// lifecycle markers (crash, restart attempt, disabled) so the file tells the
// whole story. lsp.showLog opens these files.

// LogDir is the server-log directory: $IKE_CONFIG_DIR/logs when the override
// is set, ~/.ike/logs otherwise (mirroring the config discovery).
func LogDir() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "logs")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ike", "logs")
}

// LogPath is the log file for one language's server ("" without a log dir).
func LogPath(lang string) string {
	dir := LogDir()
	if dir == "" || lang == "" {
		return ""
	}
	return filepath.Join(dir, "lsp-"+lang+".log")
}

// appendLog writes one timestamped manager lifecycle line into the language's
// log file. Best-effort: any failure is silent, logging must never block the
// manager.
func appendLog(lang, line string) {
	path := LogPath(lang)
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(time.Now().Format("2006-01-02 15:04:05") + " --- " + line + "\n")
}
