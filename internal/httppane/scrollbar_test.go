package httppane

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"ike/internal/httpclient"
)

// sbModel builds a viewer with n body lines in a pane sized w×h.
func sbModel(n, w, h int) Model {
	body := make([]string, n)
	for i := range body {
		body[i] = "line"
	}
	m := New(nil)
	m.SetSize(w, h)
	m.Set("req", &httpclient.Response{
		Status:     "200 OK",
		StatusCode: 200,
		Proto:      "HTTP/1.1",
		Headers:    http.Header{"Content-Type": {"text/plain"}},
		Body:       []byte(strings.Join(body, "\n")),
		Duration:   time.Millisecond,
	})
	return m
}

func TestScrollbarGeometry(t *testing.T) {
	m := sbModel(100, 40, 12) // body height 10, well over 100 display rows

	track, total, start, length, ok := m.scrollbarGeometry()
	if !ok {
		t.Fatal("overflowing view should have a scrollbar")
	}
	if track != m.bodyHeight() {
		t.Errorf("track = %d, want body height %d", track, m.bodyHeight())
	}
	if total != len(m.visible) {
		t.Errorf("total = %d, want display rows %d", total, len(m.visible))
	}
	if start != 0 {
		t.Errorf("thumb at top: start = %d", start)
	}
	if length < 1 || length >= track {
		t.Errorf("thumb length = %d, want within (0, %d)", length, track)
	}

	m.Scroll(m.maxTop())
	_, _, start, length, _ = m.scrollbarGeometry()
	if start+length != track {
		t.Errorf("thumb at bottom = %d+%d, want flush with track end %d", start, length, track)
	}
}

func TestScrollbarHiddenWithoutOverflow(t *testing.T) {
	m := sbModel(3, 40, 20)
	if _, _, _, _, ok := m.scrollbarGeometry(); ok {
		t.Error("fitting view should have no scrollbar")
	}
	if m.ScrollbarHit(39, 5) {
		t.Error("hit without a scrollbar")
	}
}

func TestScrollbarHit(t *testing.T) {
	m := sbModel(100, 40, 12)
	track := m.bodyHeight()

	if !m.ScrollbarHit(39, 1) || !m.ScrollbarHit(39, track) {
		t.Error("rightmost column within the track should hit")
	}
	if m.ScrollbarHit(38, 2) {
		t.Error("non-rightmost column should not hit")
	}
	if m.ScrollbarHit(39, 0) {
		t.Error("the title row should not hit")
	}
	if m.ScrollbarHit(39, track+1) {
		t.Error("the footer row should not hit")
	}
}

func TestScrollbarPressJumpAndDrag(t *testing.T) {
	m := sbModel(100, 40, 12)
	track, total, _, _, _ := m.scrollbarGeometry()

	// A press on the track's last row jumps to the bottom.
	if m.ScrollbarPress(track) { // content-local: title row + track-local track-1
		t.Fatal("track press should not start a drag")
	}
	if m.top != total-track {
		t.Errorf("bottom jump: top = %d, want %d", m.top, total-track)
	}

	// A press on the thumb (now parked at the end) starts a drag.
	_, _, start, _, _ := m.scrollbarGeometry()
	if !m.ScrollbarPress(start + 1) {
		t.Fatal("thumb press should start a drag")
	}

	// Dragging the grabbed thumb back to the top scrolls to offset 0.
	m.ScrollbarDrag(1)
	if m.top != 0 {
		t.Errorf("drag to top: top = %d, want 0", m.top)
	}
}

// sbModelWithRequest is sbModel but with an as-sent request snapshot
// attached (#1832), growing the header to two rows (#2424) — the scrollbar
// hit-testing must follow the real header height, not a hard-coded one
// (#2450).
func sbModelWithRequest(n, w, h int) Model {
	body := make([]string, n)
	for i := range body {
		body[i] = "line"
	}
	m := New(nil)
	m.SetSize(w, h)
	m.Set("req", &httpclient.Response{
		Status:     "200 OK",
		StatusCode: 200,
		Proto:      "HTTP/1.1",
		Headers:    http.Header{"Content-Type": {"text/plain"}},
		Body:       []byte(strings.Join(body, "\n")),
		Duration:   time.Millisecond,
		Request:    &httpclient.RequestSnapshot{Method: "GET", URL: "https://example.test/r"},
	})
	return m
}

// TestScrollbarHitWithRequestLine mirrors TestScrollbarHit with a two-row
// header: the track starts one row further down and hit-testing must move
// with it (#2450).
func TestScrollbarHitWithRequestLine(t *testing.T) {
	m := sbModelWithRequest(100, 40, 12)
	if got, want := m.headerLineCount(), 2; got != want {
		t.Fatalf("headerLineCount = %d, want %d", got, want)
	}
	track := m.bodyHeight()

	if !m.ScrollbarHit(39, 2) || !m.ScrollbarHit(39, 1+track) {
		t.Error("rightmost column within the track should hit")
	}
	if m.ScrollbarHit(39, 1) {
		t.Error("the request-line row should not hit")
	}
	if m.ScrollbarHit(39, 0) {
		t.Error("the title row should not hit")
	}
	if m.ScrollbarHit(39, 2+track) {
		t.Error("the footer row should not hit")
	}
}

// TestScrollbarPressJumpAndDragWithRequestLine mirrors
// TestScrollbarPressJumpAndDrag with the two-row header (#2450).
func TestScrollbarPressJumpAndDragWithRequestLine(t *testing.T) {
	m := sbModelWithRequest(100, 40, 12)
	track, total, _, _, _ := m.scrollbarGeometry()

	if m.ScrollbarPress(1 + track) {
		t.Fatal("track press should not start a drag")
	}
	if m.top != total-track {
		t.Errorf("bottom jump: top = %d, want %d", m.top, total-track)
	}

	_, _, start, _, _ := m.scrollbarGeometry()
	if !m.ScrollbarPress(1 + start + 1) {
		t.Fatal("thumb press should start a drag")
	}

	m.ScrollbarDrag(2)
	if m.top != 0 {
		t.Errorf("drag to top: top = %d, want 0", m.top)
	}
}

func TestScrollbarRendered(t *testing.T) {
	m := sbModel(100, 40, 12)
	if !strings.Contains(m.View(), "│") {
		t.Error("overflowing view should render the scrollbar track")
	}
	small := sbModel(3, 40, 20)
	if strings.Contains(small.View(), "│") {
		t.Error("fitting view should render no scrollbar")
	}
}
