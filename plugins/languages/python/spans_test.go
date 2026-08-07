package langpython

import (
	"testing"

	"ike/internal/lang"
	"ike/internal/numhint"
	"ike/internal/permhint"
)

// TestPythonPermissionSpans (#1656): an octal literal in the argument list of a
// mode API carries its symbolic form; the decimal spelling is not a Python
// octal literal and gets nothing.
func TestPythonPermissionSpans(t *testing.T) {
	l, ok := lang.ByPath("/p/deploy.py")
	if !ok || l.Spans == nil {
		t.Fatal("python: no Spans producer registered")
	}
	spans := l.Spans([]string{"os.chmod(target, 0o600)", "PORT = 8080"})
	if len(spans) != 1 || spans[0].Capture != permhint.Capture || spans[0].Line != 0 {
		t.Fatalf("spans = %+v, want one permission hint on line 0", spans)
	}
	if want := "0o600" + permhint.Gap + "rw-------"; spans[0].Replace != want {
		t.Errorf("Replace = %q, want %q", spans[0].Replace, want)
	}
}

// TestPythonConstantSpans (#1701): a CONST_CASE assignment whose right-hand
// side is pure literal arithmetic evaluates and conceals in the unit the
// name carries; a lowercase assignment stays raw.
func TestPythonConstantSpans(t *testing.T) {
	l, ok := lang.ByPath("/p/settings.py")
	if !ok || l.Spans == nil {
		t.Fatal("python: no Spans producer registered")
	}
	spans := l.Spans([]string{
		"MAX_BYTES = 10 * 1024 * 1024",
		"chunk = 4 * 1024",
	})
	if len(spans) != 1 || spans[0].Capture != numhint.SizeCapture || spans[0].Line != 0 {
		t.Fatalf("spans = %+v, want one byte-size conceal on line 0", spans)
	}
	if spans[0].Replace != "10 MiB" {
		t.Errorf("Replace = %q, want %q", spans[0].Replace, "10 MiB")
	}
}
