package editor

import (
	"strings"
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/highlight"
	"ike/internal/host"
	"ike/internal/permhint"
)

// permhints_test.go covers the editor half of the permission hints (#1656): the
// hint rides the stand-in channel, so it draws off the caret, disappears while
// the literal is edited, and switches on editor.permission_hints only. The span
// is synthetic; decoding and context detection live in internal/permhint.

// permHinted loads a buffer whose first line is a chmod call and delivers the
// hint span for its mode, cols [6,9).
func permHinted(t *testing.T) Model {
	t.Helper()
	m, path := mdLoaded(t, "chmod 755 build.sh\nplain\n")
	m.cursor = buffer.Position{Line: 1}
	spans := []highlight.Span{{
		Line: 0, StartCol: 6, EndCol: 9,
		Capture: permhint.Capture, Replace: "755" + permhint.Gap + "rwxr-xr-x",
	}}
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: spans})
	return mm
}

// TestPermHintRenders: off the caret line the literal draws with its symbolic
// form appended.
func TestPermHintRenders(t *testing.T) {
	m := permHinted(t)
	view := plainView(m)
	if !strings.Contains(view, "755") {
		t.Error("the literal itself must stay visible under the hint")
	}
	if !strings.Contains(view, "rwxr-xr-x") {
		t.Errorf("hint not rendered, view:\n%s", view)
	}
}

// TestPermHintCaretReveals (#1594 mechanic): the caret inside the literal drops
// the hint, so the buffer text is what is edited.
func TestPermHintCaretReveals(t *testing.T) {
	m := permHinted(t)
	m.cursor = buffer.Position{Line: 0, Col: 7}
	if view := plainView(m); strings.Contains(view, "rwxr-xr-x") {
		t.Error("the caret inside the literal must hide the hint")
	}
}

// TestPermHintToggle: view.togglePermissionHints switches the family off and
// back on.
func TestPermHintToggle(t *testing.T) {
	m := permHinted(t)
	m, _ = m.Update(ActionMsg{Action: "toggle_permission_hints"})
	if view := plainView(m); strings.Contains(view, "rwxr-xr-x") {
		t.Error("toggle off must drop the hint")
	}
	m, _ = m.Update(ActionMsg{Action: "toggle_permission_hints"})
	if view := plainView(m); !strings.Contains(view, "rwxr-xr-x") {
		t.Error("toggling back on must restore the hint")
	}
}

// TestPermHintConfigDefault: editor.permission_hints drives the initial state,
// and a view toggle overrides it from then on — like the #64 toggles.
func TestPermHintConfigDefault(t *testing.T) {
	m := permHinted(t)
	m.Configure(host.MapConfig{"editor.permission_hints": "false"})
	if view := plainView(m); strings.Contains(view, "rwxr-xr-x") {
		t.Error("editor.permission_hints=false must hide the hint")
	}
	m, _ = m.Update(ActionMsg{Action: "toggle_permission_hints"})
	if view := plainView(m); !strings.Contains(view, "rwxr-xr-x") {
		t.Error("the view toggle must win over the config default")
	}
	m, _ = m.Update(ActionMsg{Action: "noop"})
	if view := plainView(m); !strings.Contains(view, "rwxr-xr-x") {
		t.Error("config refresh clobbered the permission-hints toggle")
	}
}

// TestPermHintIndependentOfOtherToggles: the hint rides its own channel —
// neither the radix nor the cron switch reaches it.
func TestPermHintIndependentOfOtherToggles(t *testing.T) {
	m := permHinted(t)
	m, _ = m.Update(ActionMsg{Action: "toggle_radix_hints"})
	m, _ = m.Update(ActionMsg{Action: "toggle_cron_hints"})
	if view := plainView(m); !strings.Contains(view, "rwxr-xr-x") {
		t.Error("the radix/cron toggles must not gate the permission hints")
	}
}
