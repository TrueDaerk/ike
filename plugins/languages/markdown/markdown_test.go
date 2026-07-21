package langmarkdown

import (
	"testing"

	"ike/internal/lang"
)

// TestMarkdownRegistered guards #880: .md/.markdown resolve to markdown with
// the marksman server; the inline grammar registers as its own extension-less,
// server-less language for the injection overlay.
func TestMarkdownRegistered(t *testing.T) {
	for _, path := range []string{"/p/README.md", "/p/notes.markdown"} {
		l, ok := lang.ByPath(path)
		if !ok || l.ID != "markdown" {
			t.Errorf("%s → %v/%v, want markdown", path, l.ID, ok)
		}
	}
	l, _ := lang.ByID("markdown")
	if l.Server == nil || l.Server.Command != "marksman" {
		t.Errorf("server = %+v, want marksman", l.Server)
	}

	inline, ok := lang.ByID("markdown_inline")
	if !ok {
		t.Fatal("markdown_inline not registered")
	}
	if len(inline.Extensions) != 0 || inline.Server != nil {
		t.Errorf("markdown_inline must stay extension-less and server-less, got %+v", inline)
	}

	// HTML comments are the only comment syntax.
	_, block, ok := lang.Comments("/p/README.md")
	if !ok || block != [2]string{"<!--", "-->"} {
		t.Errorf("comments = %v/%v, want <!-- -->", block, ok)
	}
}
