package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/theme"
	"ike/internal/watch"
)

// tabbar_test.go covers the tab bar rendering (#157): bar visibility, labels
// with disambiguation and dirty/stale markers, active highlighting, and
// overflow windowing around the active tab.

// stripped renders the app frame without SGR sequences.
func stripped(m Model) string { return ansi.Strip(m.render()) }

func TestSingleTabShowsPlainTitle(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "aaa.txt", "aaa\n")
	m := openApp(t, a)
	// The status line renders "…/aaa.txt │ LF" whenever the temp path keeps
	// the divider on screen, which used to trip the tab-bar guard below
	// depending on the random temp-dir length (#457). It can also wrap onto
	// two frame lines when the path overflows the width (#471), so scope the
	// assertion to the pane body by dropping every status-line row instead of
	// just the last line.
	// The bar is the frame's tail: cut at its first row so wrapped
	// continuation rows (which don't repeat the NORMAL marker) go too.
	body := strings.Split(stripped(m), "\n")
	for i, l := range body {
		if strings.Contains(l, "NORMAL │") {
			body = body[:i]
			break
		}
	}
	v := strings.Join(body, "\n")
	if !strings.Contains(v, "aaa.txt") {
		t.Fatal("single-tab pane must show the file title")
	}
	if strings.Contains(v, "aaa.txt │") {
		t.Fatal("single-tab pane must not render a tab bar by default")
	}
}

func TestTwoTabsRenderTabBar(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "aaa.txt", "aaa\n")
	b := writeTemp(t, dir, "bbb.txt", "bbb\n")
	m := openApp(t, a, b)
	if !strings.Contains(stripped(m), "aaa.txt ✕ │ bbb.txt ✕") {
		t.Fatalf("two tabs must render as a bar; frame:\n%s", stripped(m))
	}
}

func TestTabBarAlwaysShowConfig(t *testing.T) {
	confDir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", confDir)
	if err := os.WriteFile(filepath.Join(confDir, "settings.toml"),
		[]byte("[editor.tabs]\nalways_show = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = tm.(Model)
	a := writeTemp(t, t.TempDir(), "solo.txt", "solo\n")
	tm, _ = m.openPath(a, false)
	m = tm.(Model)
	if !m.tabsAlwaysShow() {
		t.Fatal("config editor.tabs.always_show must reach the app")
	}
	inst := m.activeWS().Panes.FocusedInstance()
	if bar, ok := m.tabBar(inst, 60); !ok || !strings.Contains(ansi.Strip(bar), "solo.txt") {
		t.Fatal("always_show must render the bar for a single tab")
	}
}

func TestTabLabelsDirtyAndDisambiguation(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "one"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "two"), 0o755); err != nil {
		t.Fatal(err)
	}
	a := writeTemp(t, filepath.Join(dir, "one"), "x.txt", "aaa\n")
	b := writeTemp(t, filepath.Join(dir, "two"), "x.txt", "bbb\n")
	m := openApp(t, a, b)

	inst := m.activeWS().Panes.FocusedInstance()
	inst.Editor().RestoreText("dirty now") // active tab (b) becomes dirty

	labels := tabLabels(inst)
	if len(labels) != 2 {
		t.Fatalf("want 2 labels, got %v", labels)
	}
	if !strings.Contains(labels[0], "x.txt — ") || !strings.Contains(labels[0], "one") {
		t.Fatalf("duplicate basenames must disambiguate by directory, got %q", labels[0])
	}
	if !strings.Contains(labels[1], "two") {
		t.Fatalf("duplicate basenames must disambiguate by directory, got %q", labels[1])
	}
	if !strings.HasSuffix(labels[1], "●") {
		t.Fatalf("dirty tab must carry the ● marker, got %q", labels[1])
	}
	if strings.Contains(labels[0], "●") {
		t.Fatalf("clean tab must not carry a dirty marker, got %q", labels[0])
	}
}

func TestRenderTabBarFitsAndSeparates(t *testing.T) {
	pal := theme.DefaultPalette()
	bar := renderTabBar([]string{"a.go", "b.go", "c.go"}, 1, 60, pal)
	plain := ansi.Strip(bar)
	if got := ansi.StringWidth(plain); got > 60 {
		t.Fatalf("bar exceeds width: %d > 60 (%q)", got, plain)
	}
	if !strings.Contains(plain, "a.go ✕ │ b.go ✕ │ c.go ✕") {
		t.Fatalf("all tabs must render when they fit, got %q", plain)
	}
	if strings.Contains(plain, tabEllipsis) {
		t.Fatalf("no ellipsis when everything fits, got %q", plain)
	}
}

func TestRenderTabBarOverflowsAroundActive(t *testing.T) {
	pal := theme.DefaultPalette()
	labels := []string{"first.go", "second.go", "third.go", "fourth.go", "fifth.go"}
	bar := renderTabBar(labels, 4, 24, pal)
	plain := ansi.Strip(bar)
	if got := ansi.StringWidth(plain); got > 24 {
		t.Fatalf("bar exceeds width: %d > 24 (%q)", got, plain)
	}
	if !strings.Contains(plain, "fifth.go") {
		t.Fatalf("the active tab must stay visible, got %q", plain)
	}
	if !strings.HasPrefix(plain, "+4") {
		t.Fatalf("hidden left tabs must be counted by a +N indicator, got %q", plain)
	}
	if strings.Contains(plain, "\n") {
		t.Fatalf("the bar must never wrap, got %q", plain)
	}
}

// TestTabOverflowIndicatorCountsBothEnds (#2151): with the active tab in the
// middle of a long list, both ends carry a "+N" counting exactly the tabs
// hidden there, and the row still fits.
func TestTabOverflowIndicatorCountsBothEnds(t *testing.T) {
	pal := theme.DefaultPalette()
	labels := make([]string, 12)
	for i := range labels {
		labels[i] = "f" + strconv.Itoa(i) + ".go"
	}
	const width = 30
	bar := renderTabBar(labels, 6, width, pal)
	plain := ansi.Strip(bar)
	if got := ansi.StringWidth(plain); got > width {
		t.Fatalf("bar exceeds width: %d > %d (%q)", got, width, plain)
	}
	lo, hi := tabWindow(labels, 6, width)
	if lo > 6 || hi < 6 {
		t.Fatalf("active tab outside the window [%d,%d]", lo, hi)
	}
	if lo == 0 || hi == len(labels)-1 {
		t.Fatalf("this fixture must overflow at both ends, got window [%d,%d]", lo, hi)
	}
	if !strings.HasPrefix(plain, "+"+strconv.Itoa(lo)) {
		t.Fatalf("left indicator must count the %d hidden tabs, got %q", lo, plain)
	}
	if !strings.HasSuffix(plain, "+"+strconv.Itoa(len(labels)-1-hi)) {
		t.Fatalf("right indicator must count the %d hidden tabs, got %q", len(labels)-1-hi, plain)
	}
}

// TestTabWindowKeepsActiveVisible (#2151): whatever the tab count, the width
// or the active index, the window contains the active tab and the rendered row
// fits — the overflow contract the tab strip promises.
func TestTabWindowKeepsActiveVisible(t *testing.T) {
	pal := theme.DefaultPalette()
	labels := make([]string, 40)
	for i := range labels {
		labels[i] = strings.Repeat("x", 1+i%30) + strconv.Itoa(i) + ".go"
	}
	for _, width := range []int{6, 10, 24, 40, 80, 200} {
		for _, active := range []int{0, 1, 17, 38, 39} {
			lo, hi := tabWindow(labels, active, width)
			if active < lo || active > hi {
				t.Fatalf("width %d, active %d: window [%d,%d] hides the active tab", width, active, lo, hi)
			}
			plain := ansi.Strip(renderTabBar(labels, active, width, pal))
			if got := ansi.StringWidth(plain); got > width {
				t.Fatalf("width %d, active %d: bar %d cells wide (%q)", width, active, got, plain)
			}
			if strings.Contains(plain, "\n") {
				t.Fatalf("width %d, active %d: bar wrapped (%q)", width, active, plain)
			}
		}
	}
}

// TestTabLabelsEllipsizeAtCap (#2151): an over-long label is cut to the cap so
// one tab cannot crowd out every neighbour, and the hit geometry agrees with
// what was drawn.
func TestTabLabelsEllipsizeAtCap(t *testing.T) {
	pal := theme.DefaultPalette()
	long := "a-really-long-generated-file-name-v2.go"
	labels := []string{"a.go", long, "b.go"}
	plain := ansi.Strip(renderTabBar(labels, 1, 60, pal))
	if strings.Contains(plain, long) {
		t.Fatalf("an over-long label must ellipsize, got %q", plain)
	}
	fitted := fitTabLabels(labels)
	if ansi.StringWidth(fitted[1]) != tabLabelCap || !strings.HasSuffix(fitted[1], tabEllipsis) {
		t.Fatalf("label must be cut to %d cells with an ellipsis, got %q", tabLabelCap, fitted[1])
	}
	if !strings.Contains(plain, "a.go") || !strings.Contains(plain, "b.go") {
		t.Fatalf("neighbours must still fit next to the capped label, got %q", plain)
	}
	// The ellipsized label's own cells hit-test as its tab.
	x := strings.Index(plain, fitted[1])
	if x < 0 {
		t.Fatalf("capped label not found in %q", plain)
	}
	if idx := tabAt(labels, 1, 60, x); idx != 1 {
		t.Fatalf("cell %d must hit tab 1, got %d", x, idx)
	}
}

func TestRenderTabBarTruncatesLoneOversizedTab(t *testing.T) {
	pal := theme.DefaultPalette()
	labels := []string{"short.go", "a-very-long-file-name-that-overflows.go"}
	bar := renderTabBar(labels, 1, 16, pal)
	plain := ansi.Strip(bar)
	if got := ansi.StringWidth(plain); got > 16 {
		t.Fatalf("bar exceeds width: %d > 16 (%q)", got, plain)
	}
	if !strings.Contains(plain, "a-very") {
		t.Fatalf("the active tab must render truncated, got %q", plain)
	}
}

func TestStaleTabCarriesMarker(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "aaa.txt", "aaa\n")
	b := writeTemp(t, dir, "bbb.txt", "bbb\n")
	m := openApp(t, a, b)
	inst := m.activeWS().Panes.FocusedInstance()
	inst.Editor().RestoreText("dirty")
	tm, _ := m.Update(watch.EventMsg{Kind: watch.FileChanged, Path: b})
	m = tm.(Model)
	labels := tabLabels(inst)
	if !strings.HasSuffix(labels[1], "!") {
		t.Fatalf("stale tab must carry the ! marker, got %q", labels[1])
	}
}
