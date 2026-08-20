package app

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/shotpng"
)

// screenshot.go exports the running IDE's own frame as a PNG (#2001). The
// repository already renders cell grids to images for the documentation
// screenshots (internal/shotpng, driven offline by cmd/shotgen); the same
// painter is reachable from inside the app, so a shot for a bug report, the
// wiki or a release note no longer needs a terminal capture and manual
// cropping.
//
// The subject is the *composed* frame — exactly the string View() hands the
// renderer — cropped to a cell rectangle: the focused pane's layout rect, or
// the whole window. Nothing is re-rendered from file content, so conceal
// stand-ins, selections, gutter decorations and popups appear as they are on
// screen. Painting and writing happen off the update loop; the finished path
// lands in the clipboard and in a notification.

// ExportScreenshotMsg runs view.exportScreenshot (Whole false: the focused
// pane) and view.exportWindowScreenshot (Whole true: the entire window).
type ExportScreenshotMsg struct{ Whole bool }

// screenshotDoneMsg reports a finished export back into the update loop: the
// written path, or the error that stopped it.
type screenshotDoneMsg struct {
	Path string
	Err  error
}

// defaultScreenshotDir is where shots land when screenshot.directory is unset.
// It sits next to the user config (~/.ike) rather than in the project: a shot
// is user output, not a project artifact, and nothing has to be gitignored.
const defaultScreenshotDir = "~/.ike/screenshots"

// screenshotStamp is the timestamp format in a shot's file name — sortable,
// second resolution, no characters a shell or a wiki link would have to quote.
const screenshotStamp = "20060102-150405"

// screenshotRender is a seam over the painter so a test can assert what the
// app asks for (frame, size, region, colours) without rendering pixels.
var screenshotRender = shotpng.Render

// screenshotClock is a seam over the wall clock, so a test knows the name a
// capture writes.
var screenshotClock = time.Now

var (
	screenshotFontsOnce sync.Once
	screenshotFontsSet  *shotpng.Fonts
	screenshotFontsErr  error
)

// screenshotFonts loads the platform's monospace faces once per process:
// resolving and parsing them costs enough to be worth keeping, and every shot
// wants the same set.
func screenshotFonts() (*shotpng.Fonts, error) {
	screenshotFontsOnce.Do(func() {
		screenshotFontsSet, screenshotFontsErr = shotpng.LoadFonts(shotpng.DefaultFontSpec(0))
	})
	return screenshotFontsSet, screenshotFontsErr
}

// screenshotDir resolves screenshot.directory: "~" expands, a relative path
// resolves against the project working directory, and an unset value selects
// defaultScreenshotDir.
func (m Model) screenshotDir() string {
	dir := ""
	if cfg := m.host.Config(); cfg != nil {
		if v, ok := cfg.Get("screenshot.directory"); ok {
			dir = v
		}
	}
	if dir == "" {
		dir = defaultScreenshotDir
	}
	dir = expandHome(dir)
	if !filepath.IsAbs(dir) {
		if cwd, err := cachedGetwd(); err == nil {
			dir = filepath.Join(cwd, dir)
		}
	}
	return dir
}

// screenshotRegion is the cell rectangle a capture crops to: the whole frame,
// or the focused pane's rect (borders included — the pane as drawn). ok is
// false when a pane capture has no focused pane to point at.
func (m Model) screenshotRegion(whole bool) (image.Rectangle, bool) {
	if whole {
		return image.Rect(0, 0, m.width, m.height), true
	}
	r, ok := m.lay.Panes[m.activeWS().Panes.Focused()]
	if !ok || r.W <= 0 || r.H <= 0 {
		return image.Rectangle{}, false
	}
	return image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H), true
}

// screenshotPath builds a free path under dir: kind and timestamp, with a
// counter appended when a shot of the same kind was already taken this second.
func screenshotPath(dir, kind string, at time.Time) string {
	base := fmt.Sprintf("ike-%s-%s", kind, at.Format(screenshotStamp))
	path := filepath.Join(dir, base+".png")
	for n := 2; n < 100; n++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			break
		}
		path = filepath.Join(dir, fmt.Sprintf("%s-%d.png", base, n))
	}
	return path
}

// exportScreenshot composes the frame and hands the paint + write to a
// command: the render is cheap (the frame is a string the app produces every
// tick anyway), the PNG is not.
func (m Model) exportScreenshot(whole bool) tea.Cmd {
	if m.width <= 0 || m.height <= 0 {
		m.host.Notify(host.Warn, "screenshot: the window has no size yet")
		return nil
	}
	region, ok := m.screenshotRegion(whole)
	if !ok {
		m.host.Notify(host.Warn, "screenshot: no focused pane to capture")
		return nil
	}
	kind := "pane"
	if whole {
		kind = "window"
	}
	frame := m.render()
	pal := m.pal()
	opt := shotpng.Options{
		Cols:   m.width,
		Rows:   m.height,
		Fg:     pal.Foreground,
		Bg:     pal.Background,
		Region: region,
	}
	path := screenshotPath(m.screenshotDir(), kind, screenshotClock())
	return func() tea.Msg {
		fonts, err := screenshotFonts()
		if err != nil {
			return screenshotDoneMsg{Err: fmt.Errorf("loading the font: %w", err)}
		}
		opt.Fonts = fonts
		// One cell of margin, the same framing the documentation shots use.
		opt.Padding = fonts.CellW
		img, err := screenshotRender(frame, opt)
		if err != nil {
			return screenshotDoneMsg{Err: err}
		}
		if err := shotpng.WriteFile(path, img); err != nil {
			return screenshotDoneMsg{Err: err}
		}
		return screenshotDoneMsg{Path: path}
	}
}

// screenshotDone announces the result: the path goes to the clipboard so it
// can be pasted straight into an issue or a doc, and to a notification so the
// user sees where it landed.
func (m *Model) screenshotDone(msg screenshotDoneMsg) {
	if msg.Err != nil {
		m.host.Notify(host.Warn, "screenshot failed: "+msg.Err.Error())
		return
	}
	clipboardWrite(msg.Path)
	m.host.Notify(host.Info, "screenshot saved: "+msg.Path+" (path copied)")
}
