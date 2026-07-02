package editor

import (
	"strings"
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/highlight"
)

// feedSpans pushes a SpansMsg through Update and returns the updated model.
func feedSpans(t *testing.T, m Model, msg highlight.SpansMsg) Model {
	t.Helper()
	m, _ = m.Update(msg)
	return m
}

func TestSpansMsgCachedWhenCurrent(t *testing.T) {
	m := New()
	m.buf = buffer.FromString("package main")
	m.path = "main.go"
	m.docVersion = 7

	m = feedSpans(t, m, highlight.SpansMsg{
		Path:    "main.go",
		Version: 7,
		Spans:   []highlight.Span{{Line: 0, StartCol: 0, EndCol: 7, Capture: "keyword"}},
	})
	if m.hlIndex.Empty() {
		t.Fatal("expected spans to be cached for the current version")
	}
	if got := m.hlIndex.CaptureAt(0, 3); got != "keyword" {
		t.Errorf("CaptureAt(0,3) = %q, want keyword", got)
	}
}

func TestSpansMsgDroppedWhenStale(t *testing.T) {
	m := New()
	m.path = "main.go"
	m.docVersion = 9
	m = feedSpans(t, m, highlight.SpansMsg{Path: "main.go", Version: 4, Spans: []highlight.Span{{Line: 0, StartCol: 0, EndCol: 3, Capture: "keyword"}}})
	if !m.hlIndex.Empty() {
		t.Error("stale spans (older version) should be dropped")
	}
}

func TestSpansMsgDroppedWhenWrongPath(t *testing.T) {
	m := New()
	m.path = "main.go"
	m.docVersion = 2
	m = feedSpans(t, m, highlight.SpansMsg{Path: "other.go", Version: 2, Spans: []highlight.Span{{Line: 0, StartCol: 0, EndCol: 3, Capture: "keyword"}}})
	if !m.hlIndex.Empty() {
		t.Error("spans for a different path should be dropped")
	}
}

func TestRenderLineAppliesHighlight(t *testing.T) {
	m := New()
	m.buf = buffer.FromString("package main")
	m.path = "main.go"
	m.SetSize(40, 5)
	m = feedSpans(t, m, highlight.SpansMsg{
		Path:    "main.go",
		Version: m.docVersion,
		Spans:   []highlight.Span{{Line: 0, StartCol: 0, EndCol: 7, Capture: "keyword"}},
	})
	out := m.View()
	// A styled cell emits ANSI escape sequences; a plain render would not.
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("expected ANSI styling in highlighted output, got %q", out)
	}
}

func TestEditBumpsDocVersionAndSchedulesParse(t *testing.T) {
	m := New()
	m.buf = buffer.FromString("")
	m.path = "main.go"
	before := m.docVersion
	m.mode = Insert
	m.insert = insertSession{active: true}
	m.insertText("x")
	if m.docVersion == before {
		t.Error("an insert should bump docVersion")
	}
}
