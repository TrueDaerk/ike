package editor

import (
	"strings"
	"testing"

	"ike/internal/cronhint"
	"ike/internal/editor/buffer"
	"ike/internal/highlight"
	"ike/internal/host"
)

// cronhints_test.go covers the editor half of the cron hints (#1624): the
// hint rides the stand-in channel, so it draws off the caret, disappears while
// the expression is edited, and switches on editor.cron_hints only. The span
// is synthetic; the parsing and context detection live in internal/cronhint.

// cronHinted loads a buffer whose first line is a crontab job and delivers the
// hint span for its schedule, cols [0,9).
func cronHinted(t *testing.T) Model {
	t.Helper()
	m, path := mdLoaded(t, "0 3 * * 1 /usr/bin/backup.sh\nplain\n")
	m.cursor = buffer.Position{Line: 1}
	spans := []highlight.Span{{
		Line: 0, StartCol: 0, EndCol: 9,
		Capture: cronhint.Capture, Replace: "0 3 * * 1" + cronhint.Gap + "Mon 03:00",
	}}
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: spans})
	return mm
}

// TestCronHintRenders: off the caret line the expression draws with its
// English schedule appended.
func TestCronHintRenders(t *testing.T) {
	m := cronHinted(t)
	view := plainView(m)
	if !strings.Contains(view, "0 3 * * 1") {
		t.Error("the expression itself must stay visible under the hint")
	}
	if !strings.Contains(view, "Mon 03:00") {
		t.Errorf("hint not rendered, view:\n%s", view)
	}
}

// TestCronHintCaretReveals (#1594 mechanic): the caret inside the expression
// drops the hint, so the buffer text is what is edited.
func TestCronHintCaretReveals(t *testing.T) {
	m := cronHinted(t)
	m.cursor = buffer.Position{Line: 0, Col: 2}
	if view := plainView(m); strings.Contains(view, "Mon 03:00") {
		t.Error("the caret inside the expression must hide the hint")
	}
}

// TestCronHintToggle: view.toggleCronHints switches the family off and back
// on, independently of the other stand-in families.
func TestCronHintToggle(t *testing.T) {
	m := cronHinted(t)
	m, _ = m.Update(ActionMsg{Action: "toggle_cron_hints"})
	if view := plainView(m); strings.Contains(view, "Mon 03:00") {
		t.Error("toggle off must drop the hint")
	}
	m, _ = m.Update(ActionMsg{Action: "toggle_cron_hints"})
	if view := plainView(m); !strings.Contains(view, "Mon 03:00") {
		t.Error("toggling back on must restore the hint")
	}
}

// TestCronHintConfigDefault: editor.cron_hints drives the initial state, and a
// view toggle overrides it from then on — like the #64 toggles.
func TestCronHintConfigDefault(t *testing.T) {
	m := cronHinted(t)
	m.Configure(host.MapConfig{"editor.cron_hints": "false"})
	if view := plainView(m); strings.Contains(view, "Mon 03:00") {
		t.Error("editor.cron_hints=false must hide the hint")
	}
	m, _ = m.Update(ActionMsg{Action: "toggle_cron_hints"})
	if view := plainView(m); !strings.Contains(view, "Mon 03:00") {
		t.Error("the view toggle must win over the config default")
	}
	m, _ = m.Update(ActionMsg{Action: "noop"})
	if view := plainView(m); !strings.Contains(view, "Mon 03:00") {
		t.Error("config refresh clobbered the cron-hints toggle")
	}
}

// TestCronHintIndependentOfOtherToggles: the hint rides its own channel —
// neither the markdown nor the timestamp switch reaches it.
func TestCronHintIndependentOfOtherToggles(t *testing.T) {
	m := cronHinted(t)
	m, _ = m.Update(ActionMsg{Action: "toggle_markdown_rendering"})
	m, _ = m.Update(ActionMsg{Action: "toggle_timestamp_decoding"})
	if view := plainView(m); !strings.Contains(view, "Mon 03:00") {
		t.Error("the markdown/timestamp toggles must not gate the cron hints")
	}
}
