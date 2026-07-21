package snippet

import (
	"reflect"
	"testing"
)

func expand(t *testing.T, src string) (string, []int) {
	t.Helper()
	text, stops, err := Expand(src)
	if err != nil {
		t.Fatalf("Expand(%q) error: %v", src, err)
	}
	return text, stops
}

func TestPlainTextNoStops(t *testing.T) {
	text, stops := expand(t, "hello world")
	if text != "hello world" || stops != nil {
		t.Fatalf("got %q %v", text, stops)
	}
}

func TestBareTabstopsVisitOrderImplicitFinal(t *testing.T) {
	// "f($2, $1)" visits 1 then 2, plus the implicit end stop (no $0).
	text, stops := expand(t, "f($2, $1)")
	if text != "f(, )" {
		t.Fatalf("text = %q", text)
	}
	if want := []int{4, 2, 5}; !reflect.DeepEqual(stops, want) {
		t.Fatalf("stops = %v, want %v", stops, want)
	}
}

func TestPlaceholderDefaultStopAtEnd(t *testing.T) {
	text, stops := expand(t, "for ${1:i} := 0$0")
	if text != "for i := 0" {
		t.Fatalf("text = %q", text)
	}
	// Stop 1 at the end of "i" (offset 5), $0 at the end.
	if want := []int{5, 10}; !reflect.DeepEqual(stops, want) {
		t.Fatalf("stops = %v, want %v", stops, want)
	}
}

func TestNestedPlaceholder(t *testing.T) {
	text, stops := expand(t, "${1:outer ${2:inner}}$0")
	if text != "outer inner" {
		t.Fatalf("text = %q", text)
	}
	// Visit 1 (end of full default), then 2 (end of "inner" — same offset
	// here), then $0.
	if want := []int{11, 11, 11}; !reflect.DeepEqual(stops, want) {
		t.Fatalf("stops = %v, want %v", stops, want)
	}
}

func TestChoiceTakesFirst(t *testing.T) {
	text, stops := expand(t, "${1|public,private|} x$0")
	if text != "public x" {
		t.Fatalf("text = %q", text)
	}
	if want := []int{6, 8}; !reflect.DeepEqual(stops, want) {
		t.Fatalf("stops = %v, want %v", stops, want)
	}
}

func TestVariablesResolveEmptyOrDefault(t *testing.T) {
	if text, _ := expand(t, "a $TM_FILENAME b"); text != "a  b" {
		t.Fatalf("bare variable: %q", text)
	}
	if text, _ := expand(t, "a ${TM_FILENAME:fallback} b"); text != "a fallback b" {
		t.Fatalf("variable default: %q", text)
	}
	if text, _ := expand(t, "a ${TM_FILENAME} b"); text != "a  b" {
		t.Fatalf("braced variable: %q", text)
	}
}

func TestEscapes(t *testing.T) {
	text, stops := expand(t, `\$1 costs \\ ${1:a\}b}`)
	if text != `$1 costs \ a}b` {
		t.Fatalf("text = %q", text)
	}
	if len(stops) != 2 { // stop 1 + implicit final
		t.Fatalf("stops = %v", stops)
	}
}

func TestMirroredTabstopKeepsFirst(t *testing.T) {
	_, stops := expand(t, "$1 and $1$0")
	if want := []int{0, 5}; !reflect.DeepEqual(stops, want) {
		t.Fatalf("stops = %v, want %v", stops, want)
	}
}

func TestLoneAndTrailingDollarLiteral(t *testing.T) {
	if text, _ := expand(t, "a $ b $"); text != "a $ b $" {
		t.Fatalf("text = %q", text)
	}
}

func TestMalformedErrors(t *testing.T) {
	for _, src := range []string{"${1:unterminated", "${", "${1|a,b", "${1:x${2:y}", "${!}"} {
		if _, _, err := Expand(src); err == nil {
			t.Errorf("Expand(%q) should error", src)
		}
	}
}

func TestMultiline(t *testing.T) {
	text, stops := expand(t, "if $1 {\n\t$0\n}")
	if text != "if  {\n\t\n}" {
		t.Fatalf("text = %q", text)
	}
	if want := []int{3, 7}; !reflect.DeepEqual(stops, want) {
		t.Fatalf("stops = %v, want %v", stops, want)
	}
}
