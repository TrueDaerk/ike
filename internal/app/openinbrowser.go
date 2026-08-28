package app

// openinbrowser.go implements the JetBrains-style "Open in Browser" action
// (#1429): open the focused file — explorer selection when the tree is
// focused, else the focused editor's file — in the platform default browser.
// Only browser-viewable types are opened; anything else gets a toast instead
// of a silent no-op.
//
// A compressed HTML file (report.html.gz) is browser-viewable too (#2298):
// its inner name decides that, but a browser cannot render gzip bytes
// directly, so it is unpacked into ike's own scratch directory first and the
// plain file opened from there. The scratch directory is named for the
// source path, so reopening the same archive overwrites its unpacked copy
// instead of piling up a new one, and it is swept clean on a fresh start and
// on a clean exit (New, quit in app.go) so nothing outlives the session.

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/gzfile"
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

// openInBrowserTempDir is the ike-owned scratch directory unpacked gzip
// files are written to before being handed to the platform opener.
// Overridden in tests so they never touch the real OS temp directory.
var openInBrowserTempDir = filepath.Join(os.TempDir(), "ike-open-in-browser")

// cleanOpenInBrowserTempDir removes whatever a previous session — cleanly
// exited or not — left in the scratch directory. quit() already empties it
// on a clean exit; New() calls this too, so a kill or a crash never leaves
// unpacked copies behind for good.
func cleanOpenInBrowserTempDir() {
	os.RemoveAll(openInBrowserTempDir)
}

// openInBrowser resolves the subject like copyPath does (#1173) and hands it
// to the platform opener when the browser can render it — decompressing a
// plain gzip file first when its inner content is what is browser-viewable
// (#2298).
func (m *Model) openInBrowser() tea.Cmd {
	path, ok := m.refactorTarget()
	if !ok || path == "" {
		m.host.Notify(host.Info, "no file to open in the browser")
		return nil
	}
	if archivePath, isGzip := gzipArchiveOf(path); isGzip {
		return m.openGzipInBrowser(archivePath)
	}
	if browserViewable(path) {
		return m.browserOpenNotify(path, path)
	}
	m.host.Notify(host.Info, "not a browser-viewable file: "+filepath.Base(path))
	return nil
}

// gzipArchiveOf resolves path back to the on-disk gzip archive it names —
// either directly, or as the virtual path of its own read-only preview
// buffer (report.html.gz!report.html, the "!"-joined form ShowReadOnly opens
// a decompressed .gz under, #1763) — when that archive is a plain gzip
// gzfile claims. Anything else, including an entry inside a non-gzip
// archive, is left alone: false so the caller falls through to the ordinary
// extension check.
func gzipArchiveOf(path string) (string, bool) {
	archivePath := path
	if i := strings.Index(path, entrySep); i >= 0 {
		archivePath = path[:i]
	}
	if gzfile.IsPlain(archivePath, readHead(archivePath)) {
		return archivePath, true
	}
	return "", false
}

// openGzipInBrowser decompresses a plain gzip file into ike's own scratch
// directory and opens the result, when its inner name is itself
// browser-viewable — report.html.gz opens report.html, app.log.gz stays
// declined the same as a bare .log would. gzfile.IsPlain has already turned
// away a compressed tar (or anything merely named like one) before this
// runs, so only single-file archives ever reach it.
func (m *Model) openGzipInBrowser(path string) tea.Cmd {
	c, err := gzfile.Read(path, m.largeFileLimit())
	if err != nil {
		m.host.Notify(host.Error, "cannot read "+baseName(path)+": "+err.Error())
		return nil
	}
	if !browserViewable(c.Name) {
		m.host.Notify(host.Info, "not a browser-viewable file: "+filepath.Base(path))
		return nil
	}
	if c.Truncated {
		m.host.Notify(host.Error, fmt.Sprintf(
			"cannot open %s in the browser: decompressed content exceeds the %s limit",
			baseName(path), humanBytes(m.largeFileLimit())))
		return nil
	}
	out, err := writeOpenInBrowserTemp(path, c.Name, c.Data)
	if err != nil {
		m.host.Notify(host.Error, "open in browser failed: "+err.Error())
		return nil
	}
	return m.browserOpenNotify(out, path)
}

// writeOpenInBrowserTemp unpacks data under openInBrowserTempDir, in a
// per-source-file subdirectory keyed by the archive's own absolute path so a
// reopen overwrites the same file instead of accumulating a new one, and
// name collisions between two archives sharing an inner name never occur.
func writeOpenInBrowserTemp(archivePath, inner string, data []byte) (string, error) {
	abs, err := filepath.Abs(archivePath)
	if err != nil {
		abs = archivePath
	}
	sum := sha1.Sum([]byte(abs))
	dir := filepath.Join(openInBrowserTempDir, hex.EncodeToString(sum[:]))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	out := filepath.Join(dir, filepath.Base(inner))
	if err := os.WriteFile(out, data, 0o600); err != nil {
		return "", err
	}
	return out, nil
}

// browserOpenNotify hands displayPath to the platform opener and reports the
// outcome against label — the source file's own name, so the toast reads the
// same for a plain HTML file and its unpacked gzip counterpart.
func (m *Model) browserOpenNotify(displayPath, label string) tea.Cmd {
	if err := browserOpen(displayPath); err != nil {
		m.host.Notify(host.Error, "open in browser failed: "+err.Error())
		return nil
	}
	m.host.Notify(host.Info, "opened "+filepath.Base(label)+" in the browser")
	return nil
}
