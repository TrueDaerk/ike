package manager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogPathUsesConfigDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	want := filepath.Join(dir, "logs", "lsp-php.log")
	if got := LogPath("php"); got != want {
		t.Fatalf("LogPath = %q, want %q", got, want)
	}
	if got := LogPath(""); got != "" {
		t.Fatalf("empty lang must yield no path, got %q", got)
	}
}

func TestAppendLogWritesMarkerLine(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	appendLog("php", "server crashed")
	data, err := os.ReadFile(LogPath("php"))
	if err != nil {
		t.Fatalf("appendLog must create the file: %v", err)
	}
	if !strings.Contains(string(data), "--- server crashed") {
		t.Fatalf("log = %q", data)
	}
}
