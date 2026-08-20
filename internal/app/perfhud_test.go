package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/perfhud"
)

// perfhud_test.go covers the app half of the performance HUD (#1999): the
// toggle and its measurement hooks, the overlay, the clipboard snapshot, and
// the acceptance criterion that a hidden HUD measures nothing at all.

// hudOff resets the process-wide collector before and after a test, so a HUD
// left on cannot leak measurement into unrelated tests.
func hudOff(t *testing.T) {
	t.Helper()
	perfhud.Reset()
	t.Cleanup(perfhud.Reset)
}

func TestPerfHUDToggleStartsAndStopsCollection(t *testing.T) {
	hudOff(t)
	m := sized(t, 100, 30)

	out, cmd := m.Update(TogglePerfHUDMsg{})
	m = out.(Model)
	if !perfhud.Enabled() {
		t.Fatal("perf.hud did not enable collection")
	}
	if cmd == nil {
		t.Fatal("perf.hud did not arm the sampling tick")
	}
	if !m.perfTickArmed {
		t.Error("perfTickArmed = false after opening the HUD")
	}

	out, _ = m.Update(TogglePerfHUDMsg{})
	m = out.(Model)
	if perfhud.Enabled() {
		t.Fatal("perf.hud did not disable collection")
	}
	// A tick still in flight from the open phase must not re-arm.
	out, cmd = m.Update(perfTickMsg{})
	m = out.(Model)
	if cmd != nil || m.perfTickArmed {
		t.Error("the sampling tick re-armed after the HUD closed")
	}
}

func TestPerfHUDCountsMessagesAndAttributesPanes(t *testing.T) {
	hudOff(t)
	m := sized(t, 120, 40)
	out, _ := m.Update(TogglePerfHUDMsg{})
	m = out.(Model)

	out, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = out.(Model)
	m.render() // one full frame: chrome plus every visible pane

	s := perfhud.Collect(m.armedTimers())
	if s.Msgs == 0 {
		t.Fatal("no messages counted with the HUD on")
	}
	if s.Rates[perfhud.CatKey] == 0 {
		t.Error("the key press was not counted as input")
	}
	if s.Frames != 1 {
		t.Errorf("Frames = %d, want the one composed frame", s.Frames)
	}
	if len(s.Panes) == 0 {
		t.Fatal("no per-pane render cost attributed")
	}
	keys := map[string]bool{}
	for _, p := range s.Panes {
		keys[p.Key] = true
		if p.Frames != 1 {
			t.Errorf("pane %q rendered in %d frames, want 1", p.Key, p.Frames)
		}
	}
	for _, want := range []string{"explorer", "editor"} {
		if !keys[want] {
			t.Errorf("pane %q missing from the attribution: %v", want, keys)
		}
	}
}

func TestPerfHUDOffMeasuresNothing(t *testing.T) {
	hudOff(t)
	m := sized(t, 100, 30)
	out, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = out.(Model)
	m.render()

	if _, ok := perfhud.Latest(); ok {
		t.Fatal("a hidden HUD produced a sample")
	}
	s := perfhud.Collect(0)
	if s.Msgs != 0 || s.Frames != 0 || len(s.Panes) != 0 {
		t.Fatalf("a hidden HUD recorded %+v", s)
	}
	if s.Live {
		t.Error("Live = true with the HUD hidden")
	}
}

func TestPerfHUDOverlayAppearsAfterTheFirstSample(t *testing.T) {
	hudOff(t)
	m := sized(t, 120, 40)
	out, _ := m.Update(TogglePerfHUDMsg{})
	m = out.(Model)

	// Before the first sample there is nothing to show yet.
	if strings.Contains(m.render(), "PERF HUD") {
		t.Fatal("the HUD box drew before its first sample")
	}
	out, cmd := m.Update(perfTickMsg{})
	m = out.(Model)
	if cmd == nil {
		t.Error("the sampling tick did not re-arm while the HUD is open")
	}
	frame := m.render()
	if !strings.Contains(frame, "PERF HUD") {
		t.Fatalf("the HUD box is missing from the frame:\n%s", frame)
	}
	if !strings.Contains(frame, "goroutines") && !strings.Contains(frame, "go ") {
		t.Errorf("the HUD box lost its runtime gauges:\n%s", frame)
	}
	// Closing it takes the box away again.
	out, _ = m.Update(TogglePerfHUDMsg{})
	m = out.(Model)
	if strings.Contains(m.render(), "PERF HUD") {
		t.Fatal("the HUD box survived the close")
	}
}

// TestPerfHUDBoxIsLaidOutPerSampleNotPerFrame pins the other half of "cheap":
// the box's numbers only change once per sample, so composing it belongs on
// the tick, not in every frame the user's typing produces.
func TestPerfHUDBoxIsLaidOutPerSampleNotPerFrame(t *testing.T) {
	hudOff(t)
	m := sized(t, 120, 40)
	out, _ := m.Update(TogglePerfHUDMsg{})
	m = out.(Model)
	if m.perfBox != "" {
		t.Fatal("a box was laid out before the first sample")
	}
	out, _ = m.Update(perfTickMsg{})
	m = out.(Model)
	if m.perfBox == "" || m.perfBoxW != m.width {
		t.Fatalf("the sample did not cache a box for width %d (%q)", m.width, m.perfBox)
	}
	cached := m.perfBox
	m.render()
	m.render()
	if m.perfBox != cached {
		t.Error("rendering a frame re-laid out the HUD box")
	}
	// A resize invalidates the cache, and the frame still carries a HUD.
	out, _ = m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	m = out.(Model)
	if !strings.Contains(m.render(), "PERF HUD") {
		t.Fatal("the HUD vanished after a resize")
	}
	out, _ = m.Update(perfTickMsg{})
	m = out.(Model)
	if m.perfBoxW != 90 {
		t.Errorf("the cached box is still laid out for width %d", m.perfBoxW)
	}
	// Closing drops the cache with the HUD.
	out, _ = m.Update(TogglePerfHUDMsg{})
	m = out.(Model)
	if m.perfBox != "" {
		t.Error("the cached box outlived the HUD")
	}
}

func TestPerfSnapshotCopiesAPlainTextBlock(t *testing.T) {
	hudOff(t)
	var copied string
	orig := clipboardWrite
	clipboardWrite = func(s string) { copied = s }
	t.Cleanup(func() { clipboardWrite = orig })

	m := sized(t, 120, 40)
	out, _ := m.Update(TogglePerfHUDMsg{})
	m = out.(Model)
	m.render()
	out, _ = m.Update(perfTickMsg{})
	m = out.(Model)

	out, _ = m.Update(PerfSnapshotMsg{})
	m = out.(Model)
	for _, want := range []string{"performance snapshot", "messages:", "frames:", "goroutines", "memory:", "panes"} {
		if !strings.Contains(copied, want) {
			t.Errorf("snapshot missing %q:\n%s", want, copied)
		}
	}
	if strings.Contains(copied, "\x1b[") {
		t.Errorf("snapshot carries styling:\n%q", copied)
	}
}

func TestPerfSnapshotWorksWithTheHUDClosed(t *testing.T) {
	hudOff(t)
	var copied string
	orig := clipboardWrite
	clipboardWrite = func(s string) { copied = s }
	t.Cleanup(func() { clipboardWrite = orig })

	m := sized(t, 100, 30)
	out, _ := m.Update(PerfSnapshotMsg{})
	_ = out.(Model)
	if !strings.Contains(copied, "goroutines") {
		t.Fatalf("a closed-HUD snapshot dropped the runtime gauges:\n%s", copied)
	}
	if !strings.Contains(copied, "perf.hud") {
		t.Errorf("a closed-HUD snapshot must point at the HUD for rates:\n%s", copied)
	}
	if perfhud.Enabled() {
		t.Error("the snapshot command turned collection on")
	}
}

func TestArmedTimersCountsTheHUDsOwnTick(t *testing.T) {
	hudOff(t)
	m := sized(t, 100, 30)
	base := m.armedTimers()
	out, _ := m.Update(TogglePerfHUDMsg{})
	m = out.(Model)
	if got := m.armedTimers(); got != base+1 {
		t.Fatalf("armedTimers = %d with the HUD tick armed, want %d", got, base+1)
	}
	// The sample taken while handling the tick must still count it: it was
	// armed for the window it closes, and it re-arms immediately.
	out, _ = m.Update(perfTickMsg{})
	m = out.(Model)
	s, ok := perfhud.Latest()
	if !ok {
		t.Fatal("the tick took no sample")
	}
	if s.Timers != base+1 {
		t.Errorf("sampled timers = %d, want %d — the HUD hid its own tick", s.Timers, base+1)
	}
}

func TestPerfHUDSettingsDriveIntervalAndHistory(t *testing.T) {
	if got := perfHUDInterval(host.MapConfig{}); got != time.Second {
		t.Errorf("default interval = %s, want 1s", got)
	}
	cfg := host.MapConfig{"perf.hud_interval_ms": "250", "perf.hud_history_seconds": "30"}
	if got := perfHUDInterval(cfg); got != 250*time.Millisecond {
		t.Errorf("interval = %s, want 250ms", got)
	}
	if got := perfHUDHistory(cfg); got != 120 {
		t.Errorf("history = %d samples, want 30s / 250ms = 120", got)
	}
	// A long window at a fast interval stays bounded, a short one keeps
	// enough samples for a sparkline.
	if got := perfHUDHistory(host.MapConfig{"perf.hud_interval_ms": "100", "perf.hud_history_seconds": "600"}); got != 600 {
		t.Errorf("history = %d, want the 600-sample cap", got)
	}
	if got := perfHUDHistory(host.MapConfig{"perf.hud_interval_ms": "10000", "perf.hud_history_seconds": "10"}); got != 8 {
		t.Errorf("history = %d, want the 8-sample floor", got)
	}
	// Garbage falls back to the defaults instead of a zero interval.
	if got := perfHUDInterval(host.MapConfig{"perf.hud_interval_ms": "nonsense"}); got != time.Second {
		t.Errorf("interval = %s for a bad value, want the 1s default", got)
	}
}
