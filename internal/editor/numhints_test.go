package editor

import (
	"strings"
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/highlight"
	"ike/internal/host"
	"ike/internal/numhint"
)

// numhints_test.go covers the editor half of the number-readability hints
// (#1627): each family rides its own stand-in channel, so it draws off the
// caret, disappears while the literal is edited, and switches on its own
// toggle. The spans are synthetic; the heuristics and formatting live in
// internal/numhint.

// numHinted loads a two-line buffer whose first line is `max_size: 10485760`
// and delivers a stand-in span of the given family over the literal, cols
// [10, 18).
func numHinted(t *testing.T, capture, replace string) Model {
	t.Helper()
	m, path := mdLoaded(t, "max_size: 10485760\nplain\n")
	m.cursor = buffer.Position{Line: 1}
	spans := []highlight.Span{{
		Line: 0, StartCol: 10, EndCol: 18, Capture: capture, Replace: replace,
	}}
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: spans})
	return mm
}

// families pairs each capture with the stand-in it renders and the action that
// toggles it.
var families = []struct {
	name    string
	capture string
	replace string
	action  string
	cfgKey  string
}{
	{"size", numhint.SizeCapture, "10 MiB", "toggle_byte_size_hints", "editor.byte_size_hints"},
	{"duration", numhint.DurationCapture, "24h", "toggle_duration_hints", "editor.duration_hints"},
	{"group", numhint.GroupCapture, "10_485_760", "toggle_digit_grouping", "editor.digit_grouping"},
	{"radix", numhint.RadixCapture, "10485760" + numhint.Gap + "= 0xA00000", "toggle_radix_hints", "editor.radix_hints"},
}

// TestNumberHintRenders: off the caret line the literal draws as its hint.
func TestNumberHintRenders(t *testing.T) {
	for _, f := range families {
		m := numHinted(t, f.capture, f.replace)
		if view := plainView(m); !strings.Contains(view, f.replace) {
			t.Errorf("%s hint not rendered, view:\n%s", f.name, view)
		}
	}
}

// TestNumberHintCaretReveals (#1594 mechanic): the caret inside the literal
// drops the hint, so the raw digits are what is edited.
func TestNumberHintCaretReveals(t *testing.T) {
	for _, f := range families {
		m := numHinted(t, f.capture, f.replace)
		m.cursor = buffer.Position{Line: 0, Col: 12}
		view := plainView(m)
		if strings.Contains(view, f.replace) {
			t.Errorf("%s: the caret inside the literal must hide the hint", f.name)
		}
		if !strings.Contains(view, "10485760") {
			t.Errorf("%s: revealed line must show the raw digits, view:\n%s", f.name, view)
		}
	}
}

// TestNumberHintToggles: each family switches on its own action, and the other
// families' actions do not reach it.
func TestNumberHintToggles(t *testing.T) {
	for _, f := range families {
		m := numHinted(t, f.capture, f.replace)
		m, _ = m.Update(ActionMsg{Action: f.action})
		if view := plainView(m); strings.Contains(view, f.replace) {
			t.Errorf("%s: toggle off must drop the hint", f.name)
		}
		m, _ = m.Update(ActionMsg{Action: f.action})
		if view := plainView(m); !strings.Contains(view, f.replace) {
			t.Errorf("%s: toggling back on must restore the hint", f.name)
		}
		for _, other := range families {
			if other.action == f.action {
				continue
			}
			m, _ = m.Update(ActionMsg{Action: other.action})
		}
		if view := plainView(m); !strings.Contains(view, f.replace) {
			t.Errorf("%s: another family's toggle must not gate it", f.name)
		}
	}
}

// TestNumberHintConfigDefaults: each config key drives the initial state, and a
// view toggle overrides it from then on — like the #64 toggles.
func TestNumberHintConfigDefaults(t *testing.T) {
	for _, f := range families {
		m := numHinted(t, f.capture, f.replace)
		m.Configure(host.MapConfig{f.cfgKey: "false"})
		if view := plainView(m); strings.Contains(view, f.replace) {
			t.Errorf("%s=false must hide the hint", f.cfgKey)
		}
		m, _ = m.Update(ActionMsg{Action: f.action})
		if view := plainView(m); !strings.Contains(view, f.replace) {
			t.Errorf("%s: the view toggle must win over the config default", f.name)
		}
		m, _ = m.Update(ActionMsg{Action: "noop"})
		if view := plainView(m); !strings.Contains(view, f.replace) {
			t.Errorf("%s: config refresh clobbered the toggle", f.name)
		}
	}
}
