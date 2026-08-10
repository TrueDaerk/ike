package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// httpLoaded opens a small .http document in a 60-column view.
func httpLoaded(t *testing.T, content string) Model {
	t.Helper()
	path := filepath.Join(t.TempDir(), "req.http")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(60, 10)
	m.SetFocused(true)
	return m
}

const flightDoc = "### one\nGET http://localhost/a\n\n### two\nGET http://localhost/b\n"

// TestHTTPFlightMarkerRenders covers #1746: the app-pushed indicator shows at
// its request line and nowhere else.
func TestHTTPFlightMarkerRenders(t *testing.T) {
	m := httpLoaded(t, flightDoc)
	m.SetHTTPFlight(map[int]string{1: "⟳ 1.2s"})

	rows := strings.Split(ansi.Strip(m.View()), "\n")
	if len(rows) < 5 {
		t.Fatalf("view rows: %d", len(rows))
	}
	if !strings.Contains(rows[1], "⟳ 1.2s") {
		t.Errorf("request line must carry the indicator: %q", rows[1])
	}
	if !strings.Contains(rows[1], "GET http://localhost/a") {
		t.Errorf("the request text must survive the annotation: %q", rows[1])
	}
	for i, row := range rows {
		if i != 1 && strings.Contains(row, "⟳") {
			t.Errorf("row %d must stay unmarked: %q", i, row)
		}
	}
}

// TestHTTPFlightMarkersIndependent: two running requests are marked on their
// own lines, and one clearing leaves the other in place (#1746).
func TestHTTPFlightMarkersIndependent(t *testing.T) {
	m := httpLoaded(t, flightDoc)
	m.SetHTTPFlight(map[int]string{1: "⟳ 1.2s", 4: "⟳ 340ms"})

	rows := strings.Split(ansi.Strip(m.View()), "\n")
	if !strings.Contains(rows[1], "⟳ 1.2s") || !strings.Contains(rows[4], "⟳ 340ms") {
		t.Fatalf("both markers expected:\n%q\n%q", rows[1], rows[4])
	}

	m.SetHTTPFlight(map[int]string{4: "⟳ 600ms"})
	rows = strings.Split(ansi.Strip(m.View()), "\n")
	if strings.Contains(rows[1], "⟳") {
		t.Errorf("the finished request must lose its marker: %q", rows[1])
	}
	if !strings.Contains(rows[4], "⟳ 600ms") {
		t.Errorf("the running request keeps a refreshed marker: %q", rows[4])
	}
}

// TestHTTPFlightMarkerClears: an empty set removes every marker from the view.
func TestHTTPFlightMarkerClears(t *testing.T) {
	m := httpLoaded(t, flightDoc)
	m.SetHTTPFlight(map[int]string{1: "⟳ 1.2s"})
	before := m.RenderVersion()
	m.SetHTTPFlight(nil)

	if _, ok := m.HTTPFlightAt(1); ok {
		t.Error("the marker must be gone")
	}
	if m.RenderVersion() == before {
		t.Error("clearing must invalidate the render cache, or the view keeps the old frame")
	}
	if strings.Contains(ansi.Strip(m.View()), "⟳") {
		t.Errorf("view must be free of indicators:\n%s", m.View())
	}
}

// TestHTTPFlightMarkerRepaintsOnRefresh: an unchanged set costs no repaint, a
// moved duration does — the 250 ms tick must actually reach the screen (#1746).
func TestHTTPFlightMarkerRepaintsOnRefresh(t *testing.T) {
	m := httpLoaded(t, flightDoc)
	m.SetHTTPFlight(map[int]string{1: "⟳ 1.2s"})
	stable := m.RenderVersion()
	m.SetHTTPFlight(map[int]string{1: "⟳ 1.2s"})
	if m.RenderVersion() != stable {
		t.Error("an identical set must not invalidate the cache")
	}
	m.SetHTTPFlight(map[int]string{1: "⟳ 1.4s"})
	if m.RenderVersion() == stable {
		t.Error("a moved duration must invalidate the cache")
	}
}

// TestHTTPFlightMarkerTruncatesLongLine: the indicator wins over the tail of a
// wide request line — a transient answer must not silently vanish (#1746).
func TestHTTPFlightMarkerTruncatesLongLine(t *testing.T) {
	long := "GET http://localhost/" + strings.Repeat("x", 80) + "\n"
	m := httpLoaded(t, "### one\n"+long)
	m.SetHTTPFlight(map[int]string{1: "⟳ 1.2s"})
	rows := strings.Split(ansi.Strip(m.View()), "\n")
	if !strings.Contains(rows[1], "⟳ 1.2s") {
		t.Errorf("the indicator must render on a full-width line: %q", rows[1])
	}
}
