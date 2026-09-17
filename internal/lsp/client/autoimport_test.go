package client

import (
	"encoding/json"
	"testing"

	"ike/internal/lsp/protocol"
)

// TestInitializeAdvertisesCompletionResolveSupport guards #2610: the client
// announces labelDetailsSupport (pyright/vtsls name the auto-import module
// there) and resolveSupport for additionalTextEdits, so servers may defer the
// import edit to completionItem/resolve and IKE fetches it there. Resolve
// itself must ship the item's data token untouched.
func TestInitializeAdvertisesCompletionResolveSupport(t *testing.T) {
	var got protocol.InitializeParams
	var resolved protocol.CompletionItem
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"initialize": func(p json.RawMessage) any {
			_ = json.Unmarshal(p, &got)
			return protocol.InitializeResult{Capabilities: protocol.ServerCapabilities{
				CompletionProvider: &protocol.CompletionOptions{ResolveProvider: true},
			}}
		},
		"completionItem/resolve": func(p json.RawMessage) any {
			_ = json.Unmarshal(p, &resolved)
			return resolved
		},
	})
	ctx, cancel := ctx2s()
	defer cancel()
	if _, err := c.Initialize(ctx, InitParams{RootURI: "file:///tmp"}); err != nil {
		t.Fatal(err)
	}
	td := got.Capabilities.TextDocument
	if td == nil || td.Completion == nil || td.Completion.CompletionItem == nil {
		t.Fatalf("completion item capabilities missing: %+v", td)
	}
	ci := td.Completion.CompletionItem
	if !ci.LabelDetailsSupport {
		t.Error("labelDetailsSupport not advertised")
	}
	if ci.ResolveSupport == nil {
		t.Fatal("resolveSupport not advertised")
	}
	has := map[string]bool{}
	for _, p := range ci.ResolveSupport.Properties {
		has[p] = true
	}
	for _, p := range []string{"documentation", "detail", "additionalTextEdits"} {
		if !has[p] {
			t.Errorf("resolveSupport lacks %q: %v", p, ci.ResolveSupport.Properties)
		}
	}
	if !c.Caps().CompletionResolve {
		t.Fatal("resolveProvider not recorded")
	}
	item := protocol.CompletionItem{Label: "tldextract", Data: json.RawMessage(`{"token":1}`)}
	if _, err := c.Resolve(ctx, item); err != nil {
		t.Fatal(err)
	}
	if string(resolved.Data) != `{"token":1}` {
		t.Fatalf("server saw data %s, want the token round-tripped", resolved.Data)
	}
}
