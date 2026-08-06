package editor

import (
	"strings"
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/editor/mode"
	"ike/internal/highlight"
	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/secret"
)

// secrets_test.go covers the editor half of secret masking (#1623): the
// stand-in family gated by editor.secret_masking, and the lint-note channel
// that marks duplicate keys without a language server. The spans are
// synthetic — the dotenv producer feeding them is tested in
// plugins/languages/env.

// maskSpan masks cols [6,12) of line 0 — the value of "TOKEN=abc123".
func maskSpan() []highlight.Span {
	return []highlight.Span{
		{Line: 0, StartCol: 6, EndCol: 12, Capture: secret.Capture, Replace: secret.Mask},
	}
}

// masked loads a two-line dotenv-shaped buffer with the mask span applied and
// the caret parked off the secret's line.
func masked(t *testing.T) (Model, string) {
	t.Helper()
	m, path := mdLoaded(t, "TOKEN=abc123\nPORT=80\n")
	m.cursor = buffer.Position{Line: 1}
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: maskSpan()})
	return mm, path
}

func TestSecretValueMasked(t *testing.T) {
	m, _ := masked(t)
	view := plainView(m)
	if strings.Contains(view, "abc123") {
		t.Error("the secret value is visible off the cursor line")
	}
	if !strings.Contains(view, secret.Mask) {
		t.Errorf("mask not rendered, view:\n%s", view)
	}
}

// TestSecretRevealsUnderCaret (#1594 mechanic): the value shows only while the
// caret sits inside it.
func TestSecretRevealsUnderCaret(t *testing.T) {
	m, _ := masked(t)
	m.cursor = buffer.Position{Line: 0, Col: 0} // on the key, outside the value
	if view := plainView(m); strings.Contains(view, "abc123") {
		t.Error("the caret on the key must keep the value masked")
	}
	m.cursor = buffer.Position{Line: 0, Col: 8}
	if view := plainView(m); !strings.Contains(view, "abc123") {
		t.Error("the caret inside the value must reveal it")
	}
}

// TestSecretRevealsUnderSelection: a selection over the line is a deliberate
// "show me this", like the other conceal families.
func TestSecretRevealsUnderSelection(t *testing.T) {
	m, _ := masked(t)
	m.mode = mode.Visual
	m.anchor = buffer.Position{Line: 0, Col: 0}
	if view := plainView(m); !strings.Contains(view, "abc123") {
		t.Error("a selection over the line must reveal the raw value")
	}
}

func TestToggleSecretMaskingShowsRawValues(t *testing.T) {
	m, _ := masked(t)
	m, _ = m.Update(ActionMsg{Action: "toggle_secret_masking"})
	view := plainView(m)
	if !strings.Contains(view, "abc123") {
		t.Error("masking off must show the raw value")
	}
	if strings.Contains(view, secret.Mask) {
		t.Error("masking off must not draw the mask")
	}
	m, _ = m.Update(ActionMsg{Action: "toggle_secret_masking"})
	if view := plainView(m); strings.Contains(view, "abc123") {
		t.Error("toggling back on must mask again")
	}
}

// TestSecretMaskingConfigDefault: the config default applies until the view
// toggles, then the override sticks (the #64 view-toggle rule).
func TestSecretMaskingConfigDefault(t *testing.T) {
	m, _ := masked(t)
	m.Configure(host.MapConfig{"editor.secret_masking": "false"})
	if view := plainView(m); !strings.Contains(view, "abc123") {
		t.Error("editor.secret_masking=false must show raw values")
	}
	m, _ = m.Update(ActionMsg{Action: "toggle_secret_masking"})
	if view := plainView(m); !strings.Contains(view, secret.Mask) {
		t.Error("the view toggle must win over the config default")
	}
	// The per-Update config refresh must not clobber the override.
	m, _ = m.Update(ActionMsg{Action: "noop"})
	if view := plainView(m); strings.Contains(view, "abc123") {
		t.Error("config refresh clobbered the masking toggle")
	}
}

// TestLintNotesMarkLines: notes from the language's Lint reach the same gutter
// tint and inline underline that server diagnostics use.
func TestLintNotesMarkLines(t *testing.T) {
	m, path := mdLoaded(t, "PORT=1\nPORT=2\n")
	notes := []lang.Note{{Line: 0, StartCol: 0, EndCol: 4, Severity: lang.NoteWarn, Message: "duplicate key"}}
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Notes: notes})
	m = mm

	if sev, ok := m.worstSeverityOnLine(0); !ok || sev != lang.NoteWarn {
		t.Errorf("worstSeverityOnLine(0) = %d, %v; want a warning", sev, ok)
	}
	if _, ok := m.worstSeverityOnLine(1); ok {
		t.Error("the winning assignment must not be marked")
	}
	if sev, ok := m.diagSeverityAt(0, 2); !ok || sev != lang.NoteWarn {
		t.Errorf("diagSeverityAt(0,2) = %d, %v; want the key underlined", sev, ok)
	}
	if _, ok := m.diagSeverityAt(0, 5); ok {
		t.Error("the value must not be underlined")
	}
	if n, ok := m.NoteAt(0, 1); !ok || n.Message != "duplicate key" {
		t.Errorf("NoteAt(0,1) = %+v, %v; want the note", n, ok)
	}
}

// TestLintNotesReplacedByNextPass: an edit that removes the duplicate clears
// the mark — the notes channel is replaced wholesale, like the span index.
func TestLintNotesReplacedByNextPass(t *testing.T) {
	m, path := mdLoaded(t, "PORT=1\nPORT=2\n")
	notes := []lang.Note{{Line: 0, StartCol: 0, EndCol: 4, Severity: lang.NoteWarn, Message: "duplicate key"}}
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Notes: notes})
	m = mm
	mm, _ = m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion})
	m = mm
	if _, ok := m.worstSeverityOnLine(0); ok {
		t.Error("a pass without notes must clear the mark")
	}
}
