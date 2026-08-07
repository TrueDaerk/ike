package langgo

import (
	"testing"

	"ike/internal/escapes"
	"ike/internal/lang"
	"ike/internal/nethint"
	"ike/internal/numhint"
	"ike/internal/permhint"
)

// TestGoUnicodeEscapeSpans (#1620): the go language registers the
// unicode-escape span producer; escapes in string literals decode.
func TestGoUnicodeEscapeSpans(t *testing.T) {
	l, ok := lang.ByID("go")
	if !ok || l.Spans == nil {
		t.Fatal("go: no Spans producer registered")
	}
	spans := l.Spans([]string{`s := "M\u00e4rz \ud83d\ude00"`})
	if len(spans) != 2 {
		t.Fatalf("spans = %+v, want 2", spans)
	}
	if spans[0].Replace != "\u00e4" || spans[0].Capture != escapes.UnicodeCapture {
		t.Errorf("first span = %+v, want \u00e4 as %s", spans[0], escapes.UnicodeCapture)
	}
	if spans[1].Replace != "\U0001F600" {
		t.Errorf("second span = %+v, want the surrogate pair combined", spans[1])
	}
}

// TestGoNetworkHints (#1653): a CIDR prefix inside a Go string literal
// carries its range; the same characters outside a literal do not.
func TestGoNetworkHints(t *testing.T) {
	l, ok := lang.ByID("go")
	if !ok || l.Spans == nil {
		t.Fatal("go: no Spans producer registered")
	}
	spans := l.Spans([]string{"\tsubnet := \"10.0.0.0/24\"", "\tratio := total / 24"})
	var hint *lang.Span
	for i, s := range spans {
		if s.Capture == nethint.CIDRCapture {
			hint = &spans[i]
		}
	}
	if hint == nil || hint.Line != 0 {
		t.Fatalf("spans = %+v, want one CIDR hint on the string literal", spans)
	}
	if want := "10.0.0.0/24" + nethint.Gap + "10.0.0.0–10.0.0.255, 254 hosts"; hint.Replace != want {
		t.Errorf("hint = %q, want %q", hint.Replace, want)
	}
}

// TestGoPermissionSpans (#1656): an octal literal in the argument list of a
// mode API carries its symbolic form; a decimal one is not a Go octal literal
// and gets nothing.
func TestGoPermissionSpans(t *testing.T) {
	l, ok := lang.ByPath("/p/main.go")
	if !ok || l.Spans == nil {
		t.Fatal("go: no Spans producer registered")
	}
	spans := l.Spans([]string{"\tos.WriteFile(path, data, 0o644)", "\tport := 8080"})
	if len(spans) != 1 || spans[0].Capture != permhint.Capture || spans[0].Line != 0 {
		t.Fatalf("spans = %+v, want one permission hint on line 0", spans)
	}
	if want := "0o644" + permhint.Gap + "rw-r--r--"; spans[0].Replace != want {
		t.Errorf("Replace = %q, want %q", spans[0].Replace, want)
	}
}

// TestGoConstantSpans (#1701): a const declaration whose right-hand side is
// pure literal arithmetic evaluates and conceals in the unit the constant's
// name carries; an iota line stays raw.
func TestGoConstantSpans(t *testing.T) {
	l, ok := lang.ByID("go")
	if !ok || l.Spans == nil {
		t.Fatal("go: no Spans producer registered")
	}
	spans := l.Spans([]string{
		"const maxUploadBytes = 10 << 20",
		"const kindFirst = iota",
	})
	if len(spans) != 1 || spans[0].Capture != numhint.SizeCapture || spans[0].Line != 0 {
		t.Fatalf("spans = %+v, want one byte-size conceal on line 0", spans)
	}
	if spans[0].Replace != "10 MiB" {
		t.Errorf("Replace = %q, want %q", spans[0].Replace, "10 MiB")
	}
}
