package app

import (
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/httpfile"
)

// TestHTTPLocalFailureShowsHumanMessageInPane (#3086): a request that fails
// before the wire — unresolved placeholder, unparsable URL, missing body
// file — leaves a one-line human message in the response pane instead of the
// raw Go error.
func TestHTTPLocalFailureShowsHumanMessageInPane(t *testing.T) {
	tests := []struct {
		name, text, want string
	}{
		{"placeholder", "GET http://example.invalid/${IKE_NOPE_3086}\n",
			"Unresolved placeholder ${IKE_NOPE_3086} — define it in the environment file"},
		{"url", "GET http://%zz/\n", "Invalid URL \"http://%zz/\""},
		{"file", "POST http://example.invalid/\nContent-Type: application/json\n\n< ./missing-3086.json\n",
			"Cannot read file "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := httpApp(t)
			source := filepath.Join(t.TempDir(), "api.http")
			f := httpfile.Parse(tt.text)
			if len(f.Requests) != 1 {
				t.Fatalf("requests = %d", len(f.Requests))
			}
			msg := awaitHTTPResponse(t, m.dispatchHTTPRequest(source, f, f.Requests[0]))
			if msg.Err == nil {
				t.Fatal("the dispatch must fail")
			}
			out, _ := m.Update(msg)
			m = out.(Model)
			p := m.httpPanel()
			if p == nil {
				t.Fatal("the failure must open the response pane")
			}
			if failure := p.Failure(); !strings.Contains(failure, tt.want) {
				t.Errorf("pane failure %q, want it to contain %q", failure, tt.want)
			}
			if view := p.View(); !strings.Contains(view, strings.Fields(tt.want)[0]+" ") {
				t.Errorf("pane view must show the message:\n%s", view)
			}
		})
	}
}
