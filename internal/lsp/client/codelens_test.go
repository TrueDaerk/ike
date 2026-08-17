package client

import (
	"encoding/json"
	"testing"

	"ike/internal/lsp/protocol"
)

func TestCodeLensDecodes(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"textDocument/codeLens": func(json.RawMessage) any {
			// One resolved lens with a command, one unresolved lens carrying
			// only the opaque data token (#1912).
			return json.RawMessage(`[
				{"range":{"start":{"line":2,"character":0},"end":{"line":2,"character":5}},"command":{"title":"run test","command":"test.run","arguments":[1]}},
				{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"data":{"id":7}}
			]`)
		},
	})
	ctx, cancel := ctx2s()
	defer cancel()
	lenses, err := c.CodeLens(ctx, protocol.CodeLensParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(lenses) != 2 {
		t.Fatalf("lenses = %+v, want 2", lenses)
	}
	if lenses[0].Command == nil || lenses[0].Command.Command != "test.run" || lenses[0].Command.Title != "run test" {
		t.Errorf("first lens = %+v, want resolved test.run", lenses[0])
	}
	if lenses[1].Command != nil || string(lenses[1].Data) != `{"id":7}` {
		t.Errorf("second lens = %+v, want unresolved with data", lenses[1])
	}
}

func TestCodeLensNull(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"textDocument/codeLens": func(json.RawMessage) any { return nil },
	})
	ctx, cancel := ctx2s()
	defer cancel()
	lenses, err := c.CodeLens(ctx, protocol.CodeLensParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(lenses) != 0 {
		t.Fatalf("null result should yield no lenses, got %+v", lenses)
	}
}

// TestResolveCodeLensFillsCommand round-trips the unresolved lens and gets the
// command back; a null resolve answer keeps the input lens (#1912).
func TestResolveCodeLensFillsCommand(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"codeLens/resolve": func(params json.RawMessage) any {
			// Echo the lens back with the command filled in, data verbatim.
			var lens protocol.CodeLens
			_ = json.Unmarshal(params, &lens)
			lens.Command = &protocol.Command{Title: "3 references", Command: "lens.refs"}
			return lens
		},
	})
	ctx, cancel := ctx2s()
	defer cancel()
	in := protocol.CodeLens{
		Range: protocol.Range{Start: protocol.Position{Line: 4}, End: protocol.Position{Line: 4}},
		Data:  json.RawMessage(`"tok"`),
	}
	out, err := c.ResolveCodeLens(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Command == nil || out.Command.Command != "lens.refs" {
		t.Fatalf("resolved lens = %+v, want lens.refs command", out)
	}
	if out.Range.Start.Line != 4 || string(out.Data) != `"tok"` {
		t.Errorf("range/data must round-trip verbatim: %+v", out)
	}
}

func TestResolveCodeLensNullKeepsInput(t *testing.T) {
	c, _ := newClientWithFake(t, map[string]func(json.RawMessage) any{
		"codeLens/resolve": func(json.RawMessage) any { return nil },
	})
	ctx, cancel := ctx2s()
	defer cancel()
	in := protocol.CodeLens{Range: protocol.Range{Start: protocol.Position{Line: 1}}, Data: json.RawMessage(`1`)}
	out, err := c.ResolveCodeLens(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Range.Start.Line != 1 || string(out.Data) != `1` {
		t.Fatalf("null resolve should keep the input lens, got %+v", out)
	}
}

// TestParseCapabilitiesCodeLens gates the feature on the boolean-or-object
// provider field; only an options object can promise codeLens/resolve (#1912).
func TestParseCapabilitiesCodeLens(t *testing.T) {
	cases := []struct {
		raw         string
		want        bool
		wantResolve bool
	}{
		{`true`, true, false},
		{`{}`, true, false},
		{`{"resolveProvider":true}`, true, true},
		{`{"resolveProvider":false}`, true, false},
		{`false`, false, false},
		{``, false, false},
	}
	for _, tc := range cases {
		caps := parseCapabilities(protocol.ServerCapabilities{CodeLensProvider: json.RawMessage(tc.raw)})
		if caps.CodeLens != tc.want || caps.CodeLensResolve != tc.wantResolve {
			t.Errorf("provider %q: CodeLens = %v/%v, want %v/%v", tc.raw, caps.CodeLens, caps.CodeLensResolve, tc.want, tc.wantResolve)
		}
	}
}
