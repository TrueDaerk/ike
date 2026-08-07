package langpython

import (
	"testing"

	"ike/internal/lang"
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
