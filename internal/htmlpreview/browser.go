package htmlpreview

// browser.go is the HTML preview's browser screenshot mode (0530/8, #2746).
// The text renderer is a reading view; for CSS/JS-accurate output the pane
// can instead show a screenshot a headless Chrome/Chromium/Edge took of the
// page — the mermaid PNG renderer's pattern (internal/preview/diagrams.go,
// #2421): an external binary from the settings, exec.LookPath, off the
// update loop, cached, with a fallback.
//
//   - Mode. b in the pane (and html.preview.browser) toggles between text
//     and browser mode; the layout state remembers it per pane. Text mode is
//     always the default, and every failure lands back in it with a notice.
//   - Render. The current document is written to a fresh directory under the
//     scratch area's hidden .html-preview/ folder, next to a throwaway
//     --user-data-dir, and the browser screenshots it at the pane's pixel
//     width and a tall window (shotHeightPx). The directory is removed after
//     every render, successful or not. A <base href> pointing at the
//     document's own directory keeps its relative stylesheets, scripts and
//     images resolving from the temp copy.
//   - Display. The PNG is decoded on the render goroutine, trimmed of the
//     page background below the content and shown through an imgview.Model —
//     zoom and pan (#2688), opened zoomed to the pane width at the top, so the
//     page is one tall image the user pans. The pane's Kitty lifecycle
//     (ImageIDs/SyncSeqs/…) switches to that placement while it shows.
//   - Cache. A screenshot is keyed by the document text, the browser binary
//     and the pixel width. Toggling back to browser mode with an unchanged
//     key reuses it; edits never re-run the browser on their own — r does,
//     unconditionally — and the status line marks a shot older than the
//     buffer as stale.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"image"
	"image/png"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/imgview"
	"ike/internal/pathcomplete"
	"ike/internal/scratch"
)

// ShotMsg is a finished browser screenshot (#2746) on its way back to the
// pane that dispatched it. Key routes it, Gen drops it when a newer
// screenshot was dispatched or the mode was left meanwhile.
type ShotMsg struct {
	Key  string
	Gen  int
	Took time.Duration

	hash string
	img  image.Image
	size int64
	err  string
}

// NoticeMsg is a one-line notice for the user from an HTML preview — the
// browser mode's fallbacks (#2746). The root model shows it as a toast.
type NoticeMsg struct {
	Key  string
	Text string
}

const (
	// shotHeightPx is the browser window's height: the screenshot covers the
	// first shotHeightPx pixels of the page, trimmed to the content below.
	shotHeightPx = 4096
	// shotTrimMarginPx is the page background kept under the last content
	// row, so the bottom of the page does not sit on the pane's edge.
	shotTrimMarginPx = 16
	// shotWaitDelay bounds how long a killed browser's pipes may keep Wait
	// from returning (a helper process inheriting stderr).
	shotWaitDelay = 2 * time.Second
	// browserSettingHint names the setting every fallback notice points at.
	browserSettingHint = "Settings → Markdown Preview → HTML preview browser (preview.html_browser)"
)

// browserCandidates are the executables auto-detection looks for on PATH,
// in order — Chrome, Chromium, Edge and Chrome's headless shell.
var browserCandidates = []string{
	"google-chrome", "google-chrome-stable", "chromium", "chromium-browser",
	"chrome", "microsoft-edge", "microsoft-edge-stable", "msedge",
	"chrome-headless-shell",
}

// browserAppBundles are the macOS install locations auto-detection also
// tries: the browsers ship as app bundles there and are not on PATH. Tests
// clear it to simulate a machine without a browser.
var browserAppBundles = func() []string {
	if runtime.GOOS != "darwin" {
		return nil
	}
	return []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
	}
}()

// FindBrowser resolves the headless browser the screenshot mode runs:
// configured (preview.html_browser, "~" expanded, a bare name looked up on
// PATH) when set — no fallback to auto-detection, so a wrong setting is
// reported instead of silently replaced — else the first of
// browserCandidates on PATH or browserAppBundles that is executable.
func FindBrowser(configured string) (string, bool) {
	if configured = strings.TrimSpace(configured); configured != "" {
		p, err := exec.LookPath(pathcomplete.Expand(configured))
		return p, err == nil
	}
	for _, c := range browserCandidates {
		if p, err := exec.LookPath(c); err == nil {
			return p, true
		}
	}
	for _, p := range browserAppBundles {
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p, true
		}
	}
	return "", false
}

// browserArgs builds the headless browser's command line: a screenshot of
// page into out at w×h pixels, in a fresh profile, with nothing that talks
// to the user's own browser setup (#2746's security flags).
func browserArgs(page, out, profile string, w, h int) []string {
	return []string{
		"--headless=new",
		"--disable-gpu",
		"--no-first-run",
		"--no-default-browser-check",
		"--user-data-dir=" + profile,
		"--hide-scrollbars",
		"--screenshot=" + out,
		"--window-size=" + strconv.Itoa(w) + "," + strconv.Itoa(h),
		fileURL(page),
	}
}

// fileURL is the file:// URL of an absolute path.
func fileURL(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // a Windows drive path: file:///C:/…
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// shotWidthPx converts the pane's cell width into the browser window's pixel
// width — the diagram renderer's cell aspect, floored at a width pages lay
// out sensibly in and capped at a desktop screen.
func shotWidthPx(cols int) int { return max(800, min(1920, cols*16)) }

// shotHash keys a screenshot: the document text, the browser that renders
// it and the window width it is laid out at.
func shotHash(src, bin string, px int) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%d\x00%s", bin, px, src)
	return hex.EncodeToString(h.Sum(nil))[:16]
}

var (
	baseTagRe = regexp.MustCompile(`(?i)<base[\s>]`)
	headTagRe = regexp.MustCompile(`(?i)<head(\s[^>]*)?>`)
	htmlTagRe = regexp.MustCompile(`(?i)<html(\s[^>]*)?>`)
	doctypeRe = regexp.MustCompile(`(?i)^\s*<!doctype[^>]*>`)
)

// withBase returns src with a <base href> naming dir, so the temp copy the
// browser loads resolves relative URLs against the original document's
// directory. It goes right after <head> (else <html>, else the doctype, else
// first); a document with its own <base> is left alone.
func withBase(src, dir string) string {
	if dir == "" || baseTagRe.MatchString(src) {
		return src
	}
	tag := `<base href="` + html.EscapeString(fileURL(dir)+"/") + `">`
	for _, re := range []*regexp.Regexp{headTagRe, htmlTagRe, doctypeRe} {
		if loc := re.FindStringIndex(src); loc != nil {
			return src[:loc[1]] + tag + src[loc[1]:]
		}
	}
	return tag + src
}

// shotBaseDir is where screenshot renders make their temp directories: a
// hidden folder of the scratch area (the scratch listing skips directories),
// or the OS temp dir when the scratch area cannot be resolved.
func shotBaseDir() string {
	if d, err := scratch.Dir(); err == nil {
		return filepath.Join(d, ".html-preview")
	}
	return filepath.Join(os.TempDir(), "ike-html-preview")
}

// runBrowserShot renders src with the browser and returns the decoded,
// trimmed screenshot and the PNG's byte size. Everything it writes — the
// page copy, the profile, the PNG — lives in one temp directory removed
// before it returns. ctx cancels it (a newer render, the mode left, the pane
// closed); timeout bounds the browser itself.
func runBrowserShot(ctx context.Context, bin, src, docPath string, wpx int, timeout time.Duration) (image.Image, int64, error) {
	base := shotBaseDir()
	if err := os.MkdirAll(base, 0o700); err != nil {
		return nil, 0, err
	}
	dir, err := os.MkdirTemp(base, "shot-")
	if err != nil {
		return nil, 0, err
	}
	defer os.RemoveAll(dir)
	page := filepath.Join(dir, "page.html")
	if err := os.WriteFile(page, []byte(withBase(src, docDir(docPath))), 0o600); err != nil {
		return nil, 0, err
	}
	profile := filepath.Join(dir, "profile")
	if err := os.Mkdir(profile, 0o700); err != nil {
		return nil, 0, err
	}
	out := filepath.Join(dir, "shot.png")

	cx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cx, bin, browserArgs(page, out, profile, wpx, shotHeightPx)...)
	cmd.Dir = dir
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	cmd.WaitDelay = shotWaitDelay
	ownProcessGroup(cmd)
	err = cmd.Run()
	// The browser's helpers (renderer, GPU, crash handler) must not outlive
	// the render and keep writing into the directory being removed.
	reapProcessGroup(cmd)
	switch {
	case ctx.Err() != nil:
		return nil, 0, ctx.Err()
	case errors.Is(cx.Err(), context.DeadlineExceeded):
		return nil, 0, fmt.Errorf("timed out after %s", timeout)
	case err != nil:
		return nil, 0, errors.New(browserError(errBuf.String(), err))
	}
	data, err := os.ReadFile(out)
	if err != nil || len(data) == 0 {
		return nil, 0, errors.New("the browser wrote no screenshot")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, fmt.Errorf("screenshot could not be decoded: %w", err)
	}
	return trimBottom(img), int64(len(data)), nil
}

// docDir is the directory relative URLs of the document resolve against —
// for a gz viewer buffer ("dir/page.html.gz!page.html") the archive's.
func docDir(docPath string) string {
	if docPath == "" {
		return ""
	}
	if i := strings.LastIndex(docPath, "!"); i > 0 {
		docPath = docPath[:i]
	}
	if abs, err := filepath.Abs(docPath); err == nil {
		docPath = abs
	}
	return filepath.Dir(docPath)
}

// browserError reduces a failed browser's stderr to one line: the last
// non-empty one, which is where Chrome says why it quit (its earlier lines
// are routine log noise), or the exec error when it said nothing.
func browserError(stderr string, err error) string {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return err.Error()
}

// trimBottom cuts the rows of page background below the content off a
// screenshot: the browser window is tall, most pages are not. The colour of
// the bottom-left pixel is the background; a margin of it stays.
func trimBottom(img image.Image) image.Image {
	b := img.Bounds()
	if b.Dy() < 2 {
		return img
	}
	bg := pixelAt(img, b.Min.X, b.Max.Y-1)
	last := b.Min.Y
	for y := b.Max.Y - 1; y > b.Min.Y; y-- {
		if !rowIs(img, y, bg) {
			last = y
			break
		}
	}
	end := min(b.Max.Y, max(last+1+shotTrimMarginPx, b.Min.Y+64))
	if end >= b.Max.Y {
		return img
	}
	si, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return img
	}
	return si.SubImage(image.Rect(b.Min.X, b.Min.Y, b.Max.X, end))
}

// pixelAt returns a pixel as 16-bit RGBA, comparable across image types.
func pixelAt(img image.Image, x, y int) [4]uint32 {
	r, g, bl, a := img.At(x, y).RGBA()
	return [4]uint32{r, g, bl, a}
}

// rowIs reports whether every pixel of row y has colour c. The PNG decoder's
// concrete types are compared on their raw bytes — a 4096-row screenshot is
// millions of pixels.
func rowIs(img image.Image, y int, c [4]uint32) bool {
	b := img.Bounds()
	switch im := img.(type) {
	case *image.RGBA:
		row := im.Pix[im.PixOffset(b.Min.X, y) : im.PixOffset(b.Max.X-1, y)+4]
		return uniform(row, 4) && pixelAt(img, b.Min.X, y) == c
	case *image.NRGBA:
		row := im.Pix[im.PixOffset(b.Min.X, y) : im.PixOffset(b.Max.X-1, y)+4]
		return uniform(row, 4) && pixelAt(img, b.Min.X, y) == c
	}
	for x := b.Min.X; x < b.Max.X; x++ {
		if pixelAt(img, x, y) != c {
			return false
		}
	}
	return true
}

// uniform reports whether every n-byte pixel of row equals the first.
func uniform(row []byte, n int) bool {
	for i := n; i+n <= len(row); i += n {
		if !bytes.Equal(row[i:i+n], row[:n]) {
			return false
		}
	}
	return true
}

// browserState is one pane's browser mode: the setting values, the mode, the
// screenshot shown and the one in flight.
type browserState struct {
	on      bool          // browser mode (vs text mode)
	flipped bool          // the mode changed since TakeModeChange last looked
	bin     string        // preview.html_browser; "" auto-detects
	timeout time.Duration // preview.html_browser_timeout_s

	owed    bool // a screenshot is owed: mode switched on (restore) before the pane had a width
	running bool // a screenshot is in flight
	gen     int  // generation of the newest dispatched screenshot
	cancel  context.CancelFunc
	runSeq  int // source seq the in-flight screenshot was taken of

	shot *imgview.Model // the screenshot shown; nil before the first lands
	hash string         // shot's cache key
	seq  int            // source seq shot was taken of
}

// SetBrowser applies preview.html_browser and
// preview.html_browser_timeout_s; timeoutS <= 0 keeps the default.
func (m *Model) SetBrowser(bin string, timeoutS int) {
	if timeoutS <= 0 {
		timeoutS = config.DefaultHTMLBrowserTimeoutS
	}
	m.br.bin = bin
	m.br.timeout = time.Duration(timeoutS) * time.Second
}

// BrowserSetting returns the applied preview.html_browser and timeout.
func (m Model) BrowserSetting() (string, time.Duration) { return m.br.bin, m.br.timeout }

// BrowserMode reports whether the pane is in browser mode — what the layout
// state persists.
func (m Model) BrowserMode() bool { return m.br.on }

// browserShown reports whether the pane draws the screenshot: browser mode
// with a screenshot landed. Browser mode waiting for its first one still
// draws the text rendering.
func (m Model) browserShown() bool { return m.br.on && m.br.shot != nil }

// Shot returns the screenshot model shown in browser mode, nil otherwise.
func (m Model) Shot() *imgview.Model {
	if !m.browserShown() {
		return nil
	}
	return m.br.shot
}

// ShotRunning reports whether a screenshot is in flight.
func (m Model) ShotRunning() bool { return m.br.running }

// BrowserStatus is the status-line segment of browser mode: "" in text mode,
// else the mode plus what the screenshot is doing — being taken, older than
// the buffer (r re-renders), or its zoom level.
func (m Model) BrowserStatus() string {
	if !m.br.on {
		return ""
	}
	s := "browser"
	switch {
	case m.br.running:
		s += " │ screenshot…"
	case m.br.shot != nil && m.br.seq != m.seq:
		s += " │ stale — r re-renders"
	}
	if m.br.shot != nil {
		s += " │ " + m.br.shot.ZoomLabel()
	}
	return s
}

// TakeModeChange reports whether the mode flipped since the last call — by
// b, the command, a restore or a fallback — and resets the flag: the root
// model persists the layout then, so the mode survives a crash too.
func (m *Model) TakeModeChange() bool {
	f := m.br.flipped
	m.br.flipped = false
	return f
}

// SetBrowserMode switches browser mode on or off without the toggle's
// browser lookup — the layout restore. Switching on owes a screenshot the
// next RenderCmd dispatches (and whose lookup may still fall back).
func (m *Model) SetBrowserMode(on bool) {
	if on == m.br.on {
		return
	}
	m.setBrowser(on)
	m.br.owed = on
}

// ToggleBrowser is b in the pane and html.preview.browser: text mode →
// browser mode, dispatching the screenshot (or reusing a cached one), and
// back. Without a browser the pane stays in text mode and the returned Cmd
// carries the notice naming the setting.
func (m *Model) ToggleBrowser() tea.Cmd {
	if m.br.on {
		m.setBrowser(false)
		return nil
	}
	if _, ok := FindBrowser(m.br.bin); !ok {
		return m.notice(noBrowserNotice(m.br.bin))
	}
	m.setBrowser(true)
	m.br.owed = true
	return m.shotCmd(false)
}

// Rerender is r in browser mode: a new screenshot of the current buffer,
// whatever the cache holds.
func (m *Model) Rerender() tea.Cmd {
	if !m.br.on {
		return nil
	}
	m.br.owed = true
	return m.shotCmd(true)
}

// noBrowserNotice is the fallback notice when no browser resolves.
func noBrowserNotice(configured string) string {
	if configured = strings.TrimSpace(configured); configured != "" {
		return "HTML preview: browser " + strconv.Quote(configured) + " not found — check " + browserSettingHint + "; staying in text mode"
	}
	return "HTML preview: no headless browser (Chrome, Chromium, Edge) found — set " + browserSettingHint + "; staying in text mode"
}

// notice returns a Cmd delivering a NoticeMsg from this pane.
func (m *Model) notice(text string) tea.Cmd {
	key := m.key
	return func() tea.Msg { return NoticeMsg{Key: key, Text: text} }
}

// setBrowser flips the mode. Leaving it cancels a screenshot in flight; the
// shown one stays cached for the next toggle.
func (m *Model) setBrowser(on bool) {
	before := m.browserShown()
	if on != m.br.on {
		m.br.flipped = true
	}
	m.br.on = on
	if !on {
		m.cancelShot()
		m.br.owed = false
	}
	m.shownChanged(before)
}

// shownChanged hands the Kitty lifecycle over when the pane starts or stops
// drawing the screenshot: the placements it no longer lists are deleted by
// the app's reconcile pass, so their sent state is forgotten here and they
// transmit again when they come back.
func (m *Model) shownChanged(before bool) {
	if before == m.browserShown() {
		return
	}
	m.resetTextImages()
	if m.br.shot != nil {
		m.br.shot.Reset()
	}
}

// cancelShot abandons the screenshot in flight, if any.
func (m *Model) cancelShot() {
	if m.br.cancel != nil {
		m.br.cancel()
		m.br.cancel = nil
	}
	m.br.running = false
}

// shotCmd dispatches the owed screenshot as a Cmd, or nil when none is owed,
// the pane has no width yet, or the cache already holds this document at
// this width (force, r, skips that check). A browser that no longer
// resolves falls back to text mode with the notice.
func (m *Model) shotCmd(force bool) tea.Cmd {
	if !m.br.on || !m.br.owed || m.w <= 0 {
		return nil
	}
	m.br.owed = false
	bin, ok := FindBrowser(m.br.bin)
	if !ok {
		m.setBrowser(false)
		return m.notice(noBrowserNotice(m.br.bin))
	}
	px := shotWidthPx(m.w)
	hash := shotHash(m.src, bin, px)
	if !force && m.br.shot != nil && m.br.hash == hash {
		m.br.seq = m.seq // the same text: the cached shot is current
		return nil
	}
	m.cancelShot()
	m.br.gen = int(renderGen.Add(1))
	cx, cancel := context.WithCancel(context.Background())
	m.br.cancel, m.br.running, m.br.runSeq = cancel, true, m.seq
	key, gen, src, path := m.key, m.br.gen, m.src, m.path
	timeout := m.br.timeout
	if timeout <= 0 {
		timeout = time.Duration(config.DefaultHTMLBrowserTimeoutS) * time.Second
	}
	return func() tea.Msg {
		start := time.Now()
		img, size, err := runBrowserShot(cx, bin, src, path, px, timeout)
		if cx.Err() != nil {
			return nil // cancelled: nobody is waiting
		}
		msg := ShotMsg{Key: key, Gen: gen, Took: time.Since(start), hash: hash, img: img, size: size}
		if err != nil {
			msg.err = err.Error()
		}
		return msg
	}
}

// applyShot adopts a finished screenshot, or falls back to text mode with a
// notice when the browser failed. A re-render keeps the zoom and pan of the
// shot it replaces, so r after an edit stays where the user was reading.
func (m *Model) applyShot(msg ShotMsg) tea.Cmd {
	m.br.cancel, m.br.running = nil, false
	if msg.err != "" {
		m.setBrowser(false)
		return m.notice("HTML preview: browser render failed — " + msg.err + "; back in text mode")
	}
	before := m.browserShown()
	sh := imgview.NewFromImage(m.key, filepath.Base(m.path)+" (browser)", msg.img, "png", msg.size, m.palette())
	sh.SetSize(m.w, m.h)
	sh.SetGraphics(m.gfx)
	sh.SetFocused(m.focused)
	sh.SetPalette(m.palette())
	if old := m.br.shot; old != nil {
		sh.SetViewState(old.ViewState())
	} else {
		sh.ZoomWidth()
	}
	m.br.shot, m.br.hash, m.br.seq = &sh, msg.hash, m.br.runSeq
	m.shownChanged(before)
	return nil
}

// closeBrowser releases browser mode's background work and pixels — the
// pane closed.
func (m *Model) closeBrowser() {
	m.cancelShot()
	m.br.owed = false
	m.br.shot = nil
}
