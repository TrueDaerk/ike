package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/largefile"
	"ike/internal/registry"
)

// largeModel opens a file flagged large (line threshold forced to 1 via the
// config seam is not reachable here, so the file simply exceeds the default
// KB threshold with a small synthetic limit override through config).
func largeModel(t *testing.T) (Model, string) {
	t.Helper()
	largefile.Reset()
	t.Cleanup(largefile.Reset)
	m := sizedWith(t, registry.New(), 200, 40)
	// Lower the line threshold via the model's config seam: write the config
	// through the host is heavyweight; instead generate a file exceeding the
	// default 100k lines cheaply? Too big — use the KB threshold with a >1MB
	// sparse content instead: 1.1 MB of newlines is fast to write.
	path := filepath.Join(t.TempDir(), "big.html")
	if err := os.WriteFile(path, []byte(strings.Repeat("x<br>\n", 200_000)), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.openPath(path, false)
	return out.(Model), path
}

// TestLargeFileBannerShowsAndDismisses guards #1124.
func TestLargeFileBannerShowsAndDismisses(t *testing.T) {
	m, path := largeModel(t)
	text, _, _, _, ok := m.largeFileBanner()
	if !ok {
		t.Fatal("flagged focused document must show the banner")
	}
	if !strings.Contains(text, "Force Code Insight") || !strings.Contains(text, "large_file_kb") {
		t.Fatalf("banner must name both remedies: %q", text)
	}
	if !strings.Contains(m.render(), "Large file") {
		t.Fatal("banner must be composited into the frame")
	}
	// Esc dismisses per document and survives re-checks.
	m = step(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if _, _, _, _, ok := m.largeFileBanner(); ok {
		t.Fatal("esc must dismiss the banner")
	}
	if !largefile.NoticeDismissed(path) {
		t.Fatal("dismissal must be recorded per document")
	}
}

// TestLargeFileBannerForceRemoves guards #1124: forcing code insight removes
// the banner immediately (InsightOff turns false).
func TestLargeFileBannerForceRemoves(t *testing.T) {
	m, _ := largeModel(t)
	if _, _, _, _, ok := m.largeFileBanner(); !ok {
		t.Fatal("setup: banner missing")
	}
	m = step(m, ForceCodeInsightMsg{})
	if _, _, _, _, ok := m.largeFileBanner(); ok {
		t.Fatal("force code insight must remove the banner")
	}
}

// TestLargeFileBannerAbsentForNormalFiles guards #1124.
func TestLargeFileBannerAbsentForNormalFiles(t *testing.T) {
	largefile.Reset()
	t.Cleanup(largefile.Reset)
	m := sizedWith(t, registry.New(), 120, 40)
	path := filepath.Join(t.TempDir(), "small.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.openPath(path, false)
	m = out.(Model)
	if _, _, _, _, ok := m.largeFileBanner(); ok {
		t.Fatal("normal files must not show the banner")
	}
}
