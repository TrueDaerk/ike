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

// TestDiagnosticsCoalesceIntoOneBatch verifies a publish storm across many files
// folds into a single DiagnosticsBatchMsg holding the latest set per path (#597)
// — one Update pass + re-render instead of one per file.
func TestDiagnosticsCoalesceIntoOneBatch(t *testing.T) {
	var mu sync.Mutex
	var batches []ilsp.DiagnosticsBatchMsg
	singles := 0
	h := host.New(nil)
	h.SetSender(func(m tea.Msg) {
		mu.Lock()
		defer mu.Unlock()
		switch v := m.(type) {
		case ilsp.DiagnosticsBatchMsg:
			batches = append(batches, v)
		case ilsp.DiagnosticsMsg:
			singles++
		}
	})
	b := &bridge{h: h}

	// A burst: 100 distinct library files, plus a re-publish of one of them.
	for i := 0; i < 100; i++ {
		b.onDiagnostics(pathN(i), protocol.PublishDiagnosticsParams{}, nil, "")
	}
	b.onDiagnostics(pathN(0), protocol.PublishDiagnosticsParams{
		Diagnostics: []protocol.Diagnostic{{Message: "latest"}},
	}, []string{"x"}, "")

	// Wait for the coalesce window to flush.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(batches)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if singles != 0 {
		t.Fatalf("expected no un-batched DiagnosticsMsg, got %d", singles)
	}
	if len(batches) != 1 {
		t.Fatalf("expected exactly 1 batch, got %d", len(batches))
	}
	// 100 distinct paths, the re-publish collapses onto its path (latest wins).
	if got := len(batches[0].Items); got != 100 {
		t.Fatalf("batch held %d items, want 100 (one per path)", got)
	}
	for _, it := range batches[0].Items {
		if it.Path == pathN(0) {
			if len(it.Diagnostics) != 1 || it.Diagnostics[0].Message != "latest" {
				t.Fatalf("re-published path did not keep the latest set: %+v", it.Diagnostics)
			}
		}
	}
}

func pathN(i int) string {
	return "/proj/.venv/lib/pkg/mod" + string(rune('a'+i%26)) + string(rune('0'+i/26)) + ".py"
}
