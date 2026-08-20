package app

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/host"
	"ike/internal/registry"
	"ike/internal/shotpng"
)

// screenshotModel builds a sized model whose screenshot.directory points at
// dir, with a file open so the focused editor pane has recognisable content.
func screenshotModel(t *testing.T, dir string) Model {
	t.Helper()
	m := NewWith(registry.Global(), host.MapConfig{"screenshot.directory": dir})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = tm.(Model)

	cwd, err := cachedGetwd()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cwd, "screenshot_test_fixture.txt")
	if err := os.WriteFile(path, []byte("SCREENSHOTFIXTURE\nsecond line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })
	out, _ := m.openPath(path, false)
	return out.(Model)
}

// captureOptions runs one export through a stubbed painter and returns the
// frame and options the app asked it to paint.
func captureOptions(t *testing.T, m Model, whole bool) (string, shotpng.Options) {
	t.Helper()
	var gotFrame string
	var gotOpt shotpng.Options
	orig := screenshotRender
	screenshotRender = func(frame string, opt shotpng.Options) (*image.RGBA, error) {
		gotFrame, gotOpt = frame, opt
		return image.NewRGBA(image.Rect(0, 0, 1, 1)), nil
	}
	defer func() { screenshotRender = orig }()

	_, cmd := m.Update(ExportScreenshotMsg{Whole: whole})
	if done := screenshotResult(t, cmd); done.Err != nil {
		t.Fatalf("export failed: %v", done.Err)
	}
	return gotFrame, gotOpt
}

// screenshotResult runs the command tree an export produced and picks the
// done message out of it.
func screenshotResult(t *testing.T, cmd tea.Cmd) screenshotDoneMsg {
	t.Helper()
	for _, msg := range cmdMsgs(cmd) {
		if done, ok := msg.(screenshotDoneMsg); ok {
			return done
		}
	}
	t.Fatal("the export produced no screenshot result")
	return screenshotDoneMsg{}
}

// TestScreenshotCommandsRegistered guards #2001: both exports must be registry
// commands, or neither the palette nor the View menu can reach them.
func TestScreenshotCommandsRegistered(t *testing.T) {
	m := newSized()
	for _, id := range []string{"view.exportScreenshot", "view.exportWindowScreenshot"} {
		if _, ok := m.reg.Command(id); !ok {
			t.Fatalf("%s must be a registry command", id)
		}
	}
}

// TestScreenshotPaneRegionIsFocusedPane covers the headline criterion: a pane
// capture paints the composed frame cropped to the focused pane's rectangle,
// and that crop carries the pane's on-screen content.
func TestScreenshotPaneRegionIsFocusedPane(t *testing.T) {
	m := screenshotModel(t, t.TempDir())
	frame, opt := captureOptions(t, m, false)

	r, ok := m.lay.Panes[m.activeWS().Panes.Focused()]
	if !ok {
		t.Fatal("the focused pane must have a layout rect")
	}
	want := image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H)
	if opt.Region != want {
		t.Fatalf("Region = %v, want the focused pane's rect %v", opt.Region, want)
	}
	if opt.Cols != m.width || opt.Rows != m.height {
		t.Fatalf("frame size = %dx%d, want the window's %dx%d", opt.Cols, opt.Rows, m.width, m.height)
	}
	if opt.Fg != m.pal().Foreground || opt.Bg != m.pal().Background {
		t.Fatal("the shot must carry the active theme's default colours")
	}
	// The frame is the composed one, not a re-render from file content: the
	// cropped region holds what the pane draws on screen.
	if text := cropFrame(frame, opt.Region); !strings.Contains(text, "SCREENSHOTFIXTURE") {
		t.Fatalf("the pane crop must show the rendered buffer, got:\n%s", text)
	}
}

// TestScreenshotWindowRegionIsWholeFrame guards the window variant: it crops
// to nothing, so tool panes and the status line are in the shot.
func TestScreenshotWindowRegionIsWholeFrame(t *testing.T) {
	m := screenshotModel(t, t.TempDir())
	_, opt := captureOptions(t, m, true)

	if want := image.Rect(0, 0, m.width, m.height); opt.Region != want {
		t.Fatalf("Region = %v, want the whole frame %v", opt.Region, want)
	}
	_, pane := captureOptions(t, m, false)
	if pane.Region == opt.Region {
		t.Fatal("the pane capture must be smaller than the window: the model has tool panes")
	}
}

// TestScreenshotWritesPNGAndCopiesPath covers the end of the flow: the PNG
// lands in the configured directory at the region's pixel size, and the path
// reaches the clipboard and a notification.
func TestScreenshotWritesPNGAndCopiesPath(t *testing.T) {
	var copied string
	orig := clipboardWrite
	clipboardWrite = func(s string) { copied = s }
	defer func() { clipboardWrite = orig }()

	dir := filepath.Join(t.TempDir(), "shots") // created by the capture
	m := screenshotModel(t, dir)

	tm, cmd := m.Update(ExportScreenshotMsg{})
	m = tm.(Model)
	done := screenshotResult(t, cmd)
	if done.Err != nil {
		t.Fatalf("export failed: %v", done.Err)
	}
	if filepath.Dir(done.Path) != dir {
		t.Fatalf("shot written to %s, want the configured %s", done.Path, dir)
	}

	f, err := os.Open(done.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		t.Fatalf("the shot must be a PNG: %v", err)
	}
	fonts, err := screenshotFonts()
	if err != nil {
		t.Fatal(err)
	}
	r := m.lay.Panes[m.activeWS().Panes.Focused()]
	wantW := 2*fonts.CellW + r.W*fonts.CellW
	wantH := 2*fonts.CellW + r.H*fonts.CellH
	if cfg.Width != wantW || cfg.Height != wantH {
		t.Fatalf("shot = %dx%d px, want the pane's %dx%d", cfg.Width, cfg.Height, wantW, wantH)
	}

	tm, _ = m.Update(done)
	m = tm.(Model)
	if copied != done.Path {
		t.Fatalf("clipboard = %q, want the written path %q", copied, done.Path)
	}
	if n := lastNotification(t, m); !strings.Contains(n, done.Path) {
		t.Fatalf("the capture must notify with the path, got %q", n)
	}
}

// TestScreenshotFailureNotifies keeps a broken render from being silent.
func TestScreenshotFailureNotifies(t *testing.T) {
	m := screenshotModel(t, t.TempDir())
	tm, _ := m.Update(screenshotDoneMsg{Err: os.ErrPermission})
	m = tm.(Model)
	if n := lastNotification(t, m); !strings.Contains(n, "screenshot failed") {
		t.Fatalf("a failed capture must say so, got %q", n)
	}
}

// TestScreenshotPathNaming pins the file name: kind plus a sortable timestamp,
// with a counter when a shot of the same kind was taken in the same second.
func TestScreenshotPathNaming(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 8, 20, 19, 30, 5, 0, time.UTC)
	first := screenshotPath(dir, "pane", at)
	if got, want := filepath.Base(first), "ike-pane-20260820-193005.png"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
	if err := os.WriteFile(first, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := filepath.Base(screenshotPath(dir, "pane", at)), "ike-pane-20260820-193005-2.png"; got != want {
		t.Fatalf("second name = %q, want %q", got, want)
	}
	if got, want := filepath.Base(screenshotPath(dir, "window", at)), "ike-window-20260820-193005.png"; got != want {
		t.Fatalf("window name = %q, want %q", got, want)
	}
}

// TestScreenshotDirResolution covers the setting's three forms: unset falls
// back to ~/.ike/screenshots, "~" expands, and a relative path resolves
// against the project directory.
func TestScreenshotDirResolution(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	unset := NewWith(registry.New(), host.MapConfig{})
	if got, want := unset.screenshotDir(), filepath.Join(home, ".ike", "screenshots"); got != want {
		t.Fatalf("default dir = %q, want %q", got, want)
	}
	tilde := NewWith(registry.New(), host.MapConfig{"screenshot.directory": "~/shots"})
	if got, want := tilde.screenshotDir(), filepath.Join(home, "shots"); got != want {
		t.Fatalf("~ dir = %q, want %q", got, want)
	}
	rel := NewWith(registry.New(), host.MapConfig{"screenshot.directory": "docs/shots"})
	cwd, err := cachedGetwd()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rel.screenshotDir(), filepath.Join(cwd, "docs", "shots"); got != want {
		t.Fatalf("relative dir = %q, want %q", got, want)
	}
}

// cropFrame returns the plain text inside a cell region of a rendered frame,
// the test-side equivalent of what the painter crops to.
func cropFrame(frame string, region image.Rectangle) string {
	var out []string
	lines := strings.Split(frame, "\n")
	for y := region.Min.Y; y < region.Max.Y && y < len(lines); y++ {
		row := ansi.Strip(lines[y])
		runes := []rune(row)
		lo, hi := region.Min.X, region.Max.X
		if lo > len(runes) {
			lo = len(runes)
		}
		if hi > len(runes) {
			hi = len(runes)
		}
		out = append(out, string(runes[lo:hi]))
	}
	return strings.Join(out, "\n")
}
