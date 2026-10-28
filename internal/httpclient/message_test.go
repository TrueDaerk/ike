package httpclient

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"ike/internal/httpfile"
)

// TestClassifyLocalErrors (#3086): the failures dispatch produces before the
// wire classify as placeholder / url / file, and ErrorMessage names them.
func TestClassifyLocalErrors(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name, text, reason, msg string
	}{
		{"placeholder", "GET http://h/${NOPE_3086}\nX-A: ${NOPE_B}\n",
			"placeholder", "Unresolved placeholders ${NOPE_3086}, ${NOPE_B} — define them in the environment file"},
		{"user placeholder", "GET http://h/{{token}}\n",
			"placeholder", "Unresolved placeholder ${token} — define it in the environment file"},
		{"url parse", "GET http://%zz/\n", "url", "Invalid URL \"http://%zz/\""},
		{"url no host", "GET http:///path\n", "url", "target has no host"},
		{"file", "POST http://h/\n\n< ./missing.json\n", "file",
			"Cannot read file " + filepath.Join(dir, "missing.json")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := httpfile.Parse(tt.text)
			_, err := Dispatch(context.Background(), f.Requests[0], Options{
				Lookup:        func(string) (string, bool) { return "", false },
				DisableConfig: true,
				BaseDir:       dir,
			})
			if got := ClassifyError(err); got != tt.reason {
				t.Fatalf("ClassifyError(%v) = %q, want %q", err, got, tt.reason)
			}
			if got := ErrorMessage(err); !strings.Contains(got, tt.msg) {
				t.Errorf("ErrorMessage = %q, want %q", got, tt.msg)
			}
		})
	}
	if ErrorMessage(errors.New("boom")) != "" || ErrorMessage(syscall.ECONNREFUSED) != "" {
		t.Error("network / other errors keep their own reporting")
	}
}
