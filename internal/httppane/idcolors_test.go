package httppane

import (
	"net/http"
	"strings"
	"testing"

	"ike/internal/httpclient"
	"ike/internal/idcolor"
)

// traceResponse is a JSON body carrying the same trace id twice.
func traceResponse() *httpclient.Response {
	return &httpclient.Response{
		Status:     "200 OK",
		StatusCode: 200,
		Proto:      "HTTP/1.1",
		Headers:    http.Header{"Content-Type": {"application/json"}},
		Body:       []byte(`{"trace":"550e8400-e29b-41d4-a716-446655440000","prev":"550e8400-e29b-41d4-a716-446655440000"}`),
		RequestKey: "trace",
	}
}

// TestBodyIdentifierColors (#1626): a UUID in the response body renders in the
// rainbow color of its own hash, and the global toggle removes it.
func TestBodyIdentifierColors(t *testing.T) {
	t.Cleanup(func() { idcolor.SetEnabled(true) })
	m := New(nil)
	m.SetSize(120, 20)
	m.Set("trace", traceResponse())

	slot := idcolor.Slot("550e8400-e29b-41d4-a716-446655440000")
	st, ok := m.hl.Style(idcolor.Capture(slot))
	if !ok {
		t.Fatal("rainbow slot must resolve to a style")
	}
	marker := ansiParams(st.Render("x"))
	if marker == "" {
		t.Fatal("rainbow style renders without ANSI color")
	}
	if view := m.View(); !strings.Contains(view, marker) {
		t.Errorf("body misses the identifier color %q:\n%s", marker, view)
	}

	idcolor.SetEnabled(false)
	if view := m.View(); strings.Contains(view, marker) {
		t.Errorf("disabled identifier coloring must not paint:\n%s", view)
	}
}

// TestBodyIDsGate (#1626): bodyIDs is silent while the feature is off and
// finds the identifier while it is on.
func TestBodyIDsGate(t *testing.T) {
	t.Cleanup(func() { idcolor.SetEnabled(true) })
	m := New(nil)
	line := `  "trace": "550e8400-e29b-41d4-a716-446655440000",`
	if got := m.bodyIDs(line); len(got) != 1 {
		t.Fatalf("want one identifier, got %v", got)
	}
	idcolor.SetEnabled(false)
	if got := m.bodyIDs(line); got != nil {
		t.Fatalf("disabled scan must return nil, got %v", got)
	}
}

// TestIDAt (#1626): column lookup covers the identifier's own range only.
func TestIDAt(t *testing.T) {
	spans := []idcolor.Span{{Start: 4, End: 10, Slot: 2}}
	if _, ok := idAt(spans, 3); ok {
		t.Error("column before the span must not match")
	}
	s, ok := idAt(spans, 4)
	if !ok || s.Slot != 2 {
		t.Errorf("column 4 = %v, %v", s, ok)
	}
	if _, ok := idAt(spans, 10); ok {
		t.Error("the end column is exclusive")
	}
}

// ansiParams returns the leading SGR sequence of a rendered string.
func ansiParams(s string) string {
	end := strings.Index(s, "m")
	if !strings.HasPrefix(s, "\x1b[") || end < 0 {
		return ""
	}
	return s[:end+1]
}
