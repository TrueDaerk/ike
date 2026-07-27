package httppane

import (
	"net/http"
	"strings"
	"testing"

	"ike/internal/httpclient"
)

// wide returns a response whose body is a single line far wider than the pane.
func wide() *httpclient.Response {
	return &httpclient.Response{
		Status:  "200 OK",
		Proto:   "HTTP/1.1",
		Headers: http.Header{"Content-Type": {"text/plain"}},
		Body:    []byte(strings.Repeat("ab", 40) + "TAIL"),
	}
}

func TestScrollXClampsToLongestRow(t *testing.T) {
	m := New(nil)
	m.SetSize(40, 20)
	m.Set("wide", wide())

	m.ScrollX(1000)
	want := 84 - 39 // body line runes minus the room after the gutter space
	if m.Left() != want {
		t.Errorf("left after overscroll: %d, want %d", m.Left(), want)
	}
	m.ScrollX(-1000)
	if m.Left() != 0 {
		t.Errorf("left after underscroll: %d, want 0", m.Left())
	}
}

func TestScrollXRevealsClippedTail(t *testing.T) {
	m := New(nil)
	m.SetSize(40, 20)
	m.Set("wide", wide())

	if strings.Contains(m.View(), "TAIL") {
		t.Fatalf("tail visible before panning:\n%s", m.View())
	}
	m.ScrollX(1000)
	if !strings.Contains(m.View(), "TAIL") {
		t.Errorf("tail still hidden after panning:\n%s", m.View())
	}
}

func TestArrowKeysPanAndHomeResets(t *testing.T) {
	m := New(nil)
	m.SetSize(40, 20)
	m.Set("wide", wide())

	m.Update(keyPress("right"))
	if m.Left() != hScrollStep {
		t.Errorf("left after right: %d, want %d", m.Left(), hScrollStep)
	}
	m.Update(keyPress("left"))
	if m.Left() != 0 {
		t.Errorf("left after left: %d, want 0", m.Left())
	}
	m.Update(keyPress("right"))
	m.Update(keyPress("0"))
	if m.Left() != 0 {
		t.Errorf("left after 0: %d, want 0", m.Left())
	}
	m.Update(keyPress("$"))
	if m.Left() == 0 {
		t.Error("$ did not pan to the right edge")
	}
	m.Update(keyPress("g"))
	if m.Left() != 0 {
		t.Errorf("left after g: %d, want 0", m.Left())
	}
}

func TestArrowKeysDoNotBrowseHistory(t *testing.T) {
	m := New(nil)
	m.SetSize(40, 20)
	m.Set("wide", wide())
	m.SetHistory([]HistoryItem{{Resp: wide()}, {Resp: sample()}})

	m.Update(keyPress("left"))
	if i, _ := m.HistoryIndex(); i != 0 {
		t.Errorf("history index after left arrow: %d, want 0", i)
	}
	m.Update(keyPress("h"))
	if i, _ := m.HistoryIndex(); i != 1 {
		t.Errorf("history index after h: %d, want 1", i)
	}
}

func TestComposeResetsHorizontalOffset(t *testing.T) {
	m := New(nil)
	m.SetSize(40, 20)
	m.Set("wide", wide())
	m.ScrollX(1000)

	m.Set("next", sample())
	if m.Left() != 0 {
		t.Errorf("left after a new response: %d, want 0", m.Left())
	}
}

func TestSelectionAccountsForPan(t *testing.T) {
	m := New(nil)
	m.SetSize(40, 20)
	m.Set("wide", wide())
	m.ScrollX(10)

	// The body row: status line, one header, blank, body.
	body := 3
	if m.rows[body].kind != kindBody {
		t.Fatalf("row %d is not the body row", body)
	}
	y := body - m.top + 1 // the title line occupies y == 0
	m.MousePress(1, y)
	m.MouseDrag(5, y)
	text := m.rows[body].text
	if got, want := m.SelectionText(), text[10:14]; got != want {
		t.Errorf("selection: %q, want %q", got, want)
	}
}

func TestSearchPansToMatch(t *testing.T) {
	m := New(nil)
	m.SetSize(40, 20)
	m.Set("wide", wide())

	m.Update(keyPress("/"))
	for _, r := range "TAIL" {
		m.Update(keyPress(string(r)))
	}
	m.Update(keyPress("enter"))
	if _, n := m.MatchPosition(); n != 1 {
		t.Fatalf("matches: %d, want 1", n)
	}
	if m.Left() == 0 {
		t.Error("view did not pan to the match")
	}
	if !strings.Contains(m.View(), "TAIL") {
		t.Errorf("match not visible:\n%s", m.View())
	}
}
