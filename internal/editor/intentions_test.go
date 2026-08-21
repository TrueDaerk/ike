package editor

import (
	"testing"

	"ike/internal/concealfilter"
	"ike/internal/editor/buffer"
	"ike/internal/highlight"
	ilsp "ike/internal/lsp"
	"ike/internal/numhint"
	"ike/internal/secret"
)

// intentions_test.go covers the caret probes the intention popup gates on
// (#2020), after the applicability audit tightened them (#2026): a probe must
// answer true only where the command behind it would actually do something.

// TestConcealExplainAtCaretNeedsAStandIn is the #2026 acceptance case: on a
// plain identifier — `getConfig` in a Python import — nothing is concealed,
// so "Explain Concealed Value" is not offered. The popover itself still
// explains such a value when asked directly (#1930); it is the *intention*
// that was noise.
func TestConcealExplainAtCaretNeedsAStandIn(t *testing.T) {
	secret.SetKeyPatterns(nil)
	numhint.SetFieldUnits(nil)
	m, _ := mdLoaded(t, "from lib.x import getConfig\nplain\n")
	m.cursor = buffer.Position{Line: 0, Col: 20}
	if fam, ok := m.ConcealExplainAtCaret(); ok {
		t.Fatalf("plain identifier reported as concealed (family %q)", fam)
	}
}

// TestConcealExplainAtCaretOnMask: a caret on a secret mask reports the
// masking family, so the popup offers the explain entry together with the
// family's view toggle.
func TestConcealExplainAtCaretOnMask(t *testing.T) {
	secret.SetKeyPatterns(nil)
	span := highlight.Span{Line: 0, StartCol: 6, EndCol: 12, Capture: secret.Capture, Replace: secret.Mask}
	m := explainOn(t, "TOKEN=abc123\nPORT=80\n", span, 8)
	fam, ok := m.ConcealExplainAtCaret()
	if !ok {
		t.Fatal("caret on a mask reported nothing to explain")
	}
	if fam != concealfilter.SecretMasking {
		t.Fatalf("family=%q want %q", fam, concealfilter.SecretMasking)
	}
}

// TestConcealExplainAtCaretOnNumberHint: the same for a number stand-in, the
// other half of what the explainer speaks for.
func TestConcealExplainAtCaretOnNumberHint(t *testing.T) {
	numhint.SetFieldUnits(nil)
	span := highlight.Span{Line: 0, StartCol: 10, EndCol: 18, Capture: numhint.SizeCapture, Replace: "10 MiB"}
	m := explainOn(t, "max_size: 10485760\nplain\n", span, 12)
	fam, ok := m.ConcealExplainAtCaret()
	if !ok {
		t.Fatal("caret on a size hint reported nothing to explain")
	}
	if fam != concealfilter.ByteSizeHints {
		t.Fatalf("family=%q want %q", fam, concealfilter.ByteSizeHints)
	}
}

// TestDiagnosticOnCaretLineNeedsAnIgnoreRule (#2026): lsp.ignoreDiagnostic
// writes a rule matching source/code/message, so a diagnostic carrying none
// of them is not an offer — picking it only reported that there is nothing to
// match on.
func TestDiagnosticOnCaretLineNeedsAnIgnoreRule(t *testing.T) {
	m, _ := mdLoaded(t, "one\ntwo\n")
	m.cursor = buffer.Position{Line: 0, Col: 0}
	m.diagByLine = map[int][]ilsp.Diagnostic{0: {{}}}
	if m.DiagnosticOnCaretLine() {
		t.Fatal("diagnostic without source, code or message offered as ignorable")
	}
	m.diagByLine = map[int][]ilsp.Diagnostic{0: {{Source: "vet", Code: "S1000"}}}
	if !m.DiagnosticOnCaretLine() {
		t.Fatal("diagnostic with a source and code not offered as ignorable")
	}
}
