package httppane

import (
	"net/http"
	"testing"

	"ike/internal/httpclient"
)

// bodylang_test.go covers BodyLang (#2451), the language the shown body is
// classified as — what playground.open picks its dialect from in the host.

func TestBodyLangNamesTheShownBody(t *testing.T) {
	for _, tc := range []struct{ name, ct, body, want string }{
		{"json", "application/json", `{"a":1}`, "json"},
		{"json vendor suffix", "application/vnd.api+json; charset=utf-8", `{"a":1}`, "json"},
		{"yaml", "application/yaml", "a: 1\n", "yaml"},
		{"yaml legacy", "text/x-yaml", "a: 1\n", "yaml"},
		{"yaml suffix", "application/vnd.k8s+yaml", "a: 1\n", "yaml"},
		{"xml", "text/xml", "<a/>", "xml"},
		{"html", "text/html; charset=utf-8", "<html></html>", "html"},
		{"plain text", "text/plain", "hello", ""},
		{"unknown type", "application/octet-stream", "hello", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(nil)
			m.SetSize(80, 20)
			m.Set("one", &httpclient.Response{
				Status: "200 OK", StatusCode: 200, Proto: "HTTP/1.1",
				Headers: http.Header{"Content-Type": {tc.ct}},
				Body:    []byte(tc.body),
			})
			if got := m.BodyLang(); got != tc.want {
				t.Errorf("BodyLang() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestBodyLangWithoutABody: an empty or binary body is nothing to query, so it
// carries no language however the server labelled it.
func TestBodyLangWithoutABody(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"empty", nil},
		{"binary", []byte{0x00, 0x01, 0x02}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(nil)
			m.SetSize(80, 20)
			m.Set("one", &httpclient.Response{
				Status: "200 OK", StatusCode: 200, Proto: "HTTP/1.1",
				Headers: http.Header{"Content-Type": {"application/json"}},
				Body:    tc.body,
			})
			if got := m.BodyLang(); got != "" {
				t.Errorf("BodyLang() = %q, want \"\" for a %s body", got, tc.name)
			}
		})
	}
}

// TestBodyLangWithNoResponse: the empty pane classifies nothing.
func TestBodyLangWithNoResponse(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 20)
	if got := m.BodyLang(); got != "" {
		t.Errorf("BodyLang() on an empty pane = %q, want \"\"", got)
	}
}
