package httppane

import (
	"strings"
	"testing"

	"ike/internal/httpclient"
)

// A `# @no-log` exchange (#3115) says so on its status row; an ordinary one
// does not.
func TestNoLogMarkerOnStatusRow(t *testing.T) {
	resp := sample()
	resp.Request = &httpclient.RequestSnapshot{Method: "GET", URL: "http://example.com/", NoLog: true}
	m := New(nil)
	m.SetSize(120, 24)
	m.Set("create", resp)
	if !strings.Contains(m.rows[0].text, "· "+NotLoggedMarker) {
		t.Fatalf("status row = %q, want the not-logged marker", m.rows[0].text)
	}
	resp = sample()
	resp.Request = &httpclient.RequestSnapshot{Method: "GET", URL: "http://example.com/"}
	m.Set("create", resp)
	if strings.Contains(m.rows[0].text, NotLoggedMarker) {
		t.Fatalf("status row = %q, an ordinary exchange must carry no marker", m.rows[0].text)
	}
}
