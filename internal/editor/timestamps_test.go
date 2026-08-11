package editor

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"ike/internal/editor/buffer"
	"ike/internal/epochtime"
	"ike/internal/highlight"
	"ike/internal/host"
)

// timestamps_test.go covers the editor half of the inline epoch decoding
// (#1618): the stand-in renders off the caret, the raw digits come back under
// it, and the switch is the timestamp toggle alone — not the markdown one.
// The spans are synthetic; the detection heuristics live in internal/epochtime.

// stampSpan is a decoded epoch conceal over "1722945600" at cols [3, 13).
func stampSpan(line int) []highlight.Span {
	return []highlight.Span{
		{Line: line, StartCol: 3, EndCol: 13, Capture: epochtime.Capture, Replace: "2024-08-06 12:00:00Z"},
	}
}

// stamped loads a buffer holding one epoch number and delivers stampSpan.
func stamped(t *testing.T) Model {
	t.Helper()
	m, path := mdLoaded(t, "ts 1722945600 end\nplain\n")
	m.cursor = buffer.Position{Line: 1}
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: stampSpan(0)})
	return mm
}

// TestTimestampRendersDecoded: off the caret line the raw number displays as
// its UTC form, and the span still styles the raw source.
func TestTimestampRendersDecoded(t *testing.T) {
	m := stamped(t)
	view := plainView(m)
	if strings.Contains(view, "1722945600") {
		t.Error("raw epoch number still visible off the caret line")
	}
	if !strings.Contains(view, "2024-08-06 12:00:00Z") {
		t.Errorf("decoded stand-in not rendered, view:\n%s", view)
	}
	if got := m.hlIndex.CaptureAt(0, 3); got != epochtime.Capture {
		t.Errorf("style capture at the raw range = %q, want %q", got, epochtime.Capture)
	}
}

// TestTimestampCaretRevealsRaw (#1594 mechanic): the raw number shows while
// the caret sits inside the range, and only then.
func TestTimestampCaretRevealsRaw(t *testing.T) {
	m := stamped(t)
	m.cursor = buffer.Position{Line: 0, Col: 0}
	if view := plainView(m); strings.Contains(view, "1722945600") {
		t.Error("caret outside the range must keep the decoded form")
	}
	m.cursor = buffer.Position{Line: 0, Col: 5}
	view := plainView(m)
	if !strings.Contains(view, "1722945600") {
		t.Error("caret inside the range must show the raw number")
	}
	if strings.Contains(view, "2024-08-06") {
		t.Error("the revealed range must not render the stand-in too")
	}
}

// TestTimestampAdjacentCaretRevealsRaw (#1686): the epoch number is a value
// conceal, so the columns directly before ([3,13) starts at 3) and directly
// after it reveal the raw digits as well — one column out, no further.
func TestTimestampAdjacentCaretRevealsRaw(t *testing.T) {
	for _, tc := range []struct {
		name string
		col  int
		raw  bool
	}{
		{"directly before", 2, true},
		{"directly after", 13, true},
		{"two before", 1, false},
		{"two after", 14, false},
	} {
		m := stamped(t)
		m.cursor = buffer.Position{Line: 0, Col: tc.col}
		view := plainView(m)
		if got := strings.Contains(view, "1722945600"); got != tc.raw {
			t.Errorf("caret %s (col %d): raw digits shown = %v, want %v, view:\n%s",
				tc.name, tc.col, got, tc.raw, view)
		}
	}
}

// TestTimestampToggle: view.toggleTimestampDecoding switches the layer off —
// and back on — for this view.
func TestTimestampToggle(t *testing.T) {
	m := stamped(t)
	m, _ = m.Update(ActionMsg{Action: "toggle_timestamp_decoding"})
	if view := plainView(m); !strings.Contains(view, "1722945600") {
		t.Errorf("toggling decoding off must show the raw number, view:\n%s", view)
	}
	m, _ = m.Update(ActionMsg{Action: "toggle_timestamp_decoding"})
	if view := plainView(m); !strings.Contains(view, "2024-08-06 12:00:00Z") {
		t.Error("toggling decoding back on must restore the decoded form")
	}
}

// TestTimestampIndependentOfMarkdownToggle: the stamps ride their own conceal
// channel, so the markdown rendering switch does not reach them (and the
// markdown chrome does not follow the timestamp switch).
func TestTimestampIndependentOfMarkdownToggle(t *testing.T) {
	m := stamped(t)
	m, _ = m.Update(ActionMsg{Action: "toggle_markdown_rendering"}) // markdown rendering off
	if view := plainView(m); !strings.Contains(view, "2024-08-06 12:00:00Z") {
		t.Error("the markdown toggle must not gate timestamp decoding")
	}

	m, path := mdLoaded(t, "**bold** x\nplain\n")
	m.cursor = buffer.Position{Line: 1}
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: concealSpans(0)})
	m = mm
	m, _ = m.Update(ActionMsg{Action: "toggle_timestamp_decoding"}) // timestamp decoding off
	if view := plainView(m); strings.Contains(view, "**bold**") {
		t.Error("the timestamp toggle must not gate the markdown conceal")
	}
}

// TestTimestampProportionalClickMap (#1782): a click inside the epoch
// stand-in maps proportionally into the raw digits — the first stand-in cell
// lands on the range start, the last cell on the range's last raw column, and
// offsets in between land monotonically in-between, not always at the start.
func TestTimestampProportionalClickMap(t *testing.T) {
	m := stamped(t)
	// Raw range is [3,13) — 10 digits; stand-in "2024-08-06 12:00:00Z" is 20
	// cells. Offset 0 is 'ts ' has been consumed by `from`; displayClickCol
	// counts offset from the line start here (from=0), so account for "ts ".
	prefix := lipgloss.Width("ts ")
	standInCells := lipgloss.Width("2024-08-06 12:00:00Z")

	last := -1
	for offset := 0; offset < standInCells; offset++ {
		got := m.displayClickCol(0, 0, prefix+offset)
		if got < 3 || got > 12 {
			t.Fatalf("offset %d -> col %d, want within [3,12]", offset, got)
		}
		if got < last {
			t.Errorf("offset %d -> col %d, want >= previous col %d (monotonic)", offset, got, last)
		}
		last = got
	}
	if got := m.displayClickCol(0, 0, prefix+0); got != 3 {
		t.Errorf("first stand-in cell -> col %d, want 3 (range start)", got)
	}
	if got := m.displayClickCol(0, 0, prefix+standInCells-1); got != 12 {
		t.Errorf("last stand-in cell -> col %d, want 12 (last raw column)", got)
	}
}

// TestTimestampConfigDefault: editor.timestamp_decoding drives the initial
// state, and a view toggle overrides it from then on — like the #64 toggles.
func TestTimestampConfigDefault(t *testing.T) {
	m := stamped(t)
	m.Configure(host.MapConfig{"editor.timestamp_decoding": "false"})
	if view := plainView(m); !strings.Contains(view, "1722945600") {
		t.Errorf("editor.timestamp_decoding=false must show raw numbers, view:\n%s", view)
	}
	m, _ = m.Update(ActionMsg{Action: "toggle_timestamp_decoding"})
	if view := plainView(m); !strings.Contains(view, "2024-08-06 12:00:00Z") {
		t.Error("the view toggle must win over the config default")
	}
	// The per-Update config refresh must not clobber the override.
	m, _ = m.Update(ActionMsg{Action: "noop"})
	if view := plainView(m); !strings.Contains(view, "2024-08-06 12:00:00Z") {
		t.Error("config refresh clobbered the timestamp-decoding toggle")
	}
}
