package consthint

import (
	"strings"
	"testing"
)

// scriptspans_test.go covers the JS/TS constant conceals (#2345).

func TestScriptSpansConstCase(t *testing.T) {
	lines := []string{
		`export const MAX_BYTES = 0x100000;`,
		`const TIMEOUT_MS = 30_000`,
		`let RETRY_LIMIT = 6 * 7; // comment`,
		`const el = document.getElementById("x");`, // no gate: stays raw
		`const url = "https://example.com";`,
		`if (MAX_BYTES == 1) {`, // a comparison, not an assignment
	}
	spans := ScriptSpans(lines)
	if len(spans) != 3 {
		t.Fatalf("got %d spans, want 3: %+v", len(spans), spans)
	}
	if spans[0].Line != 0 || !strings.Contains(spans[0].Replace, "= 1") {
		t.Errorf("span 0 = %+v, want the 0x100000 radix reading on line 0", spans[0])
	}
	if spans[1].Line != 1 {
		t.Errorf("span 1 on line %d, want the underscored literal on line 1", spans[1].Line)
	}
	if spans[2].Line != 2 || spans[2].Replace != "42" {
		t.Errorf("span 2 = %+v, want the computed 42 on line 2", spans[2])
	}
}

func TestScriptSpansAnnotation(t *testing.T) {
	spans := ScriptSpans([]string{`const MAX_BYTES: number = 0x100000;`})
	if len(spans) != 1 || spans[0].Line != 0 {
		t.Fatalf("an annotated declaration must conceal, got %+v", spans)
	}
}
