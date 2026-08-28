package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/explorer"
	"ike/internal/host"
)

// popuptermcwd_test.go covers terminal.popup_cwd (#2316): where a fresh popup
// shell starts — the project root by default, the focused file's directory
// under "file", degrading to the root when no file is open.

func TestPopupCwdDefaultIsProjectRoot(t *testing.T) {
	m := sized(t, 100, 40)
	if dir := m.popupShellDir(); dir != "." {
		t.Fatalf("default popup shell dir = %q, want %q (project root)", dir, ".")
	}
}

func TestPopupCwdFileModeUsesFocusedFileDir(t *testing.T) {
	m := sized(t, 100, 40)
	sub := filepath.Join(t.TempDir(), "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "main.go")
	if err := os.WriteFile(path, []byte("package pkg\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)
	m.host.SetConfig(host.MapConfig{"terminal.popup_cwd": "file"})
	if dir := m.popupShellDir(); dir != sub {
		t.Fatalf("file-mode popup shell dir = %q, want %q", dir, sub)
	}
	// The spawned session actually starts there.
	m = openTestPopupWith(t, m)
	got := m.popup.inst.ActiveTerminal().Dir()
	want, _ := filepath.Abs(sub)
	if got != want {
		t.Fatalf("popup session dir = %q, want %q", got, want)
	}
}

func TestPopupCwdFileModeWithoutFileFallsBackToRoot(t *testing.T) {
	m := sized(t, 100, 40)
	m.host.SetConfig(host.MapConfig{"terminal.popup_cwd": "file"})
	if dir := m.popupShellDir(); dir != "." {
		t.Fatalf("file-mode popup shell dir with no file = %q, want %q", dir, ".")
	}
	m = openTestPopupWith(t, m)
	got := m.popup.inst.ActiveTerminal().Dir()
	root, _ := filepath.Abs(".")
	if !strings.HasPrefix(got, root) {
		t.Fatalf("popup session dir = %q, want the project root %q", got, root)
	}
}
