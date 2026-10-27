package lsp

import (
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// TestJinjaTemplateDiagnosticsFiltered (#3060): a stubbed server publish for
// a .yml.j2 buffer reports one diagnostic on the {% for %} tag and one on
// plain YAML; the set the bridge delivers — the one the app hands to both the
// Problems store and the editor gutter — holds only the second.
func TestJinjaTemplateDiagnosticsFiltered(t *testing.T) {
	var mu sync.Mutex
	var got []ilsp.Diagnostic
	delivered := false
	h := host.New(nil)
	h.SetSender(func(m tea.Msg) {
		mu.Lock()
		defer mu.Unlock()
		if v, ok := m.(ilsp.DiagnosticsBatchMsg); ok {
			for _, it := range v.Items {
				got, delivered = it.Diagnostics, true
			}
		}
	})
	b := &bridge{h: h}
	lines := []string{
		`items:`,
		`{% for x in xs %}`,
		`  - {{ x }}`,
		`{% endfor %}`,
		`port: oops`,
	}
	rng := func(line, sc, ec int) protocol.Range {
		return protocol.Range{
			Start: protocol.Position{Line: line, Character: sc},
			End:   protocol.Position{Line: line, Character: ec},
		}
	}
	b.onDiagnostics("/proj/app.yml.j2", protocol.PublishDiagnosticsParams{
		Diagnostics: []protocol.Diagnostic{
			{Range: rng(1, 0, 17), Message: "Implicit map keys need to be followed by map values"},
			{Range: rng(4, 6, 10), Message: "Incorrect type. Expected integer"},
		},
	}, lines, "")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := delivered
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if !delivered {
		t.Fatal("no diagnostics delivered")
	}
	if len(got) != 1 || got[0].Range.Start.Line != 4 {
		t.Fatalf("delivered %+v, want only the line-5 diagnostic", got)
	}
}
