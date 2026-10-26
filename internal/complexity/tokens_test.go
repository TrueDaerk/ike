//go:build cgo

package complexity

// tokens_test.go covers the normalized token stream (#3044): comments are
// dropped, identifiers and literals collapse to placeholders per kind, code
// outside every unit contributes nothing, every top-level unit is a
// segment, and the compact cache encoding round-trips.

import (
	"bytes"
	"encoding/gob"
	"reflect"
	"strings"
	"testing"
)

func TestTokensDropCommentsAndOutsideCode(t *testing.T) {
	plain := tokensOf(t, "go", goFile(goFunc("sumPos", "xs []int", goBody)))
	commented := tokensOf(t, "go", "package a\n\nimport \"fmt\"\n\nvar top = fmt.Sprint(1)\n\n// sumPos sums the positives.\n"+
		goFunc("sumPos", "xs []int", "\t// the accumulator\n"+strings.ReplaceAll(goBody, "return total", "return total // done")))
	if plain.Len() == 0 {
		t.Fatal("no tokens")
	}
	if !reflect.DeepEqual(plain.Hash, commented.Hash) {
		t.Fatalf("comments, imports and top-level code must not change the stream:\n%v\n%v", plain.Hash, commented.Hash)
	}
	if plain.Lang != "go" || len(plain.Seg) != 1 || plain.Seg[0] != 0 || len(plain.Line) != plain.Len() {
		t.Fatalf("stream shape = %+v", plain)
	}
	if plain.Line[0] != 2 || commented.Line[0] != 7 {
		t.Fatalf("first token lines %d / %d, want the func lines", plain.Line[0], commented.Line[0])
	}
}

func TestTokensPlaceholders(t *testing.T) {
	one := func(stmt string) Tokens {
		return tokensOf(t, "go", goFile("func f(a int) int {\n\t"+stmt+"\n}\n"))
	}
	if !reflect.DeepEqual(one("return a + 1").Hash, one("return b + 22").Hash) {
		t.Fatal("renamed identifier and changed number must match")
	}
	if reflect.DeepEqual(one("return a + 1").Hash, one("return a - 1").Hash) {
		t.Fatal("a different operator must not match")
	}
	if reflect.DeepEqual(one(`x := 1; return x`).Hash, one(`x := "s"; return x`).Hash) {
		t.Fatal("a number and a string are different literal kinds")
	}
	if !reflect.DeepEqual(one(`x := "one"; return len(x)`).Hash, one(`y := "two"; return len(y)`).Hash) {
		t.Fatal("a changed string must match")
	}
	if reflect.DeepEqual(one("return foo(a)").Hash, one("return bar(a)").Hash) {
		t.Fatal("a different callee must not match")
	}
	if reflect.DeepEqual(one("return a.x").Hash, one("return a.y").Hash) {
		t.Fatal("a different member must not match")
	}
}

func TestTokensSegmentsPerUnit(t *testing.T) {
	// A closure is part of its enclosing unit's segment; the two top-level
	// functions are two segments.
	src := goFile("func f(xs []int) int {\n\tg := func(x int) int { return x + 1 }\n\treturn g(xs[0])\n}\n",
		goFunc("h", "xs []int", goBody))
	fns, toks, err := MeasureTokens("go", strings.Split(src, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fns) != 3 {
		t.Fatalf("units = %d, want f, its closure and h", len(fns))
	}
	if len(toks.Seg) != 2 || toks.Seg[0] != 0 {
		t.Fatalf("segments = %v, want two", toks.Seg)
	}
	if toks.segEnd(0) != int(toks.Seg[1]) || toks.segEnd(1) != toks.Len() {
		t.Fatalf("segment ends %d / %d", toks.segEnd(0), toks.segEnd(1))
	}
	if got := int(toks.Line[toks.Seg[1]]); got != fns[2].Line {
		t.Fatalf("second segment starts at line %d, want h's line %d", got, fns[2].Line)
	}
}

func TestTokensUnsupported(t *testing.T) {
	if _, _, err := MeasureTokens("nope", []string{"x"}); err != ErrUnsupported {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
	if toks := tokensOf(t, "go", "package a\n\nvar x = 1\n"); toks.Len() != 0 || len(toks.Seg) != 0 {
		t.Fatalf("a file without units has no tokens: %+v", toks)
	}
}

func TestTokensGobRoundTrip(t *testing.T) {
	for _, toks := range []Tokens{
		{},
		{Lang: "go"},
		tokensOf(t, "go", goFile(goFunc("a", "xs []int", goBody), goFunc("b", "items []int", renamedBody))),
		{Lang: "x", Hash: []uint32{0, 1, 0xffffffff}, Line: []int32{5, 3, 90}, Seg: []int32{0, 2}},
	} {
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(toks); err != nil {
			t.Fatal(err)
		}
		var got Tokens
		if err := gob.NewDecoder(&buf).Decode(&got); err != nil {
			t.Fatal(err)
		}
		want := toks
		if want.Hash == nil {
			want.Hash = []uint32{}
		}
		if want.Line == nil {
			want.Line = []int32{}
		}
		if want.Seg == nil {
			want.Seg = []int32{}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("round trip\n got %+v\nwant %+v", got, want)
		}
	}
	// The packed form is compact: four bytes per hash plus a byte or two per
	// line.
	toks := tokensOf(t, "go", goFile(goFunc("a", "xs []int", goBody)))
	b, _ := toks.GobEncode()
	if len(b) > 6*toks.Len()+16 {
		t.Fatalf("%d tokens packed into %d bytes", toks.Len(), len(b))
	}
	var bad Tokens
	for _, in := range [][]byte{nil, {5}, {0, 200, 1}, {0, 2, 1, 2, 3, 4, 5, 6, 7, 8, 9}} {
		if err := bad.GobDecode(in); err == nil {
			t.Fatalf("decoding %v must fail", in)
		}
	}
}
