package editor

import (
	"strings"
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/escapes"
	"ike/internal/highlight"
	"ike/internal/host"
)

// escapes_test.go covers the editor half of the escaped-text decoding
// (#1620): each family's stand-in renders off the caret, the raw bytes come
// back under it, and each family switches on its own toggle only. The spans
// are synthetic; the detection heuristics live in internal/escapes.

// escaped loads a buffer with one raw escape per family and delivers one
// stand-in span per family, so cross-family independence is observable.
func escaped(t *testing.T) Model {
	t.Helper()
	// Line 0: cols [0,6) a unicode escape, [8,14) an entity, [16,24) base64.
	m, path := mdLoaded(t, "\\u00e4  &auml;  YWRtaW4=\nplain\n")
	m.cursor = buffer.Position{Line: 1}
	spans := []highlight.Span{
		{Line: 0, StartCol: 0, EndCol: 6, Capture: escapes.UnicodeCapture, Replace: "ä"},
		{Line: 0, StartCol: 8, EndCol: 14, Capture: escapes.EntityCapture, Replace: "Ä"},
		{Line: 0, StartCol: 16, EndCol: 24, Capture: escapes.Base64Capture, Replace: "admin"},
	}
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: spans})
	return mm
}

// TestEscapesRenderDecoded: off the caret line every family renders its
// stand-in, and the spans still style the raw source.
func TestEscapesRenderDecoded(t *testing.T) {
	m := escaped(t)
	view := plainView(m)
	for _, raw := range []string{"\\u00e4", "&auml;", "YWRtaW4="} {
		if strings.Contains(view, raw) {
			t.Errorf("raw %q still visible off the caret line", raw)
		}
	}
	for _, decoded := range []string{"ä", "Ä", "admin"} {
		if !strings.Contains(view, decoded) {
			t.Errorf("decoded stand-in %q not rendered, view:\n%s", decoded, view)
		}
	}
	if got := m.hlIndex.CaptureAt(0, 0); got != escapes.UnicodeCapture {
		t.Errorf("style capture at the raw range = %q, want %q", got, escapes.UnicodeCapture)
	}
}

// TestEscapesCaretRevealsRaw (#1594 mechanic): the caret inside one span
// reveals exactly that span's raw bytes; the other families stay decoded.
func TestEscapesCaretRevealsRaw(t *testing.T) {
	m := escaped(t)
	m.cursor = buffer.Position{Line: 0, Col: 9} // inside the entity span
	view := plainView(m)
	if !strings.Contains(view, "&auml;") {
		t.Error("caret inside the entity span must show the raw reference")
	}
	if strings.Contains(view, "Ä") {
		t.Error("the revealed range must not render the stand-in too")
	}
	if !strings.Contains(view, "ä") || !strings.Contains(view, "admin") {
		t.Error("the caret must not reveal the other families' spans")
	}
}

// TestEscapesPerFamilyToggle: each view.toggle*Decoding action switches only
// its own family raw.
func TestEscapesPerFamilyToggle(t *testing.T) {
	cases := []struct {
		action  string
		raw     string
		decoded string
	}{
		{"toggle_unicode_escape_decoding", "\\u00e4", "ä"},
		{"toggle_entity_decoding", "&auml;", "Ä"},
		{"toggle_base64_decoding", "YWRtaW4=", "admin"},
	}
	for _, c := range cases {
		m := escaped(t)
		m, _ = m.Update(ActionMsg{Action: c.action})
		view := plainView(m)
		if !strings.Contains(view, c.raw) {
			t.Errorf("%s off must show %q, view:\n%s", c.action, c.raw, view)
		}
		for _, other := range cases {
			if other.action != c.action && !strings.Contains(view, other.decoded) {
				t.Errorf("%s must not switch %q raw", c.action, other.raw)
			}
		}
		m, _ = m.Update(ActionMsg{Action: c.action})
		if view := plainView(m); !strings.Contains(view, c.decoded) {
			t.Errorf("toggling %s back on must restore %q", c.action, c.decoded)
		}
	}
}

// TestEscapesIndependentOfMarkdownAndTimestampToggles: the escape families
// ride their own channels — neither the markdown rendering switch nor the
// timestamp one reaches them.
func TestEscapesIndependentOfMarkdownAndTimestampToggles(t *testing.T) {
	m := escaped(t)
	m, _ = m.Update(ActionMsg{Action: "toggle_markdown_rendering"})
	m, _ = m.Update(ActionMsg{Action: "toggle_timestamp_decoding"})
	if view := plainView(m); !strings.Contains(view, "admin") || !strings.Contains(view, "ä") {
		t.Error("the markdown/timestamp toggles must not gate the escape families")
	}
}

// TestEscapesConfigDefaults: the editor.*_decoding keys drive the initial
// state, and a view toggle overrides them from then on — like the #64
// toggles.
func TestEscapesConfigDefaults(t *testing.T) {
	m := escaped(t)
	m.Configure(host.MapConfig{
		"editor.unicode_escape_decoding": "false",
		"editor.entity_decoding":         "false",
		"editor.base64_decoding":         "false",
	})
	view := plainView(m)
	for _, raw := range []string{"\\u00e4", "&auml;", "YWRtaW4="} {
		if !strings.Contains(view, raw) {
			t.Errorf("config off must show %q, view:\n%s", raw, view)
		}
	}
	m, _ = m.Update(ActionMsg{Action: "toggle_entity_decoding"})
	if view := plainView(m); !strings.Contains(view, "Ä") {
		t.Error("the view toggle must win over the config default")
	}
	// The per-Update config refresh must not clobber the override.
	m, _ = m.Update(ActionMsg{Action: "noop"})
	if view := plainView(m); !strings.Contains(view, "Ä") {
		t.Error("config refresh clobbered the entity-decoding toggle")
	}
}
