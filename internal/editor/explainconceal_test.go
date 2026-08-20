package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/concealexplain"
	"ike/internal/editor/buffer"
	"ike/internal/highlight"
	"ike/internal/numhint"
	"ike/internal/secret"
)

// explainconceal_test.go covers the explain & override popover (#1998): what
// it says about a span, and what its one-key actions produce. The spans are
// synthetic, like the other conceal-family tests — the provenance itself is
// covered in internal/concealexplain.

// explainOn loads a buffer, applies one stand-in span over the first line and
// parks the caret at col inside it.
func explainOn(t *testing.T, content string, span highlight.Span, col int) Model {
	t.Helper()
	m, path := mdLoaded(t, content)
	mm, _ := m.Update(highlight.SpansMsg{Path: path, Version: m.docVersion, Spans: []highlight.Span{span}})
	mm.cursor = buffer.Position{Line: 0, Col: col}
	return mm
}

// explainView opens the popover with g? and returns its rendered text.
func explainView(t *testing.T, m Model) (Model, string) {
	t.Helper()
	m = typeKeys(m, "g?")
	if !m.ExplainOpen() {
		t.Fatal("g? did not open the explain popover")
	}
	return m, ansiRE.ReplaceAllString(m.ExplainView(), "")
}

// TestExplainNumberHintNamesHeuristic (#1998): the popover on a concealed
// number names the heuristic that fired and the unit it chose.
func TestExplainNumberHintNamesHeuristic(t *testing.T) {
	numhint.SetFieldUnits(nil)
	span := highlight.Span{Line: 0, StartCol: 10, EndCol: 18, Capture: numhint.SizeCapture, Replace: "10 MiB"}
	m := explainOn(t, "max_size: 10485760\nplain\n", span, 12)
	m, view := explainView(t, m)
	for _, want := range []string{"10485760", "10 MiB", `"size"`, "binary byte size", "max_size"} {
		if !strings.Contains(view, want) {
			t.Fatalf("popover does not mention %q, view:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "reclassify") {
		t.Fatalf("popover offers no reclassification, view:\n%s", view)
	}
}

// TestExplainSecretNamesPattern (#1998): on a masked value the popover names
// the secret pattern that matched the key.
func TestExplainSecretNamesPattern(t *testing.T) {
	secret.SetKeyPatterns(nil)
	span := highlight.Span{Line: 0, StartCol: 6, EndCol: 12, Capture: secret.Capture, Replace: secret.Mask}
	m := explainOn(t, "TOKEN=abc123\nPORT=80\n", span, 8)
	m, view := explainView(t, m)
	for _, want := range []string{"abc123", "TOKEN", "always mask", "never mask"} {
		if !strings.Contains(view, want) {
			t.Fatalf("popover does not mention %q, view:\n%s", want, view)
		}
	}
}

// TestExplainAdjacentCaret (#1686 widening): a caret directly after the span
// still explains it — that is where appending digits leaves it.
func TestExplainAdjacentCaret(t *testing.T) {
	numhint.SetFieldUnits(nil)
	span := highlight.Span{Line: 0, StartCol: 10, EndCol: 18, Capture: numhint.SizeCapture, Replace: "10 MiB"}
	m := explainOn(t, "max_size: 10485760\nplain\n", span, 18)
	_, view := explainView(t, m)
	if !strings.Contains(view, "10485760") {
		t.Fatalf("adjacent caret did not explain the span, view:\n%s", view)
	}
}

// TestExplainReveal (#1686): the reveal key moves the caret into the span, so
// the positional reveal shows the raw value, and closes the popover.
func TestExplainReveal(t *testing.T) {
	numhint.SetFieldUnits(nil)
	span := highlight.Span{Line: 0, StartCol: 10, EndCol: 18, Capture: numhint.SizeCapture, Replace: "10 MiB"}
	m := explainOn(t, "max_size: 10485760\nplain\n", span, 18)
	m, _ = explainView(t, m)
	m = typeKeys(m, "r")
	if m.ExplainOpen() {
		t.Fatal("reveal left the popover open")
	}
	if m.cursor.Col != 10 {
		t.Fatalf("caret at col %d, want the span start", m.cursor.Col)
	}
	if view := plainView(m); !strings.Contains(view, "10485760") {
		t.Fatalf("raw value not revealed, view:\n%s", view)
	}
}

// ruleMsgOf runs a key against an open popover and returns the rule message it
// produced.
func ruleMsgOf(t *testing.T, m Model, k string) ConcealRuleMsg {
	t.Helper()
	var cmd tea.Cmd
	for _, kp := range keys(k) {
		m, cmd = m.Update(kp)
	}
	if cmd == nil {
		t.Fatal("no command from the popover key")
	}
	msg, ok := cmd().(ConcealRuleMsg)
	if !ok {
		t.Fatalf("key produced %T, want ConcealRuleMsg", cmd())
	}
	return msg
}

// TestExplainReclassifyWritesFieldRule (#1998): a reclassification key emits
// the field rule for the store the heuristics read.
func TestExplainReclassifyWritesFieldRule(t *testing.T) {
	numhint.SetFieldUnits(nil)
	span := highlight.Span{Line: 0, StartCol: 10, EndCol: 18, Capture: numhint.SizeCapture, Replace: "10 MiB"}
	m := explainOn(t, "max_size: 10485760\nplain\n", span, 12)
	m, _ = explainView(t, m)
	msg := ruleMsgOf(t, m, "4") // epoch timestamp (seconds)
	if msg.Setting != concealexplain.UnitsSetting {
		t.Fatalf("setting = %q", msg.Setting)
	}
	if msg.Entry != "max_size=timestamp-s" {
		t.Fatalf("entry = %q", msg.Entry)
	}
	if msg.Pattern != "max_size" {
		t.Fatalf("pattern = %q", msg.Pattern)
	}
}

// TestExplainPinsCurrentReading (#1998): "a" pins the reading the heuristic
// chose, so a later heuristic change cannot move the field.
func TestExplainPinsCurrentReading(t *testing.T) {
	numhint.SetFieldUnits(nil)
	span := highlight.Span{Line: 0, StartCol: 10, EndCol: 18, Capture: numhint.SizeCapture, Replace: "10 MiB"}
	m := explainOn(t, "max_size: 10485760\nplain\n", span, 12)
	m, _ = explainView(t, m)
	if msg := ruleMsgOf(t, m, "a"); msg.Entry != "max_size=bytes" {
		t.Fatalf("entry = %q, want the current reading", msg.Entry)
	}
}

// TestExplainSecretRules (#1998): the masking keys emit the positive and the
// exempting entry for the key pattern store.
func TestExplainSecretRules(t *testing.T) {
	secret.SetKeyPatterns(nil)
	span := highlight.Span{Line: 0, StartCol: 6, EndCol: 12, Capture: secret.Capture, Replace: secret.Mask}
	base := explainOn(t, "TOKEN=abc123\nPORT=80\n", span, 8)
	m, _ := explainView(t, base)
	msg := ruleMsgOf(t, m, "u")
	if msg.Setting != concealexplain.SecretSetting || msg.Entry != "-TOKEN" {
		t.Fatalf("exempt rule = %q / %q", msg.Setting, msg.Entry)
	}
	m, _ = explainView(t, base)
	if msg := ruleMsgOf(t, m, "m"); msg.Entry != "TOKEN" {
		t.Fatalf("mask rule = %q", msg.Entry)
	}
}

// TestExplainPlainValue (#1998, #1930): with nothing concealed the popover
// still answers — why this value is *not* transformed.
func TestExplainPlainValue(t *testing.T) {
	numhint.SetFieldUnits(nil)
	secret.SetKeyPatterns(nil)
	m, _ := mdLoaded(t, "retries = 3\nplain\n")
	m.cursor = buffer.Position{Line: 0, Col: 10}
	m, view := explainView(t, m)
	if !strings.Contains(view, "no rule matches") {
		t.Fatalf("popover does not explain the absence of a rule, view:\n%s", view)
	}
	if msg := ruleMsgOf(t, m, "1"); msg.Entry != "retries=bytes" {
		t.Fatalf("entry = %q", msg.Entry)
	}
}

// TestExplainEscapeCloses (#1998): esc closes the popover, and an unrelated
// key closes it and is handled normally.
func TestExplainEscapeCloses(t *testing.T) {
	numhint.SetFieldUnits(nil)
	span := highlight.Span{Line: 0, StartCol: 10, EndCol: 18, Capture: numhint.SizeCapture, Replace: "10 MiB"}
	m := explainOn(t, "max_size: 10485760\nplain\n", span, 12)
	open, _ := explainView(t, m)
	closed, _ := open.Update(special(tea.KeyEscape))
	if closed.ExplainOpen() {
		t.Fatal("esc left the popover open")
	}
	open, _ = explainView(t, m)
	moved := typeKeys(open, "j")
	if moved.ExplainOpen() {
		t.Fatal("an unrelated key left the popover open")
	}
	if moved.cursor.Line != 1 {
		t.Fatalf("the unrelated key was swallowed, cursor at %v", moved.cursor)
	}
}
