package words

import (
	"testing"

	"ike/internal/complete"
	"ike/internal/lang"
)

// TestCommentContextOffersCurrentBufferOnly (#2654): a request in a comment
// answers with the current buffer's words and nothing from other buffers
// or the project tier — prose in a comment refers to the code around it.
func TestCommentContextOffersCurrentBufferOnly(t *testing.T) {
	s := New("")
	s.Observe(change("/a.go", "alpha // al"))
	s.Observe(change("/b.go", "alternate"))

	code := labels(t, s, complete.Request{Path: "/a.go", Line: 0, Col: 11})
	if len(code) != 2 || code[0] != "alpha" || code[1] != "alternate" {
		t.Fatalf("code context = %v, want [alpha alternate] (other buffers included)", code)
	}
	comment := labels(t, s, complete.Request{Path: "/a.go", Line: 0, Col: 11, Context: lang.CtxComment})
	if len(comment) != 1 || comment[0] != "alpha" {
		t.Fatalf("comment context = %v, want [alpha] (current buffer only)", comment)
	}
}

// TestWordsClaimCommentsOnly: the word index is dispatched in comments and
// nowhere else outside code — never inside a string literal or on an
// import line.
func TestWordsClaimCommentsOnly(t *testing.T) {
	var src complete.ContextSource = New("")
	for ctx, want := range map[lang.CompletionContext]bool{
		lang.CtxComment: true,
		lang.CtxString:  false,
		lang.CtxImport:  false,
	} {
		if got := src.CompletesIn(ctx); got != want {
			t.Errorf("CompletesIn(%q) = %v, want %v", ctx, got, want)
		}
	}
}
