package lsp

import (
	"encoding/json"
	"testing"

	"ike/internal/lsp/protocol"
)

func TestSignatureContentOffsets(t *testing.T) {
	sh := &protocol.SignatureHelp{
		ActiveSignature: 1,
		ActiveParameter: 1,
		Signatures: []protocol.SignatureInformation{
			{Label: "f(x int)"},
			{
				Label:         "Greet(name string, times int) string",
				Documentation: json.RawMessage(`{"kind":"markdown","value":"Greets someone.\nMore."}`),
				Parameters: []protocol.ParameterInformation{
					{Label: json.RawMessage(`"name string"`)},
					{Label: json.RawMessage(`[19, 28]`)},
				},
			},
		},
	}
	label, start, end, doc, more := SignatureContent(sh)
	if label != "Greet(name string, times int) string" {
		t.Fatalf("label = %q", label)
	}
	if got := string([]rune(label)[start:end]); got != "times int" {
		t.Fatalf("offset pair should highlight %q, got %q", "times int", got)
	}
	if doc != "Greets someone." {
		t.Errorf("doc = %q", doc)
	}
	if more != 1 {
		t.Errorf("more = %d", more)
	}
}

func TestSignatureContentSubstringAndNil(t *testing.T) {
	sh := &protocol.SignatureHelp{
		Signatures: []protocol.SignatureInformation{{
			Label:      "add(a, b int)",
			Parameters: []protocol.ParameterInformation{{Label: json.RawMessage(`"a"`)}},
		}},
	}
	label, start, end, _, _ := SignatureContent(sh)
	if got := string([]rune(label)[start:end]); got != "a" {
		t.Fatalf("substring param should highlight, got %q in %q", got, label)
	}
	if l, _, _, _, _ := SignatureContent(nil); l != "" {
		t.Fatal("nil help should flatten to empty")
	}
}

func TestUTF16OffToRune(t *testing.T) {
	s := "f(🙂 x, y)" // the emoji is 2 UTF-16 units, 1 rune
	if got := utf16OffToRune(s, 5); got != 4 {
		t.Fatalf("offset past the emoji should shrink by one, got %d", got)
	}
}
