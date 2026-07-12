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
	msg := SignatureContent(sh)
	if msg.Label != "Greet(name string, times int) string" {
		t.Fatalf("label = %q", msg.Label)
	}
	if got := string([]rune(msg.Label)[msg.ParamStart:msg.ParamEnd]); got != "times int" {
		t.Fatalf("offset pair should highlight %q, got %q", "times int", got)
	}
	if msg.Doc != "Greets someone." {
		t.Errorf("doc = %q", msg.Doc)
	}
	if msg.More != 1 {
		t.Errorf("more = %d", msg.More)
	}
	if len(msg.Params) != 2 {
		t.Fatalf("params = %d, want 2", len(msg.Params))
	}
	if msg.Params[0].Label != "name string" || msg.Params[1].Label != "times int" {
		t.Errorf("param labels = %q, %q", msg.Params[0].Label, msg.Params[1].Label)
	}
	if msg.ActiveParam != 1 {
		t.Errorf("active param = %d, want 1", msg.ActiveParam)
	}
}

func TestSignatureContentParamDocsAndClamp(t *testing.T) {
	sh := &protocol.SignatureHelp{
		ActiveParameter: 5, // out of range: no usable active parameter
		Signatures: []protocol.SignatureInformation{{
			Label: "add(a, b int)",
			Parameters: []protocol.ParameterInformation{
				{Label: json.RawMessage(`"a"`), Documentation: json.RawMessage(`"first operand"`)},
				{Label: json.RawMessage(`"b int"`), Documentation: json.RawMessage(`{"kind":"markdown","value":"second\nrest"}`)},
			},
		}},
	}
	msg := SignatureContent(sh)
	if msg.ActiveParam != -1 {
		t.Errorf("out-of-range active param should clamp to -1, got %d", msg.ActiveParam)
	}
	if msg.Params[0].Doc != "first operand" || msg.Params[1].Doc != "second" {
		t.Errorf("param docs = %q, %q", msg.Params[0].Doc, msg.Params[1].Doc)
	}
}

func TestSignatureContentSubstringAndNil(t *testing.T) {
	sh := &protocol.SignatureHelp{
		Signatures: []protocol.SignatureInformation{{
			Label:      "add(a, b int)",
			Parameters: []protocol.ParameterInformation{{Label: json.RawMessage(`"a"`)}},
		}},
	}
	msg := SignatureContent(sh)
	if got := msg.Params[0].Label; got != "a" {
		t.Fatalf("substring param should resolve, got %q in %q", got, msg.Label)
	}
	if m := SignatureContent(nil); m.Label != "" || m.ActiveParam != -1 {
		t.Fatal("nil help should flatten to empty")
	}
}

func TestUTF16OffToRune(t *testing.T) {
	s := "f(🙂 x, y)" // the emoji is 2 UTF-16 units, 1 rune
	if got := utf16OffToRune(s, 5); got != 4 {
		t.Fatalf("offset past the emoji should shrink by one, got %d", got)
	}
}
