package semantic

import (
	"testing"

	"ike/internal/lsp/protocol"
)

var legend = Legend{
	TokenTypes:     []string{"keyword", "function", "variable", "type", "customThing"},
	TokenModifiers: []string{"declaration", "readonly", "defaultLibrary"},
}

func TestDecodeRelativeEncoding(t *testing.T) {
	lines := []string{"func main()", "\tx := y"}
	// func(kw @0:0 len4) main(fn @0:5 len4) then next line x(var @1:1 len1)
	data := []uint32{
		0, 0, 4, 0, 0,
		0, 5, 4, 1, 0,
		1, 1, 1, 2, 0,
	}
	spans := Decode(data, legend, lines, protocol.EncodingUTF16)
	if len(spans) != 3 {
		t.Fatalf("spans = %+v", spans)
	}
	if spans[0].Capture != "keyword" || spans[0].StartCol != 0 || spans[0].EndCol != 4 {
		t.Errorf("kw span = %+v", spans[0])
	}
	if spans[1].Capture != "function" || spans[1].StartCol != 5 || spans[1].EndCol != 9 {
		t.Errorf("fn span = %+v", spans[1])
	}
	if spans[2].Line != 1 || spans[2].Capture != "variable" || spans[2].StartCol != 1 {
		t.Errorf("var span = %+v", spans[2])
	}
}

func TestDecodeModifiersRefineCaptures(t *testing.T) {
	lines := []string{"const A = B"}
	data := []uint32{
		0, 6, 1, 2, 1 << 1, // variable + readonly -> constant
		0, 4, 1, 2, 1 << 2, // variable + defaultLibrary -> variable.builtin
	}
	spans := Decode(data, legend, lines, protocol.EncodingUTF16)
	if len(spans) != 2 || spans[0].Capture != "constant" || spans[1].Capture != "variable.builtin" {
		t.Fatalf("spans = %+v", spans)
	}
}

func TestDecodeDropsUnknownAndOutOfRange(t *testing.T) {
	lines := []string{"x"}
	data := []uint32{
		0, 0, 1, 4, 0, // customThing: no capture mapping
		5, 0, 1, 0, 0, // line 5: beyond the document
	}
	if spans := Decode(data, legend, lines, protocol.EncodingUTF16); len(spans) != 0 {
		t.Fatalf("spans = %+v", spans)
	}
}

func TestDecodeUTF16Offsets(t *testing.T) {
	lines := []string{"a🙂xy"}
	// Token starting at UTF-16 unit 3 (after a + emoji=2 units), length 2.
	data := []uint32{0, 3, 2, 1, 0}
	spans := Decode(data, legend, lines, protocol.EncodingUTF16)
	if len(spans) != 1 || spans[0].StartCol != 2 || spans[0].EndCol != 4 {
		t.Fatalf("utf-16 conversion wrong: %+v", spans)
	}
}

func TestApplyDelta(t *testing.T) {
	prev := []uint32{0, 0, 4, 0, 0, 1, 0, 3, 1, 0}
	// Replace the second tuple's length (index 7) and append one tuple.
	edits := []protocol.SemanticTokensEdit{
		{Start: 7, DeleteCount: 1, Data: []uint32{9}},
		{Start: 10, DeleteCount: 0, Data: []uint32{1, 0, 1, 2, 0}},
	}
	got := ApplyDelta(prev, edits)
	want := []uint32{0, 0, 4, 0, 0, 1, 0, 9, 1, 0, 1, 0, 1, 2, 0}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	// Original untouched, out-of-range clamped.
	if prev[7] != 3 {
		t.Fatal("ApplyDelta must not mutate prev")
	}
	if got := ApplyDelta([]uint32{1, 2}, []protocol.SemanticTokensEdit{{Start: 99, DeleteCount: 5, Data: []uint32{7}}}); len(got) != 3 {
		t.Fatalf("clamped edit wrong: %v", got)
	}
}
