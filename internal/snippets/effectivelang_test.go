package snippets

import (
	"context"
	"testing"

	"ike/internal/complete"
)

// TestSourceFollowsEffectiveLanguage (#2652): the completion source scopes
// templates by the request's effective language, so a cursor inside a ```go
// fence of a Markdown buffer is offered the Go templates although the
// buffer's own name resolves to Markdown (or to nothing at all).
func TestSourceFollowsEffectiveLanguage(t *testing.T) {
	items, err := NewSource().Complete(context.Background(), complete.Request{Path: "/README.md", Lang: "go"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range items {
		if it.Label == "iferr" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Lang go inside a Markdown buffer must offer the Go templates, got %d items", len(items))
	}
	// Outside the fence the effective language is the buffer's: no Go.
	items, _ = NewSource().Complete(context.Background(), complete.Request{Path: "/README.md", Lang: "markdown"})
	for _, it := range items {
		if it.Label == "iferr" {
			t.Fatal("Lang markdown must not offer the Go templates")
		}
	}
}
