package lsp

import (
	"testing"

	"ike/internal/host"
)

func TestTypedChar(t *testing.T) {
	ev := host.EditorEvent{Text: "ab(\nsecond", Line: 0, Col: 3}
	if got := typedChar(ev); got != "(" {
		t.Fatalf("typedChar = %q", got)
	}
	if got := typedChar(host.EditorEvent{Text: "ab", Line: 0, Col: 0}); got != "" {
		t.Fatalf("col 0 should yield empty, got %q", got)
	}
	if got := typedChar(host.EditorEvent{Text: "ab", Line: 5, Col: 1}); got != "" {
		t.Fatalf("out-of-range line should yield empty, got %q", got)
	}
	// Unicode before the cursor.
	if got := typedChar(host.EditorEvent{Text: "π(", Line: 0, Col: 2}); got != "(" {
		t.Fatalf("unicode line typedChar = %q", got)
	}
}

// TestSignatureAutoAndInlayToggles covers the config gates (#523): the
// signature auto-popup defaults on (absent key), inlay hints default off.
func TestSignatureAutoAndInlayToggles(t *testing.T) {
	mk := func(cfg host.MapConfig) *bridge {
		return &bridge{h: host.New(cfg)}
	}
	if b := mk(host.MapConfig{}); !b.signatureAutoEnabled() {
		t.Fatal("absent lsp.signature_auto must mean enabled")
	}
	if b := mk(host.MapConfig{"lsp.signature_auto": "false"}); b.signatureAutoEnabled() {
		t.Fatal("lsp.signature_auto=false must disable the auto popup")
	}
	if b := mk(host.MapConfig{}); b.inlayHintsEnabled() {
		t.Fatal("absent lsp.inlay_hints must mean disabled (#523)")
	}
	if b := mk(host.MapConfig{"lsp.inlay_hints": "true"}); !b.inlayHintsEnabled() {
		t.Fatal("lsp.inlay_hints=true must enable hints")
	}
	if (&bridge{}).signatureAutoEnabled() != true {
		t.Fatal("no host attached must mean auto enabled")
	}
}

// TestStringRetryCols covers the #525 fallback scanner: inside a string
// argument the request retries just before the opening delimiter, then just
// past the closing one.
func TestStringRetryCols(t *testing.T) {
	eq := func(a, b []int) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}
	line := `	t.Error("abc") // "x"`
	cases := []struct {
		name string
		col  int
		want []int
	}{
		{"on function name", 3, nil},
		{"inside string", 11, []int{8, 14}},
		{"on closing quote", 13, []int{8, 14}},
		{"after closing quote", 14, nil},
		{"second literal on the line", 20, []int{18, 22}},
	}
	for _, c := range cases {
		if got := stringRetryCols(line, c.col); !eq(got, c.want) {
			t.Errorf("%s: col %d = %v, want %v", c.name, c.col, got, c.want)
		}
	}

	// Escaped quote does not close the literal; backticks ignore backslashes.
	if got := stringRetryCols(`f("a\"b", 1)`, 6); !eq(got, []int{1, 8}) {
		t.Errorf(`escaped quote: got %v, want [1 8]`, got)
	}
	if got := stringRetryCols("f(`a\\`, x)", 7); got != nil {
		t.Errorf("outside a raw string must return nil, got %v", got)
	}
	if got := stringRetryCols("f('x', y)", 3); !eq(got, []int{1, 5}) {
		t.Errorf("single quote: got %v, want [1 5]", got)
	}
	if got := stringRetryCols("plain(code, here)", 8); got != nil {
		t.Errorf("no literal on the line must return nil, got %v", got)
	}
	// Literal at line start (col 0) has no front candidate.
	if got := stringRetryCols(`"x" + y`, 1); !eq(got, []int{3}) {
		t.Errorf("line-start literal: got %v, want [3]", got)
	}
}

func TestIsTriggerChar(t *testing.T) {
	trig := []string{"(", ","}
	if !isTriggerChar("(", trig) || !isTriggerChar(",", trig) {
		t.Fatal("advertised chars should trigger")
	}
	if isTriggerChar(")", trig) || isTriggerChar("", trig) {
		t.Fatal("other chars must not trigger")
	}
}
