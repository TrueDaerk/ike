package httppane

// window_test.go covers the windowed render path (#2386): per-column data is
// gathered for the drawn window of a row, not the whole line — a megabyte
// minified body must not cost the whole line on every frame — and the
// longest-row cache behind maxLeft invalidates whenever rows change.

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"ike/internal/httpclient"
	"ike/internal/idcolor"
)

// TestWindowedIDColoringPansIntoView: an identifier deep inside a long single
// line is not painted while off screen, and paints with its full-text slot
// once the view pans onto it — the padded window scan sees it whole.
func TestWindowedIDColoringPansIntoView(t *testing.T) {
	const uuid = "550e8400-e29b-41d4-a716-446655440000"
	line := strings.Repeat("x ", 2500) + `"` + uuid + `" tail`
	resp := &httpclient.Response{
		Status: "200 OK", StatusCode: 200, Proto: "HTTP/1.1",
		Headers:  http.Header{"Content-Type": {"text/plain"}},
		Body:     []byte(line),
		Duration: time.Millisecond,
	}
	m := New(nil)
	m.SetSize(80, 10)
	m.Set("r", resp)
	m.FinishHighlight()

	st, ok := m.hl.Style(idcolor.Capture(idcolor.Slot(uuid)))
	if !ok {
		t.Fatal("rainbow slot must resolve to a style")
	}
	marker := ansiParams(st.Render("x"))
	if marker == "" {
		t.Fatal("rainbow style renders without ANSI color")
	}
	if view := m.View(); strings.Contains(view, marker) {
		t.Error("identifier color painted while the identifier is off screen")
	}
	m.ScrollX(5000)
	if view := m.View(); !strings.Contains(view, marker) {
		t.Errorf("identifier color missing after panning to it (left=%d)", m.Left())
	}
}

// TestMaxLeftCacheFollowsStream: the cached longest-row width invalidates when
// a stream appends wider rows, so the sideways clamp keeps up.
func TestMaxLeftCacheFollowsStream(t *testing.T) {
	m := New(nil)
	m.SetSize(40, 10)
	m.StartStream("s", "HTTP/1.1", "200 OK", http.Header{})
	if got := m.maxLeft(); got != 0 {
		t.Fatalf("maxLeft before body rows = %d, want 0", got)
	}
	m.AppendStream([]byte(strings.Repeat("a", 200) + "\n"))
	if got, want := m.maxLeft(), 200-m.rowWidth(); got != want {
		t.Fatalf("maxLeft after wide stream row = %d, want %d", got, want)
	}
}
