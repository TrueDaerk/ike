package httppane

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"ike/internal/highlight"
	"ike/internal/httpclient"
)

// foldViewer composes a response whose JSON body folds: rows 0 status,
// 1 Content-Type, 2 blank, then the body. The fold ranges are injected rather
// than parsed so the test states the geometry it asserts on.
func foldViewer(t *testing.T) *Model {
	t.Helper()
	m := New(nil)
	m.SetSize(120, 20)
	m.Set("r", &httpclient.Response{
		Status: "200 OK", StatusCode: 200, Proto: "HTTP/1.1",
		Headers:  http.Header{"Content-Type": {"application/json"}},
		Body:     []byte("{\n  \"mapping\": {\n    \"a\": 1,\n    \"b\": 2\n  },\n  \"tail\": 3\n}"),
		Duration: time.Millisecond,
	})
	start := bodyRow(&m, "{")
	if start < 0 {
		t.Fatalf("body not composed: %v", m.rows)
	}
	// The "mapping" object spans the body's lines 1..4 — its header row plus
	// three hidden rows.
	m.setFolds([]highlight.Fold{{HeaderLine: 1, EndLine: 4}}, start)
	return &m
}

// foldViewerWithRequest is foldViewer but with an as-sent request snapshot
// attached (#1832), growing the header to two rows (#2424) — fold hit
// testing must follow the real header height (#2450).
func foldViewerWithRequest(t *testing.T) *Model {
	t.Helper()
	m := New(nil)
	m.SetSize(120, 20)
	m.Set("r", &httpclient.Response{
		Status: "200 OK", StatusCode: 200, Proto: "HTTP/1.1",
		Headers:  http.Header{"Content-Type": {"application/json"}},
		Body:     []byte("{\n  \"mapping\": {\n    \"a\": 1,\n    \"b\": 2\n  },\n  \"tail\": 3\n}"),
		Duration: time.Millisecond,
		Request:  &httpclient.RequestSnapshot{Method: "GET", URL: "https://example.test/r"},
	})
	start := bodyRow(&m, "{")
	if start < 0 {
		t.Fatalf("body not composed: %v", m.rows)
	}
	m.setFolds([]highlight.Fold{{HeaderLine: 1, EndLine: 4}}, start)
	return &m
}

// mappingRows is the fold's content as SelectionText should return it.
const mappingRows = "  \"mapping\": {\n    \"a\": 1,\n    \"b\": 2\n  },"

func TestSelectionOverCollapsedFoldCopiesHiddenRows(t *testing.T) {
	m := foldViewer(t)
	header := bodyRow(m, "  \"mapping\": {")
	if header < 0 {
		t.Fatalf("fold header row not found: %v", m.rows)
	}
	if !m.ToggleFold(header) {
		t.Fatal("collapsing the fold failed")
	}
	if _, ok := m.FoldedAt(header); !ok {
		t.Fatalf("fold at row %d not collapsed", header)
	}

	// Select exactly the one row the collapsed fold shows.
	m.sel = selection{on: true, anchor: pos{header, 0}, head: pos{header, len([]rune(m.rows[header].text))}}
	if got := m.SelectionText(); got != mappingRows {
		t.Fatalf("selection over a collapsed fold = %q, want %q", got, mappingRows)
	}
}

// TestSelectionOverOpenFoldUnchanged is the counter-case: an open fold's
// header row is an ordinary row.
func TestSelectionOverOpenFoldUnchanged(t *testing.T) {
	m := foldViewer(t)
	header := bodyRow(m, "  \"mapping\": {")
	m.sel = selection{on: true, anchor: pos{header, 0}, head: pos{header, len([]rune(m.rows[header].text))}}
	if got := m.SelectionText(); got != "  \"mapping\": {" {
		t.Fatalf("selection over an open fold header = %q, want just the header", got)
	}
}

// TestSelectionSpanningPastCollapsedFoldUnchanged guards the behaviour that
// already worked: the rows live in the full `rows` slice, so a selection
// reaching past the fold copies its hidden rows and nothing more.
func TestSelectionSpanningPastCollapsedFoldUnchanged(t *testing.T) {
	m := foldViewer(t)
	header := bodyRow(m, "  \"mapping\": {")
	m.ToggleFold(header)
	tail := bodyRow(m, "  \"tail\": 3")
	m.sel = selection{on: true, anchor: pos{header, 0}, head: pos{tail, len([]rune(m.rows[tail].text))}}
	want := mappingRows + "\n  \"tail\": 3"
	if got := m.SelectionText(); got != want {
		t.Fatalf("selection past a collapsed fold = %q, want %q", got, want)
	}
}

// TestCopyKeyOverCollapsedFoldEmitsFullContent walks the real copy path
// (`y` / cmd+c) rather than calling SelectionText directly.
func TestCopyKeyOverCollapsedFoldEmitsFullContent(t *testing.T) {
	m := foldViewer(t)
	header := bodyRow(m, "  \"mapping\": {")
	m.ToggleFold(header)
	m.sel = selection{on: true, anchor: pos{header, 0}, head: pos{header, len([]rune(m.rows[header].text))}}

	cmd := m.handleKey(keyPress("y"))
	if cmd == nil {
		t.Fatal("y with a selection should emit a copy command")
	}
	msg, ok := cmd().(CopyMsg)
	if !ok {
		t.Fatalf("copy message = %T, want CopyMsg", cmd())
	}
	if msg.Text != mappingRows {
		t.Fatalf("copied %q, want %q", msg.Text, mappingRows)
	}
	if !strings.Contains(msg.What, "selection") {
		t.Fatalf("copy label = %q, want the selection label", msg.What)
	}
}

// TestExpandFoldedStopsWithoutCollapsedFold is the fast path: nothing
// collapsed, nothing grows.
func TestExpandFoldedStopsWithoutCollapsedFold(t *testing.T) {
	m := foldViewer(t)
	if got := m.expandFolded(0, 2); got != 2 {
		t.Fatalf("expandFolded without a collapsed fold = %d, want 2", got)
	}
}
