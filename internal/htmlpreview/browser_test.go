package htmlpreview

// browser_test.go drives the browser screenshot mode (#2746) against a fake
// browser put on PATH — a script that records its arguments, checks the
// throwaway profile exists, keeps a copy of the page it was handed and
// "screenshots" it by copying a fixture PNG — mirroring the diagram
// renderer's tests (internal/preview/diagrams_test.go). Nothing here needs
// Chrome installed.

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/theme"
)

const browserDoc = "<!DOCTYPE html><html><head><title>T</title></head><body><p>hello</p></body></html>"

// fakeBrowserScript is the fake Chrome: arguments go to $ARGLOG, the page it
// loads to $PAGECOPY, and the fixture PNG to the --screenshot path.
const fakeBrowserScript = `
echo "--run--" >> "$ARGLOG"
for a in "$@"; do
  echo "$a" >> "$ARGLOG"
  case "$a" in
    --screenshot=*) out="${a#--screenshot=}";;
    --user-data-dir=*) prof="${a#--user-data-dir=}";;
  esac
  last="$a"
done
[ -d "$prof" ] || { echo "no profile dir" >&2; exit 3; }
cp "${last#file://}" "$PAGECOPY"
cp "$FIXTURE" "$out"
`

// browserEnv isolates a test: the scratch area under a temp IKE_CONFIG_DIR,
// no macOS app bundles, and PATH holding only bin (when a script is given)
// plus the system dirs the script needs. It returns the arg log path and the
// page copy path.
func browserEnv(t *testing.T, bin, script string) (argLog, pageCopy string) {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", cfg)
	saved := browserAppBundles
	browserAppBundles = nil
	t.Cleanup(func() { browserAppBundles = saved })
	dir := t.TempDir()
	argLog, pageCopy = filepath.Join(dir, "args.log"), filepath.Join(dir, "page.copy")
	fixture := filepath.Join(dir, "fixture.png")
	writeShotPNG(t, fixture)
	t.Setenv("ARGLOG", argLog)
	t.Setenv("PAGECOPY", pageCopy)
	t.Setenv("FIXTURE", fixture)
	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if bin != "" {
		if err := os.WriteFile(filepath.Join(binDir, bin), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir+":/usr/bin:/bin")
	return argLog, pageCopy
}

// writeShotPNG writes a 100×400 white page with a dark block over its first
// 300 rows: trimmed, it is 316 rows tall (the block plus the margin) — a
// tall page, opened zoomed to the pane width.
func writeShotPNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 100, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 100; x++ {
			c := color.NRGBA{255, 255, 255, 255}
			if y < 300 && x > 10 && x < 90 {
				c = color.NRGBA{20, 20, 20, 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// browserPreview returns a sized, rendered preview of browserDoc bound to a
// page in docDir, on a terminal with Kitty graphics.
func browserPreview(t *testing.T, docDir string) *Model {
	t.Helper()
	m := New("htmlpreview", filepath.Join(docDir, "page.html"), theme.DefaultPalette())
	m.SetSize(60, 30)
	m.SetGraphics(true)
	m.SetSourceImmediate(browserDoc)
	m.Flush()
	return &m
}

// drain runs cmd to completion the way the runtime would — batches fanned
// out, every message fed back into the model and its own Cmds run too — and
// returns the messages seen.
func drain(m *Model, cmd tea.Cmd) []tea.Msg {
	var out []tea.Msg
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		if msg == nil {
			continue
		}
		out = append(out, msg)
		queue = append(queue, m.Update(msg))
	}
	return out
}

// notices returns the texts of the NoticeMsgs among msgs.
func notices(msgs []tea.Msg) []string {
	var out []string
	for _, msg := range msgs {
		if n, ok := msg.(NoticeMsg); ok {
			out = append(out, n.Text)
		}
	}
	return out
}

// runs counts the fake browser's invocations.
func runs(t *testing.T, argLog string) int {
	t.Helper()
	data, err := os.ReadFile(argLog)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "--run--\n")
}

func TestBrowserArgs(t *testing.T) {
	args := browserArgs("/tmp/x/page.html", "/tmp/x/shot.png", "/tmp/x/profile", 1024, 4096)
	want := []string{
		"--headless=new", "--disable-gpu", "--no-first-run", "--no-default-browser-check",
		"--user-data-dir=/tmp/x/profile", "--hide-scrollbars", "--screenshot=/tmp/x/shot.png",
		"--window-size=1024,4096", "file:///tmp/x/page.html",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("args =\n%q\nwant\n%q", args, want)
	}
	if got := fileURL("/tmp/a b/p.html"); got != "file:///tmp/a%20b/p.html" {
		t.Fatalf("fileURL escapes = %q", got)
	}
	if shotWidthPx(10) != 800 || shotWidthPx(80) != 1280 || shotWidthPx(500) != 1920 {
		t.Fatalf("pixel widths = %d %d %d", shotWidthPx(10), shotWidthPx(80), shotWidthPx(500))
	}
}

func TestShotHashKey(t *testing.T) {
	base := shotHash("<p>a</p>", "/bin/chrome", 1024)
	if base != shotHash("<p>a</p>", "/bin/chrome", 1024) {
		t.Fatal("the key must be stable")
	}
	for name, h := range map[string]string{
		"source":  shotHash("<p>b</p>", "/bin/chrome", 1024),
		"browser": shotHash("<p>a</p>", "/bin/edge", 1024),
		"width":   shotHash("<p>a</p>", "/bin/chrome", 1280),
	} {
		if h == base {
			t.Errorf("a different %s must change the key", name)
		}
	}
}

func TestWithBase(t *testing.T) {
	const tag = `<base href="file:///docs/site/">`
	cases := map[string]string{
		"<html><head><title>x</title></head></html>": "<html><head>" + tag + "<title>x</title></head></html>",
		`<HTML lang="en"><body>x</body></HTML>`:      `<HTML lang="en">` + tag + "<body>x</body></HTML>",
		"<!doctype html><p>x":                        "<!doctype html>" + tag + "<p>x",
		"<p>x":                                       tag + "<p>x",
		`<head><base href="http://e/"></head>`:       `<head><base href="http://e/"></head>`,
	}
	for in, want := range cases {
		if got := withBase(in, "/docs/site"); got != want {
			t.Errorf("withBase(%q) =\n%q\nwant\n%q", in, got, want)
		}
	}
	if docDir("/a/b/page.html.gz!page.html") != "/a/b" || docDir("/a/b/page.html") != "/a/b" {
		t.Fatalf("docDir = %q / %q", docDir("/a/b/page.html.gz!page.html"), docDir("/a/b/page.html"))
	}
}

func TestTrimBottom(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 10, 1000))
	for y := 0; y < 1000; y++ {
		for x := 0; x < 10; x++ {
			img.Set(x, y, color.RGBA{200, 200, 255, 255})
		}
	}
	img.Set(5, 100, color.RGBA{0, 0, 0, 255})
	if got := trimBottom(img).Bounds().Dy(); got != 101+shotTrimMarginPx {
		t.Fatalf("trimmed height = %d, want %d", got, 101+shotTrimMarginPx)
	}
	// A blank page keeps a minimum; content reaching the bottom keeps all.
	blank := image.NewRGBA(image.Rect(0, 0, 10, 1000))
	if got := trimBottom(blank).Bounds().Dy(); got != 64 {
		t.Fatalf("blank page height = %d, want 64", got)
	}
	img.Set(5, 998, color.RGBA{0, 0, 0, 255})
	if got := trimBottom(img).Bounds().Dy(); got != 1000 {
		t.Fatalf("full page height = %d, want 1000", got)
	}
}

// TestBrowserNotFoundKeepsTextMode: with an empty PATH (and no app bundles)
// the toggle stays in text mode and says which setting to set.
func TestBrowserNotFoundKeepsTextMode(t *testing.T) {
	browserEnv(t, "", "")
	t.Setenv("PATH", "")
	if _, ok := FindBrowser(""); ok {
		t.Fatal("no browser must resolve on an empty PATH")
	}
	m := browserPreview(t, t.TempDir())
	msgs := drain(m, m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"}))
	if m.BrowserMode() {
		t.Fatal("the pane must stay in text mode")
	}
	n := notices(msgs)
	if len(n) != 1 || !strings.Contains(n[0], "preview.html_browser") || !strings.Contains(n[0], "text mode") {
		t.Fatalf("notices = %q, want one naming the setting", n)
	}
	// A configured browser that does not exist is named in the notice
	// instead of silently replaced by auto-detection.
	m.SetBrowser("/nope/chrome", 0)
	n = notices(drain(m, m.ToggleBrowser()))
	if len(n) != 1 || !strings.Contains(n[0], `"/nope/chrome"`) {
		t.Fatalf("notices = %q, want the configured path named", n)
	}
}

func TestFindBrowserResolution(t *testing.T) {
	browserEnv(t, "chromium", fakeBrowserScript)
	p, ok := FindBrowser("")
	if !ok || filepath.Base(p) != "chromium" {
		t.Fatalf("auto-detect = %q, %v; want the chromium on PATH", p, ok)
	}
	if p2, ok := FindBrowser(p); !ok || p2 != p {
		t.Fatalf("configured absolute path = %q, %v", p2, ok)
	}
	if _, ok := FindBrowser("chromium"); !ok {
		t.Fatal("a configured bare name must resolve on PATH")
	}
	if _, ok := FindBrowser(t.TempDir()); ok {
		t.Fatal("a directory is not a browser")
	}
}

// TestBrowserModeShowsScreenshot: b runs the browser with the documented
// flags on a copy of the page carrying a <base href>, the trimmed PNG shows
// as the pane's one Kitty placement, and the render's temp directory — page,
// profile and PNG — is gone afterwards.
func TestBrowserModeShowsScreenshot(t *testing.T) {
	argLog, pageCopy := browserEnv(t, "google-chrome", fakeBrowserScript)
	docDir := t.TempDir()
	m := browserPreview(t, docDir)
	textIDs := m.ImageIDs()
	msgs := drain(m, m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"}))
	if n := notices(msgs); len(n) > 0 {
		t.Fatalf("unexpected notices %q", n)
	}
	if !m.BrowserMode() || m.Shot() == nil {
		t.Fatal("b must switch to browser mode and show the screenshot")
	}
	if !m.TakeModeChange() || m.TakeModeChange() {
		t.Fatal("the flip must be reported once, for the layout save")
	}
	args, _ := os.ReadFile(argLog)
	for _, want := range []string{"--headless=new", "--disable-gpu", "--no-first-run", "--no-default-browser-check", "--hide-scrollbars", "--window-size=960,4096"} {
		if !strings.Contains(string(args), want+"\n") {
			t.Errorf("browser args miss %q:\n%s", want, args)
		}
	}
	page, _ := os.ReadFile(pageCopy)
	if !strings.Contains(string(page), `<base href="`+fileURL(docDir)+`/">`) || !strings.Contains(string(page), "<p>hello</p>") {
		t.Fatalf("page handed to the browser = %q", page)
	}
	// The fixture's 400 rows trim to the content plus the margin, and the
	// tall page opens zoomed to the pane width.
	if v := m.View(); !strings.Contains(v, "100×316 px") {
		t.Fatalf("the footer must show the trimmed size:\n%s", v)
	}
	if w, _ := m.Shot().Grid(); w != 60 || m.Shot().AtFit() {
		t.Fatalf("shot grid width = %d (fit %v), want the pane width", w, m.Shot().AtFit())
	}
	if ids := m.ImageIDs(); len(ids) != 1 || ids[0] != m.Shot().ID() || slices.Equal(ids, textIDs) {
		t.Fatalf("ImageIDs = %v, want the screenshot's id only", ids)
	}
	if !m.HasImages() || len(m.SyncSeqs()) == 0 || len(m.TransmittedIDs()) != 1 {
		t.Fatal("the screenshot must go through the Kitty reconcile")
	}
	if v := m.View(); !strings.Contains(v, "page.html (browser)") {
		t.Fatalf("view must be the image viewer with its footer:\n%s", v)
	}
	if !strings.HasPrefix(m.BrowserStatus(), "browser") {
		t.Fatalf("status = %q", m.BrowserStatus())
	}
	left, err := os.ReadDir(shotBaseDir())
	if err != nil || len(left) != 0 {
		t.Fatalf("render temp dirs left behind: %v (err %v)", left, err)
	}
	if !strings.HasPrefix(shotBaseDir(), os.Getenv("IKE_CONFIG_DIR")) {
		t.Fatalf("temp dirs live at %q, want under the scratch area", shotBaseDir())
	}
	// b again returns to text mode; the text rendering's view comes back.
	drain(m, m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"}))
	if m.BrowserMode() || m.Shot() != nil || !strings.Contains(m.View(), "hello") {
		t.Fatalf("second b must return to text mode:\n%s", m.View())
	}
}

// TestBrowserCacheAndRerender: toggling back with the same text reuses the
// cached screenshot, an edit marks it stale without re-running the browser,
// and r re-renders — keeping the reading position.
func TestBrowserCacheAndRerender(t *testing.T) {
	argLog, pageCopy := browserEnv(t, "google-chrome", fakeBrowserScript)
	m := browserPreview(t, t.TempDir())
	drain(m, m.ToggleBrowser())
	first := m.Shot()
	drain(m, m.ToggleBrowser())
	if cmd := m.ToggleBrowser(); cmd != nil {
		t.Fatal("an unchanged document must reuse the cached screenshot")
	}
	if m.Shot() != first || runs(t, argLog) != 1 {
		t.Fatalf("browser ran %d times, want 1 (cached)", runs(t, argLog))
	}
	m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}) // pans the screenshot
	before := m.Shot().ViewState()

	m.SetSourceImmediate(strings.Replace(browserDoc, "hello", "edited", 1))
	drain(m, m.RenderCmd())
	if runs(t, argLog) != 1 || !strings.Contains(m.BrowserStatus(), "stale") {
		t.Fatalf("an edit must not re-run the browser but mark the shot stale (runs %d, status %q)", runs(t, argLog), m.BrowserStatus())
	}
	drain(m, m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"}))
	if runs(t, argLog) != 2 || m.Shot() == first {
		t.Fatalf("r must re-render (runs %d)", runs(t, argLog))
	}
	if page, _ := os.ReadFile(pageCopy); !strings.Contains(string(page), "edited") {
		t.Fatalf("the re-render must see the edit: %q", page)
	}
	if strings.Contains(m.BrowserStatus(), "stale") {
		t.Fatalf("status after r = %q", m.BrowserStatus())
	}
	if got := m.Shot().ViewState(); got != before {
		t.Fatalf("r must keep the zoom and pan: %+v, want %+v", got, before)
	}
}

// TestBrowserKeysZoomAndPan: while the screenshot shows, the vim motions pan
// it and +/- zoom, instead of scrolling the text rendering.
func TestBrowserKeysZoomAndPan(t *testing.T) {
	browserEnv(t, "google-chrome", fakeBrowserScript)
	m := browserPreview(t, t.TempDir())
	drain(m, m.ToggleBrowser())
	z := m.Shot().Zoom()
	m.Update(tea.KeyPressMsg{Code: '+', Text: "+"})
	if m.Shot().Zoom() <= z {
		t.Fatalf("+ must zoom in: %v → %v", z, m.Shot().Zoom())
	}
	m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.Shot().ViewState().PanY == 0 {
		t.Fatal("j must pan the zoomed screenshot down")
	}
	m.ScrollBy(-100)
	if m.Shot().ViewState().PanY != 0 {
		t.Fatal("the wheel must pan the screenshot back up")
	}
	if m.Top() != 0 {
		t.Fatal("the text rendering must not scroll underneath")
	}
}

// TestBrowserTimeoutFallsBack: a browser that outlives the timeout is killed
// and the pane falls back to text mode with a notice; its temp dir is gone.
func TestBrowserTimeoutFallsBack(t *testing.T) {
	browserEnv(t, "google-chrome", "exec sleep 30\n")
	m := browserPreview(t, t.TempDir())
	m.SetBrowser("", 1)
	start := time.Now()
	n := notices(drain(m, m.ToggleBrowser()))
	if time.Since(start) > 10*time.Second {
		t.Fatalf("the timeout did not kill the browser (%s)", time.Since(start))
	}
	if m.BrowserMode() || len(n) != 1 || !strings.Contains(n[0], "timed out after 1s") || !strings.Contains(n[0], "text mode") {
		t.Fatalf("mode %v, notices %q", m.BrowserMode(), n)
	}
	if left, _ := os.ReadDir(shotBaseDir()); len(left) != 0 {
		t.Fatalf("temp dirs left behind: %v", left)
	}
}

// TestBrowserErrorFallsBack: a failing browser's last stderr line is the
// notice, and the pane is back in text mode.
func TestBrowserErrorFallsBack(t *testing.T) {
	browserEnv(t, "google-chrome", "echo 'noise' >&2\necho 'cannot open display' >&2\nexit 1\n")
	m := browserPreview(t, t.TempDir())
	n := notices(drain(m, m.ToggleBrowser()))
	if m.BrowserMode() || len(n) != 1 || !strings.Contains(n[0], "cannot open display") {
		t.Fatalf("mode %v, notices %q", m.BrowserMode(), n)
	}
	if !strings.Contains(m.View(), "hello") {
		t.Fatalf("text mode must show the rendering:\n%s", m.View())
	}
}

// TestBrowserModeRestore: a pane restored in browser mode owes its
// screenshot to the next RenderCmd; one sized only later dispatches then.
func TestBrowserModeRestore(t *testing.T) {
	argLog, _ := browserEnv(t, "google-chrome", fakeBrowserScript)
	m := New("htmlpreview", filepath.Join(t.TempDir(), "page.html"), theme.DefaultPalette())
	m.SetBrowserMode(true)
	m.SetSourceImmediate(browserDoc)
	if m.RenderCmd() != nil {
		t.Fatal("nothing may dispatch before the pane has a width")
	}
	m.SetSize(60, 30)
	m.SetGraphics(true)
	drain(&m, m.RenderCmd())
	if runs(t, argLog) != 1 || m.Shot() == nil || !m.BrowserMode() {
		t.Fatalf("restore must take the screenshot (runs %d)", runs(t, argLog))
	}
}

// TestInterruptOwesScreenshot: a workspace park cancels the screenshot in
// flight and owes it, so the resume takes it again instead of waiting for a
// result nothing routes back.
func TestInterruptOwesScreenshot(t *testing.T) {
	argLog, _ := browserEnv(t, "google-chrome", fakeBrowserScript)
	m := browserPreview(t, t.TempDir())
	cmd := m.ToggleBrowser()
	m.Interrupt()
	if m.ShotRunning() {
		t.Fatal("the park must cancel the screenshot")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("the cancelled screenshot must not report back, got %T", msg)
	}
	drain(m, m.RenderCmd())
	if m.Shot() == nil || !m.BrowserMode() || runs(t, argLog) < 1 {
		t.Fatal("the resume's render pass must take the screenshot")
	}
}

// TestClosingPaneCancelsScreenshot: a screenshot in flight is abandoned when
// the pane closes — the browser is killed and nothing reports back.
func TestClosingPaneCancelsScreenshot(t *testing.T) {
	browserEnv(t, "google-chrome", "exec sleep 30\n")
	m := browserPreview(t, t.TempDir())
	cmd := m.ToggleBrowser()
	if cmd == nil || !m.ShotRunning() {
		t.Fatal("setup: the screenshot must dispatch")
	}
	if !strings.Contains(m.View(), strings.TrimSpace(shotNotice)) {
		t.Fatalf("the pending screenshot must be marked:\n%s", m.View())
	}
	m.Close()
	start := time.Now()
	if msg := cmd(); msg != nil {
		t.Fatalf("a closed pane's screenshot must be cancelled, got %T", msg)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("cancellation did not kill the browser")
	}
}
