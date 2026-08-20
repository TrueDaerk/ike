package perfhud

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// perfhud_test.go pins the collection half of the HUD (#1999): counter
// aggregation into rates, per-pane attribution, the window reset between
// samples, the bounded history, and — the acceptance criterion that matters
// most — that a disabled collector records nothing at all.

// fakeTickMsg stands in for an app timer deadline: classification recognises
// those by the "Tick" spelling every ticker in the codebase uses.
type fakeTickMsg struct{}

// fakeResultMsg stands in for a background Cmd's result.
type fakeResultMsg struct{}

// testCollector returns a collector on a controllable clock, already enabled.
// The returned advance function moves the clock forward.
func testCollector(t *testing.T) (*Collector, func(time.Duration)) {
	t.Helper()
	now := time.Unix(1_700_000_000, 0)
	c := New()
	c.SetNow(func() time.Time { return now })
	c.SetEnabled(true)
	return c, func(d time.Duration) { now = now.Add(d) }
}

func TestCountAggregatesIntoPerCategoryRates(t *testing.T) {
	c, advance := testCollector(t)
	for i := 0; i < 4; i++ {
		c.Count(tea.KeyPressMsg{})
	}
	c.Count(tea.MouseClickMsg{})
	c.Count(fakeTickMsg{})
	c.Count(fakeResultMsg{})
	advance(2 * time.Second)

	s := c.Sample(3)
	if s.Msgs != 7 {
		t.Fatalf("Msgs = %d, want 7", s.Msgs)
	}
	if s.Window != 2*time.Second {
		t.Fatalf("Window = %s, want 2s", s.Window)
	}
	if s.MsgRate != 3.5 {
		t.Fatalf("MsgRate = %v, want 3.5", s.MsgRate)
	}
	want := map[Category]float64{CatKey: 2, CatMouse: 0.5, CatResize: 0, CatTick: 0.5, CatOther: 0.5}
	for cat, w := range want {
		if s.Rates[cat] != w {
			t.Errorf("rate[%s] = %v, want %v", cat, s.Rates[cat], w)
		}
	}
	if s.Timers != 3 {
		t.Errorf("Timers = %d, want 3", s.Timers)
	}
	if !s.Live {
		t.Error("Live = false for an enabled collector")
	}
	if len(s.Types) == 0 || s.Types[0].N != 4 || !strings.Contains(s.Types[0].Type, "KeyPressMsg") {
		t.Fatalf("top type = %+v, want KeyPressMsg x4", s.Types)
	}
	if s.Types[0].Rate != 2 {
		t.Errorf("top type rate = %v, want 2", s.Types[0].Rate)
	}
}

func TestDisabledCollectorRecordsNothing(t *testing.T) {
	c, advance := testCollector(t)
	c.SetEnabled(false)
	c.Count(tea.KeyPressMsg{})
	c.RecordFrame(5 * time.Millisecond)
	c.RecordPane("editor", 3*time.Millisecond)
	advance(time.Second)

	s := c.Sample(0)
	if s.Msgs != 0 || s.Frames != 0 || len(s.Panes) != 0 || len(s.Types) != 0 {
		t.Fatalf("disabled collector recorded %+v", s)
	}
	if s.Live {
		t.Error("Live = true with collection off")
	}
	// The runtime gauges are read regardless — that is what lets the snapshot
	// command answer with the HUD off.
	if s.Goroutines == 0 {
		t.Error("Goroutines = 0, want the live runtime count")
	}
}

func TestEnablingDropsTheStaleHistory(t *testing.T) {
	c, advance := testCollector(t)
	advance(time.Second)
	c.Sample(0)
	if len(c.History()) != 1 {
		t.Fatalf("history = %d samples, want 1", len(c.History()))
	}
	c.SetEnabled(false)
	advance(time.Hour) // the HUD was closed for an hour
	c.SetEnabled(true)
	if got := len(c.History()); got != 0 {
		t.Fatalf("history = %d samples after re-enabling, want 0", got)
	}
	advance(time.Second)
	if s := c.Sample(0); s.Window != time.Second {
		t.Fatalf("Window = %s after re-enabling, want 1s (not the closed hour)", s.Window)
	}
}

func TestPaneAttributionRanksByTotalCost(t *testing.T) {
	c, advance := testCollector(t)
	// Two frames: the explorer is cheap, the editor expensive, and a
	// terminal only appeared in the second frame.
	c.RecordPane("editor", 4*time.Millisecond)
	c.RecordPane("explorer", time.Millisecond)
	c.RecordPane("editor", 6*time.Millisecond)
	c.RecordPane("explorer", time.Millisecond)
	c.RecordPane("terminal", 3*time.Millisecond)
	c.RecordFrame(8 * time.Millisecond)
	c.RecordFrame(12 * time.Millisecond)
	advance(2 * time.Second)

	s := c.Sample(0)
	if len(s.Panes) != 3 {
		t.Fatalf("panes = %+v, want 3", s.Panes)
	}
	if s.Panes[0].Key != "editor" || s.Panes[0].Total != 10*time.Millisecond {
		t.Fatalf("first pane = %+v, want editor with 10ms total", s.Panes[0])
	}
	if s.Panes[0].Frames != 2 || s.Panes[0].Avg != 5*time.Millisecond {
		t.Errorf("editor avg = %s over %d frames, want 5ms over 2", s.Panes[0].Avg, s.Panes[0].Frames)
	}
	if s.Panes[1].Key != "terminal" || s.Panes[1].Avg != 3*time.Millisecond {
		t.Errorf("second pane = %+v, want terminal at 3ms", s.Panes[1])
	}
	if s.Panes[2].Key != "explorer" {
		t.Errorf("third pane = %q, want explorer", s.Panes[2].Key)
	}
	if s.Frames != 2 || s.FrameAvg != 10*time.Millisecond || s.FrameMax != 12*time.Millisecond {
		t.Errorf("frames = %d avg %s max %s, want 2 / 10ms / 12ms", s.Frames, s.FrameAvg, s.FrameMax)
	}
	if s.FrameRate != 1 {
		t.Errorf("FrameRate = %v, want 1", s.FrameRate)
	}
}

func TestSampleStartsAFreshWindow(t *testing.T) {
	c, advance := testCollector(t)
	c.Count(tea.KeyPressMsg{})
	c.RecordPane("editor", time.Millisecond)
	advance(time.Second)
	c.Sample(0)

	advance(time.Second)
	s := c.Sample(0)
	if s.Msgs != 0 || len(s.Panes) != 0 || s.Frames != 0 {
		t.Fatalf("second window carried state: %+v", s)
	}
	if s.Window != time.Second {
		t.Fatalf("Window = %s, want 1s", s.Window)
	}
}

func TestHistoryIsBounded(t *testing.T) {
	c, advance := testCollector(t)
	c.SetHistory(3)
	for i := 0; i < 6; i++ {
		advance(time.Second)
		c.Count(tea.KeyPressMsg{})
		c.Sample(0)
	}
	hist := c.History()
	if len(hist) != 3 {
		t.Fatalf("history = %d samples, want 3", len(hist))
	}
	// The kept ones must be the newest, in order.
	for i := 1; i < len(hist); i++ {
		if !hist[i].At.After(hist[i-1].At) {
			t.Fatalf("history out of order at %d: %v", i, hist)
		}
	}
	latest, ok := c.Latest()
	if !ok || !latest.At.Equal(hist[len(hist)-1].At) {
		t.Fatalf("Latest = %v (ok=%v), want the newest history entry", latest.At, ok)
	}
}

func TestLatestBeforeAnySample(t *testing.T) {
	c := New()
	if _, ok := c.Latest(); ok {
		t.Fatal("Latest reported a sample before any was taken")
	}
}

func TestResetClearsEverything(t *testing.T) {
	c, advance := testCollector(t)
	c.Count(tea.KeyPressMsg{})
	advance(time.Second)
	c.Sample(0)
	c.Reset()
	if c.Enabled() {
		t.Error("still enabled after Reset")
	}
	if len(c.History()) != 0 {
		t.Error("history survived Reset")
	}
}

// TestDisabledHooksAllocateNothing is the acceptance guard for "cheap when
// hidden" (#1999, the #1095–#1101 lesson): the hooks sit in the message and
// render hot paths, so with the HUD off they must not put a single allocation
// there.
func TestDisabledHooksAllocateNothing(t *testing.T) {
	c := New()
	var msg tea.Msg = tea.KeyPressMsg{} // boxed once, outside the measured run
	if n := testing.AllocsPerRun(200, func() {
		c.Count(msg)
		c.RecordFrame(time.Millisecond)
		c.RecordPane("editor", time.Millisecond)
	}); n != 0 {
		t.Fatalf("a disabled collector allocated %v times per run", n)
	}
}

func BenchmarkCountDisabled(b *testing.B) {
	c := New()
	var msg tea.Msg = tea.KeyPressMsg{}
	for b.Loop() {
		c.Count(msg)
	}
}

func BenchmarkCountEnabled(b *testing.B) {
	c := New()
	c.SetEnabled(true)
	var msg tea.Msg = tea.KeyPressMsg{}
	for b.Loop() {
		c.Count(msg)
	}
}

func TestPackageLevelHooksFollowTheDefaultCollector(t *testing.T) {
	t.Cleanup(Reset)
	Reset()
	if Enabled() {
		t.Fatal("the default collector starts enabled")
	}
	Count(tea.KeyPressMsg{}) // must be a no-op while off
	SetEnabled(true)
	Count(tea.KeyPressMsg{})
	RecordFrame(time.Millisecond)
	RecordPane("editor", time.Millisecond)
	s := Collect(1)
	if s.Msgs != 1 || s.Frames != 1 || len(s.Panes) != 1 {
		t.Fatalf("default collector sample = %+v", s)
	}
	if len(History()) != 1 {
		t.Fatalf("history = %d, want 1", len(History()))
	}
}
