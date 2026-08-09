package editor

import (
	"fmt"
	"strings"
	"testing"

	"ike/internal/editor/buffer"
)

// deltaDoc is a service log with a one-second cadence and one stall in the
// middle; the lines are short so the hints fit the 60-column test view.
const deltaDoc = `10:11:10 INFO a
10:11:11 INFO b
10:11:12 INFO c
10:11:42 WARN stalled
10:11:43 INFO d
`

// TestLogDeltaHints: each line past the first carries its elapsed-time hint,
// and the first one — nothing to measure against — carries none.
func TestLogDeltaHints(t *testing.T) {
	m := logLoaded(t, deltaDoc)
	m.cursor = buffer.Position{Line: 0}

	for l, want := range map[int]string{1: "+1s", 2: "+1s", 3: "+30s", 4: "+1s"} {
		if got, _, ok := m.logDeltaAt(l); !ok || got != want {
			t.Errorf("logDeltaAt(%d) = %q, %v; want %q, true", l, got, ok, want)
		}
	}
	if _, _, ok := m.logDeltaAt(0); ok {
		t.Error("the first line has nothing to measure against")
	}
	if got := m.View(); !strings.Contains(got, "+30s") || !strings.Contains(got, "+1s") {
		t.Errorf("view must render the delta hints:\n%s", got)
	}
}

// TestLogDeltaGapEmphasized: the stall is flagged, the ordinary ticks are not.
func TestLogDeltaGapEmphasized(t *testing.T) {
	m := logLoaded(t, deltaDoc)
	if _, gap, _ := m.logDeltaAt(3); !gap {
		t.Error("a 30s stall in a 1s-cadence file must be emphasized")
	}
	for _, l := range []int{1, 2, 4} {
		if _, gap, _ := m.logDeltaAt(l); gap {
			t.Errorf("line %d is the ordinary cadence and must not be emphasized", l)
		}
	}
}

// TestLogDeltaUnstampedLine: a stack-trace frame gets no hint, and the next
// stamped line still measures from the last real timestamp.
func TestLogDeltaUnstampedLine(t *testing.T) {
	m := logLoaded(t, "10:11:10 ERROR boom\n\tat Foo.bar(Foo.java:42)\n10:11:13 INFO ok\n")
	if _, _, ok := m.logDeltaAt(1); ok {
		t.Error("a line without a timestamp must show no hint")
	}
	if got, _, ok := m.logDeltaAt(2); !ok || got != "+3s" {
		t.Errorf("logDeltaAt(2) = %q, %v; want \"+3s\", true — the chain must not break", got, ok)
	}
}

// TestLogDeltaToggleOffRendersRaw: raw mode (view.toggleLogRendering) drops
// the hints entirely.
func TestLogDeltaToggleOffRendersRaw(t *testing.T) {
	m := logLoaded(t, deltaDoc)
	m.toggleLogRendering()

	if _, _, ok := m.logDeltaAt(3); ok {
		t.Error("raw mode must report no delta")
	}
	if got := m.View(); strings.Contains(got, "+30s") {
		t.Errorf("raw mode must drop the hints:\n%s", got)
	}
}

// TestLogDeltaNonLogBufferUnaffected: the hints are a log-rendering feature.
func TestLogDeltaNonLogBufferUnaffected(t *testing.T) {
	m := New()
	m.buf = buffer.FromString("10:11:10 INFO a\n10:11:12 INFO b")
	m.path = "notes.txt"
	m.SetSize(60, 10)
	if _, _, ok := m.logDeltaAt(1); ok {
		t.Error("a text buffer must show no delta hints")
	}
}

// TestLogDeltaTooNarrowRowUnchanged: a line that fills the width keeps every
// column of its text — the hint only uses spare padding.
func TestLogDeltaTooNarrowRowUnchanged(t *testing.T) {
	long := strings.Repeat("x", 200)
	m := logLoaded(t, "10:11:10 INFO "+long+"\n10:11:12 INFO "+long+"\n")
	if got, _, ok := m.logDeltaAt(1); !ok || got != "+2s" {
		t.Fatalf("precondition: logDeltaAt(1) = %q, %v; want \"+2s\", true", got, ok)
	}
	if got := m.View(); strings.Contains(got, "+2s") {
		t.Errorf("a full-width row must not be overwritten by the hint:\n%s", got)
	}
}

// TestLogDeltaFullTextBesideScrollbar: with the buffer overflowing the
// viewport the scrollbar owns the rightmost column; the hint must move one
// column left, not lose its trailing unit — "+460ms" read as "+460m" (#1728).
func TestLogDeltaFullTextBesideScrollbar(t *testing.T) {
	var b strings.Builder
	ms := 0
	for i := 0; i < 20; i++ { // 20 lines > the 10-row test viewport
		fmt.Fprintf(&b, "10:11:%02d,%03d INFO tick\n", ms/1000, ms%1000)
		ms += 460
	}
	m := logLoaded(t, b.String())
	if _, _, _, _, ok := m.scrollbarGeometry(); !ok {
		t.Fatal("precondition: the scrollbar must be visible")
	}
	if got, _, ok := m.logDeltaAt(1); !ok || got != "+460ms" {
		t.Fatalf("precondition: logDeltaAt(1) = %q, %v; want \"+460ms\", true", got, ok)
	}
	if got := m.View(); !strings.Contains(got, "+460ms") {
		t.Errorf("the hint must keep its full text beside the scrollbar:\n%s", got)
	}
}

// TestLogDeltasFollowEdits: the cache is keyed by document version, so editing
// a timestamp recomputes the chain.
func TestLogDeltasFollowEdits(t *testing.T) {
	m := logLoaded(t, deltaDoc)
	if got, _, _ := m.logDeltaAt(1); got != "+1s" {
		t.Fatalf("precondition: logDeltaAt(1) = %q; want \"+1s\"", got)
	}

	lines := m.buf.Lines()
	lines[1] = "10:11:15 INFO b"
	m.buf.ReplaceAll(strings.Join(lines, "\n"))
	m.docVersion++
	if got, _, _ := m.logDeltaAt(1); got != "+5s" {
		t.Errorf("logDeltaAt(1) = %q after the edit; want \"+5s\"", got)
	}
}
