package perfhud

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"ike/internal/theme"
)

// sampleFixture is a filled-in Sample so the renderers are exercised with
// every field populated, not with a zero value.
func sampleFixture() Sample {
	return Sample{
		At:      time.Unix(1_700_000_000, 0).UTC(),
		Window:  time.Second,
		Live:    true,
		Msgs:    14,
		MsgRate: 14,
		Rates: [CatCount]float64{
			CatKey: 2, CatMouse: 0, CatResize: 0, CatTick: 11, CatOther: 1,
		},
		Types: []TypeRate{
			{Type: "app.followTickMsg", N: 11, Rate: 11},
			{Type: "tea.KeyPressMsg", N: 2, Rate: 2},
		},
		Frames:     12,
		FrameRate:  12,
		FrameAvg:   2400 * time.Microsecond,
		FrameMax:   8100 * time.Microsecond,
		Panes:      []PaneCost{{Key: "editor:2", Frames: 12, Avg: 1200 * time.Microsecond, Total: 14400 * time.Microsecond}},
		Goroutines: 84,
		Timers:     2,
		GCs:        1,
		GCPause:    400 * time.Microsecond,
		HeapInuse:  41 << 20,
		HeapSys:    60 << 20,
		RSS:        120 << 20,
	}
}

func TestSnapshotTextCarriesEveryMetric(t *testing.T) {
	s := sampleFixture()
	hist := []Sample{s, s}
	out := SnapshotText(s, hist)
	for _, want := range []string{
		"performance snapshot",
		"messages: 14.0/s",
		"key 2.0/s",
		"tick 11.0/s",
		"app.followTickMsg: 11",
		"frames: 12.0 fps",
		"84 goroutines",
		"2 armed timers",
		"heap in use 41.0 MiB",
		"rss 120.0 MiB",
		"editor:2",
		"history:",
		"msgs/s",
		"goroutines min",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("snapshot missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("snapshot carries ANSI styling; it must be plain text")
	}
}

func TestSnapshotTextSaysWhenRatesAreNotCollected(t *testing.T) {
	s := sampleFixture()
	s.Live = false
	out := SnapshotText(s, nil)
	if !strings.Contains(out, "not collected") || !strings.Contains(out, "perf.hud") {
		t.Fatalf("snapshot with the HUD off must say so:\n%s", out)
	}
	if strings.Contains(out, "messages: ") {
		t.Fatalf("snapshot printed uncollected rates as if measured:\n%s", out)
	}
	// The runtime gauges are still worth having.
	if !strings.Contains(out, "84 goroutines") {
		t.Fatalf("snapshot dropped the runtime gauges:\n%s", out)
	}
}

func TestSnapshotLabelsAPeakRSS(t *testing.T) {
	s := sampleFixture()
	s.RSSPeak = true
	if out := SnapshotText(s, nil); !strings.Contains(out, "rss (peak) 120.0 MiB") {
		t.Fatalf("a peak RSS must be labelled:\n%s", out)
	}
}

func TestRenderStaysInsideTheGivenWidth(t *testing.T) {
	s := sampleFixture()
	hist := []Sample{s, s, s}
	for _, w := range []int{30, 44, 120} {
		box := Render(s, hist, theme.DefaultPalette(), w)
		if box == "" {
			t.Fatalf("width %d rendered nothing", w)
		}
		if got := lipgloss.Width(box); got > w {
			t.Errorf("width %d: box is %d columns wide", w, got)
		}
		for _, line := range strings.Split(box, "\n") {
			if got := lipgloss.Width(line); got > w {
				t.Errorf("width %d: line %q is %d columns", w, line, got)
			}
		}
	}
}

func TestRenderRefusesAHopelesslyNarrowScreen(t *testing.T) {
	if box := Render(sampleFixture(), nil, theme.DefaultPalette(), 10); box != "" {
		t.Fatalf("a 10-column screen should render no HUD, got %q", box)
	}
}

func TestBodyLinesShowTheLoudestSources(t *testing.T) {
	lines := strings.Join(bodyLines(sampleFixture(), nil), "\n")
	for _, want := range []string{"msgs  14.0/s", "tick 11.0", "key 2.0", "followTickMsg", "frame 2.4ms", "go 84", "heap 41.0 MiB", "editor:2"} {
		if !strings.Contains(lines, want) {
			t.Errorf("HUD body missing %q:\n%s", want, lines)
		}
	}
	// Zero buckets are dropped rather than printed as measured zeros.
	if strings.Contains(lines, "mouse") || strings.Contains(lines, "resize") {
		t.Errorf("HUD body printed empty categories:\n%s", lines)
	}
}

func TestBodyLinesWithCollectionOff(t *testing.T) {
	s := sampleFixture()
	s.Live = false
	lines := strings.Join(bodyLines(s, nil), "\n")
	if !strings.Contains(lines, "collection off") {
		t.Errorf("HUD body must say the rates are not collected:\n%s", lines)
	}
	if strings.Contains(lines, "panes") {
		t.Errorf("HUD body listed panes without collection:\n%s", lines)
	}
}

func TestSparkTracksTheHistoryShape(t *testing.T) {
	hist := []Sample{{MsgRate: 0}, {MsgRate: 5}, {MsgRate: 10}}
	got := spark(hist, func(s Sample) float64 { return s.MsgRate })
	if []rune(got)[0] != '▁' || []rune(got)[2] != '█' {
		t.Fatalf("spark = %q, want a ramp from ▁ to █", got)
	}
	if n := len([]rune(got)); n != 3 {
		t.Fatalf("spark has %d cells, want 3", n)
	}
	if got := spark(nil, func(s Sample) float64 { return 0 }); got != "" {
		t.Fatalf("empty history sparked %q", got)
	}
	// A flat-zero history must not divide by zero.
	if got := spark([]Sample{{}, {}}, func(s Sample) float64 { return s.MsgRate }); got != "▁▁" {
		t.Fatalf("flat history sparked %q, want ▁▁", got)
	}
}

func TestSparkIsBoundedByTheBoxWidth(t *testing.T) {
	hist := make([]Sample, 100)
	for i := range hist {
		hist[i].MsgRate = float64(i)
	}
	got := spark(hist, func(s Sample) float64 { return s.MsgRate })
	if n := len([]rune(got)); n != sparkLen {
		t.Fatalf("spark has %d cells, want %d", n, sparkLen)
	}
	if []rune(got)[sparkLen-1] != '█' {
		t.Fatalf("spark = %q, want the newest (largest) sample last", got)
	}
}

func TestRangeLine(t *testing.T) {
	hist := []Sample{{MsgRate: 1}, {MsgRate: 3}, {MsgRate: 5}}
	got := rangeLine(hist, func(s Sample) float64 { return s.MsgRate })
	if got != "min 1.0  avg 3.0  max 5.0" {
		t.Fatalf("rangeLine = %q", got)
	}
	if got := rangeLine(nil, func(s Sample) float64 { return 0 }); got != "n/a" {
		t.Fatalf("empty rangeLine = %q, want n/a", got)
	}
}

func TestFormatHelpers(t *testing.T) {
	durs := map[time.Duration]string{
		0:                       "0",
		500 * time.Nanosecond:   "500ns",
		20 * time.Microsecond:   "20µs",
		2400 * time.Microsecond: "2.4ms",
		1500 * time.Millisecond: "1.5s",
	}
	for in, want := range durs {
		if got := fmtDur(in); got != want {
			t.Errorf("fmtDur(%v) = %q, want %q", in, got, want)
		}
	}
	bytes := map[uint64]string{4 << 10: "4 KiB", 3 << 20: "3.0 MiB", 2 << 30: "2.0 GiB"}
	for in, want := range bytes {
		if got := fmtBytes(in); got != want {
			t.Errorf("fmtBytes(%d) = %q, want %q", in, got, want)
		}
	}
	if got := fmtRate(12.34); got != "12.3" {
		t.Errorf("fmtRate(12.34) = %q", got)
	}
	if got := fmtRate(1234.5); got != "1234" {
		t.Errorf("fmtRate(1234.5) = %q", got)
	}
	if got := shortType("app.followTickMsg"); got != "followTickMsg" {
		t.Errorf("shortType = %q", got)
	}
	if got := shortKey("editor:12", 6); got != "…or:12" {
		t.Errorf("shortKey = %q", got)
	}
}
