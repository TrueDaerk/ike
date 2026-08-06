package langenv

import (
	"testing"

	"ike/internal/highlight"
	"ike/internal/lang"
	"ike/internal/secret"
)

// maskOn returns the mask span covering the given line, if any.
func maskOn(spans []lang.Span, line int) (lang.Span, bool) {
	for _, s := range spans {
		if s.Line == line && s.Capture == secret.Capture {
			return s, true
		}
	}
	return lang.Span{}, false
}

func TestMaskSuspectValues(t *testing.T) {
	lines := []string{
		"STRIPE_SECRET_KEY=sk_live_abc123",
		"export DB_PASSWORD = hunter2",
		"API_HOST=api.example.com",
	}
	spans := envSpans(lines)

	s, ok := maskOn(spans, 0)
	if !ok {
		t.Fatal("a *_SECRET_KEY value must be masked")
	}
	if got := lines[0][s.StartCol:s.EndCol]; got != "sk_live_abc123" {
		t.Errorf("mask covers %q, want the whole value", got)
	}
	if s.Replace != secret.Mask {
		t.Errorf("Replace = %q, want the mask stand-in", s.Replace)
	}

	s, ok = maskOn(spans, 1)
	if !ok {
		t.Fatal("an exported password must be masked too")
	}
	if got := lines[1][s.StartCol:s.EndCol]; got != "hunter2" {
		t.Errorf("mask covers %q, want the value after the spaced =", got)
	}

	if _, ok := maskOn(spans, 2); ok {
		t.Error("a plain host value must stay readable")
	}
}

// TestMaskWinsOverValueStyle: overlapping spans resolve first-covering-wins,
// so the mask span must be emitted ahead of the value's own string span —
// otherwise the conceal layer never sees it as the covering capture.
func TestMaskWinsOverValueStyle(t *testing.T) {
	spans := envSpans([]string{"API_TOKEN=abc"})
	for _, s := range spans {
		if s.Line != 0 || s.StartCol > 10 || s.EndCol <= 10 {
			continue
		}
		if s.Capture != secret.Capture {
			t.Fatalf("first span covering the value is %q, want %q", s.Capture, secret.Capture)
		}
		return
	}
	t.Fatal("no span covers the value")
}

// TestMaskThroughHighlightPipeline: the mask and the lint notes reach the
// editor through the registry, not only through the local functions.
func TestMaskThroughHighlightPipeline(t *testing.T) {
	lines := []string{"API_TOKEN=abc", "API_TOKEN=def"}
	found := false
	for _, s := range highlight.Highlight("/p/.env", lines) {
		if s.Line == 0 && s.Capture == secret.Capture && s.Replace == secret.Mask {
			found = true
		}
	}
	if !found {
		t.Error("the highlight pipeline must carry the mask span for a .env path")
	}
	if notes := highlight.Lint("/p/.env", lines); len(notes) != 1 {
		t.Errorf("highlight.Lint returned %d notes, want the one shadowed key", len(notes))
	}
}

func TestMaskSkipsEmptyAndKeylessLines(t *testing.T) {
	spans := envSpans([]string{"API_TOKEN=", "# API_TOKEN=abc", "BARE_SECRET"})
	for li := range 3 {
		if _, ok := maskOn(spans, li); ok {
			t.Errorf("line %d must not carry a mask", li)
		}
	}
}
