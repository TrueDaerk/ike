package emmet

import (
	"context"
	"strings"
	"testing"

	"ike/internal/complete"
	"ike/internal/host"
)

func change(path, text string) host.EditorEvent {
	return host.EditorEvent{Kind: host.EditorChange, Path: path, Text: text}
}

func items(t *testing.T, s *Source, path, line string) []string {
	t.Helper()
	s.Observe(change(path, line))
	out, err := s.Complete(context.Background(), complete.Request{Path: path, Line: 0, Col: len([]rune(line))})
	if err != nil {
		t.Fatal(err)
	}
	inserts := make([]string, len(out))
	for i, it := range out {
		if !it.IsSnippet {
			t.Fatalf("emmet items must be snippets, got %+v", it)
		}
		inserts[i] = it.InsertText
	}
	return inserts
}

func TestCSSNumericShorthand(t *testing.T) {
	s := New()
	got := items(t, s, "/a.css", "m10")
	if len(got) != 1 || got[0] != "margin: 10px;$0" {
		t.Fatalf("m10 = %v", got)
	}
	if got := items(t, s, "/a.css", "fz14"); len(got) != 1 || got[0] != "font-size: 14px;$0" {
		t.Fatalf("fz14 = %v", got)
	}
	if got := items(t, s, "/a.css", "m0"); len(got) != 1 || got[0] != "margin: 0;$0" {
		t.Fatalf("m0 = %v", got)
	}
}

func TestCSSValuelessAndFixed(t *testing.T) {
	s := New()
	if got := items(t, s, "/a.scss", "bg"); len(got) != 1 || got[0] != "background: $1;$0" {
		t.Fatalf("bg = %v", got)
	}
	if got := items(t, s, "/a.css", "df"); len(got) != 1 || got[0] != "display: flex;$0" {
		t.Fatalf("df = %v", got)
	}
	if got := items(t, s, "/a.css", "zzz"); len(got) != 0 {
		t.Fatalf("unknown abbrev = %v, want none", got)
	}
}

func TestHTMLTagSnippets(t *testing.T) {
	s := New()
	got := items(t, s, "/a.html", "di")
	if len(got) != 1 || got[0] != "<div>$1</div>$0" {
		t.Fatalf("di = %v", got)
	}
	got = items(t, s, "/a.html", "ul")
	if len(got) != 1 || !strings.Contains(got[0], "<li>$1</li>") {
		t.Fatalf("ul = %v, want the list shape", got)
	}
	got = items(t, s, "/a.html", "im")
	if len(got) != 1 || got[0] != `<img src="$1" alt="$2">$0` {
		t.Fatalf("im = %v", got)
	}
}

func TestHTMLNotInsideAttributes(t *testing.T) {
	s := New()
	line := `<div class="di`
	s.Observe(change("/a.html", line))
	out, _ := s.Complete(context.Background(), complete.Request{Path: "/a.html", Line: 0, Col: len([]rune(line))})
	if len(out) != 0 {
		t.Fatalf("inside attribute value got %v, want none", out)
	}
}

func TestOtherLanguagesSilent(t *testing.T) {
	s := New()
	if got := items(t, s, "/a.go", "div"); len(got) != 0 {
		t.Fatalf("go file got %v, want none", got)
	}
}

func TestPreviewDetail(t *testing.T) {
	s := New()
	s.Observe(change("/a.html", "ul"))
	out, _ := s.Complete(context.Background(), complete.Request{Path: "/a.html", Line: 0, Col: 2})
	if len(out) != 1 || !strings.HasPrefix(out[0].Detail, "⌁ ") || strings.Contains(out[0].Detail, "\n") {
		t.Fatalf("detail preview = %q", out[0].Detail)
	}
}
