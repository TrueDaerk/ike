package app

// openinbrowser.go implements the JetBrains-style "Open in Browser" action
// (#1429): open the focused file — explorer selection when the tree is
// focused, else the focused editor's file — in the platform default browser.
// Only browser-viewable types are opened; anything else gets a toast instead
// of a silent no-op.

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
)

// browserViewableExts lists the extensions a browser renders natively:
// markup, images and PDF. Markdown is deliberately absent — browsers show it
// as raw text, which is what the markdown.preview command is for.
var browserViewableExts = map[string]bool{
	".html": true, ".htm": true, ".xhtml": true,
	".svg": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".webp": true, ".bmp": true, ".ico": true, ".avif": true,
	".pdf": true,
}

// browserViewable reports whether the platform browser can render the file.
func browserViewable(path string) bool {
	return browserViewableExts[strings.ToLower(filepath.Ext(path))]
}

// browserOpen is a seam over the platform opener so tests don't launch a real
// browser: `open` on macOS, `start` on Windows, `xdg-open` elsewhere.
var browserOpen = func(path string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", path)
	case "windows":
		c = exec.Command("cmd", "/c", "start", "", path)
	default:
		c = exec.Command("xdg-open", path)
	}
	return c.Start()
}

// openInBrowser resolves the subject like copyPath does (#1173) and hands it
// to the platform opener when the browser can render it.
func (m *Model) openInBrowser() tea.Cmd {
	path, ok := m.refactorTarget()
	if !ok || path == "" {
		m.host.Notify(host.Info, "no file to open in the browser")
		return nil
	}
	if !browserViewable(path) {
		m.host.Notify(host.Info, "not a browser-viewable file: "+filepath.Base(path))
		return nil
	}
	if err := browserOpen(path); err != nil {
		m.host.Notify(host.Error, "open in browser failed: "+err.Error())
		return nil
	}
	m.host.Notify(host.Info, "opened "+filepath.Base(path)+" in the browser")
	return nil
}
