package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/largefile"
)

// Per-feature large-file degradation (#2159): each per-edit service switches
// off at its own threshold, below the base cliff, and the badge accessors
// report exactly which ones.

func TestPerFeatureThresholdDegradesOnlyThatFeature(t *testing.T) {
	cfg := host.MapConfig{"files.large_file_highlight_kb": "1"}
	m, _ := loadedWith(t, cfg, "big.go", strings.Repeat("// x\n", 500)) // 2.5 KB
	if m.LargeFile() {
		t.Fatal("setup: the file must stay below the base cliff")
	}
	if !m.FeatureOff(largefile.FeatureHighlight) {
		t.Fatal("highlighting must degrade past its own threshold")
	}
	if cmd := m.Reparse(); cmd != nil {
		t.Fatal("degraded highlighting must not schedule a parse")
	}
	for _, f := range []largefile.Feature{largefile.FeatureLSP, largefile.FeatureVCS, largefile.FeatureSearch, largefile.FeatureFormat} {
		if m.FeatureOff(f) {
			t.Fatalf("%s must be untouched by the highlight threshold", f.Label())
		}
	}
	off := m.DegradedFeatures()
	if len(off) != 1 || off[0] != largefile.FeatureHighlight {
		t.Fatalf("DegradedFeatures = %v, want [FeatureHighlight]", off)
	}
}

func TestLSPThresholdSuppressesChangeText(t *testing.T) {
	cfg := host.MapConfig{"files.large_file_lsp_kb": "1"}
	m, _ := loadedWith(t, cfg, "big.txt", strings.Repeat("x", 2048))
	if m.LargeFile() {
		t.Fatal("setup: below the base cliff")
	}
	var got Event
	m.SetEmitter(EmitterFunc(func(e Event) {
		if e.Kind == EventChange {
			got = e
		}
	}))
	m = send(m, key('i'), key('Y'), special(tea.KeyEscape))
	if got.Text != "" || !got.Large {
		t.Fatalf("change events must ship no text past the LSP threshold (Text=%d bytes, Large=%v)", len(got.Text), got.Large)
	}
}

func TestSearchCounterHiddenAboveSearchThreshold(t *testing.T) {
	content := strings.Repeat("needle haystack\n", 200) // ~3 KB
	m, _ := loadedWith(t, host.MapConfig{"files.large_file_search_kb": "1"}, "big.txt", content)
	m = send(m, key('/'), key('n'), special(tea.KeyEnter))
	if c := m.SearchCounter(); c != "" {
		t.Fatalf("the match counter must hide past the search threshold, got %q", c)
	}
	// The same search on an unrestricted editor counts normally.
	m2, _ := loadedWith(t, host.MapConfig{}, "big2.txt", content)
	m2 = send(m2, key('/'), key('n'), special(tea.KeyEnter))
	if c := m2.SearchCounter(); c == "" {
		t.Fatal("setup: the counter must be live without a threshold")
	}
}

func TestFormatThresholdSkipsSaveChain(t *testing.T) {
	cfg := host.MapConfig{
		"files.large_file_format_kb": "1",
		"editor.format_on_save":      "true",
	}
	m, _ := loadedWith(t, cfg, "big.go", strings.Repeat("// x\n", 500))
	if !m.FeatureOff(largefile.FeatureFormat) {
		t.Fatal("format on save must degrade past its threshold")
	}
	if cmd := m.beginSaveChain(false); cmd != nil {
		t.Fatal("a degraded document must save raw, without the LSP chain")
	}
}

func TestForceCodeInsightLiftsPerFeatureDegradation(t *testing.T) {
	cfg := host.MapConfig{"files.large_file_highlight_kb": "1", "files.large_file_vcs_kb": "1"}
	m, _ := loadedWith(t, cfg, "big.go", strings.Repeat("// x\n", 500))
	if len(m.DegradedFeatures()) != 2 {
		t.Fatalf("setup: expected 2 degraded features, got %v", m.DegradedFeatures())
	}
	if m.ForceCodeInsight() == nil {
		t.Fatal("force must apply (and reparse) when only per-feature degradation is active")
	}
	if off := m.DegradedFeatures(); len(off) != 0 {
		t.Fatalf("force must lift every per-feature degradation, still off: %v", off)
	}
}

func TestBaseCliffDegradesEveryFeature(t *testing.T) {
	m, _ := loadedWith(t, smallLimits, "big.txt", strings.Repeat("x", 2048))
	if off := m.DegradedFeatures(); len(off) != int(largefile.FeatureCount) {
		t.Fatalf("past the base cliff every feature is off, got %v", off)
	}
}
