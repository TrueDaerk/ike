package postfix

import (
	"context"
	"testing"

	"ike/internal/complete"
)

// TestCompleteFollowsEffectiveLanguage (#2652): templates come from the
// request's effective language, so a fence of a language with postfix
// templates inside a buffer whose own name has none still offers them —
// and the token fallback detects the expression where the host grammar
// cannot.
func TestCompleteFollowsEffectiveLanguage(t *testing.T) {
	regLang(t, "pfeff", goTemplates)
	s := feed("/x/README.md", "```pfeff\nerr.\n```\n")
	items, err := s.Complete(context.Background(), complete.Request{Path: "/x/README.md", Line: 1, Col: 4, Char: ".", Lang: "pfpfeff"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != len(goTemplates) {
		t.Fatalf("got %d items, want the fence language's %d templates", len(items), len(goTemplates))
	}
	// Without the effective language the buffer's own (unregistered) name
	// answers nothing.
	items, _ = s.Complete(context.Background(), complete.Request{Path: "/x/README.md", Line: 1, Col: 4, Char: "."})
	if len(items) != 0 {
		t.Fatalf("an unregistered buffer language must offer nothing, got %d", len(items))
	}
}
