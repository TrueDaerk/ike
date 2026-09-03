package httppane

import (
	"strings"
	"testing"
	"time"

	"ike/internal/httpclient"
)

// timed returns the sample response with a phase breakdown attached (#2404).
func timed() *httpclient.Response {
	resp := sample()
	resp.Timing = &httpclient.Timing{
		DNS:      2 * time.Millisecond,
		Connect:  11 * time.Millisecond,
		TLS:      34 * time.Millisecond,
		TTFB:     210 * time.Millisecond,
		Transfer: 4 * time.Millisecond,
	}
	return resp
}

// TestTimingRowUnderStatus: the breakdown composes as its own row directly
// beneath the status line, so "slow" and "slow where" are read together.
func TestTimingRowUnderStatus(t *testing.T) {
	m := New(nil)
	m.SetSize(100, 24)
	m.Set("create", timed())

	rows := m.rows
	if len(rows) < 2 || rows[0].kind != kindStatus {
		t.Fatalf("first row is not the status line: %+v", rows)
	}
	if rows[1].kind != kindTiming {
		t.Fatalf("second row = %v, want the timing row", rows[1].kind)
	}
	want := "dns 2ms · connect 11ms · tls 34ms · ttfb 210ms · transfer 4ms"
	if rows[1].text != want {
		t.Fatalf("timing row = %q, want %q", rows[1].text, want)
	}
	if view := m.View(); !strings.Contains(view, "ttfb 210ms") {
		t.Fatalf("timing line missing from the view:\n%s", view)
	}
}

// TestTimingRowAbsentWithoutBreakdown: a response restored from a history
// file written before #2404 renders no row at all, not a row of zeros.
func TestTimingRowAbsentWithoutBreakdown(t *testing.T) {
	m := New(nil)
	m.SetSize(100, 24)
	m.Set("create", sample())
	for _, r := range m.rows {
		if r.kind == kindTiming {
			t.Fatalf("timing row composed without a breakdown: %q", r.text)
		}
	}
}

// TestTimingRowInHeadersCopy: "Y" copies the status block, and the breakdown
// is part of what a user pastes into a bug report.
func TestTimingRowInHeadersCopy(t *testing.T) {
	m := New(nil)
	m.SetSize(100, 24)
	m.Set("create", timed())
	if got := m.HeadersText(); !strings.Contains(got, "ttfb 210ms") {
		t.Fatalf("headers copy misses the breakdown:\n%s", got)
	}
}

// TestTimingSurvivesHistoryBrowsing: the breakdown travels with the stored
// entry, so an older response shows its own numbers (#2404).
func TestTimingSurvivesHistoryBrowsing(t *testing.T) {
	older := sample()
	older.Timing = &httpclient.Timing{TTFB: 900 * time.Millisecond, Reused: true}

	m := New(nil)
	m.SetSize(100, 24)
	m.Set("create", timed())
	m.SetHistory([]HistoryItem{{Resp: timed()}, {Resp: older}})
	m.showHistory(1)
	view := m.View()
	if !strings.Contains(view, "conn reused") || !strings.Contains(view, "ttfb 900ms") {
		t.Fatalf("older entry's breakdown missing:\n%s", view)
	}
}

// TestCancelHintAppearsAfterOneSecond: a young flight shows no hint (the
// median request is gone before it would help), a second-old one names both
// the pane key and the bound chord.
func TestCancelHintAppearsAfterOneSecond(t *testing.T) {
	m := New(nil)
	m.SetSize(100, 24)
	m.Set("create", sample())
	m.SetCancelChord("ctrl+.")

	m.SetPending("create", time.Now())
	if hint := m.cancelHint(); hint != "" {
		t.Fatalf("hint shown immediately: %q", hint)
	}
	if view := m.View(); !strings.Contains(view, "⟳ running create") {
		t.Fatalf("elapsed-time marker missing while in flight:\n%s", view)
	}

	m.SetPending("create", time.Now().Add(-2*time.Second))
	if got, want := m.cancelHint(), " x / ctrl+. cancels"; got != want {
		t.Fatalf("hint = %q, want %q", got, want)
	}
	if view := m.View(); !strings.Contains(view, "x / ctrl+. cancels") {
		t.Fatalf("hint missing from the view:\n%s", view)
	}

	m.ClearPending()
	if hint := m.cancelHint(); hint != "" {
		t.Fatalf("hint outlived the flight: %q", hint)
	}
}

// TestCancelHintWithoutChord: an unbound http.cancel leaves the hint naming
// the pane key alone rather than inventing a chord.
func TestCancelHintWithoutChord(t *testing.T) {
	m := New(nil)
	m.SetSize(100, 24)
	m.SetPending("create", time.Now().Add(-2*time.Second))
	if got, want := m.cancelHint(), " x cancels"; got != want {
		t.Fatalf("hint = %q, want %q", got, want)
	}
}

// TestCancelHintCountsAsHeaderLine: the hint is a fixed line, so the body
// viewport must shrink by exactly one row while it is up — otherwise the last
// body row would be drawn past the pane's height.
func TestCancelHintCountsAsHeaderLine(t *testing.T) {
	m := New(nil)
	m.SetSize(100, 24)
	m.Set("create", timed())
	before, wasTall := m.bodyHeight(), strings.Count(m.View(), "\n")
	m.SetPending("create", time.Now().Add(-2*time.Second))
	if got := m.bodyHeight(); got != before-1 {
		t.Fatalf("bodyHeight = %d, want %d (one line for the hint)", got, before-1)
	}
	if lines := strings.Count(m.View(), "\n"); lines != wasTall {
		t.Fatalf("view is %d lines tall with the hint, want %d — it must not overflow the pane", lines, wasTall)
	}
}

// TestCancelHintDuringStream: a live stream is a flight too, and it is the
// one most likely to need stopping.
func TestCancelHintDuringStream(t *testing.T) {
	m := New(nil)
	m.SetSize(100, 24)
	m.SetCancelChord("cmd+.")
	m.StartStream("feed", "HTTP/1.1", "200 OK", nil)
	m.SetPending("feed", time.Now().Add(-3*time.Second))
	m.ClearPending()
	m.pendingSince = time.Now().Add(-3 * time.Second)
	if got, want := m.cancelHint(), " x / cmd+. cancels"; got != want {
		t.Fatalf("hint = %q, want %q", got, want)
	}
}
