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
	// Each carries a diagnostic — an empty set for a never-delivered path is
	// dropped before batching since #2402 and would not appear at all.
	for i := 0; i < 100; i++ {
		b.onDiagnostics(pathN(i), protocol.PublishDiagnosticsParams{
			Diagnostics: []protocol.Diagnostic{{Message: "first"}},
		}, []string{"x"}, "")
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

// TestDiagsCacheDropsRetractedPaths (#1543): an empty publish removes the
// path's key from the code-action diagnostics cache — servers publish for
// paths far outside any root (module caches), and empty-set keys would pile
// up for the whole session.
func TestDiagsCacheDropsRetractedPaths(t *testing.T) {
	b := &bridge{}
	b.onDiagnostics("/x/a.go", protocol.PublishDiagnosticsParams{
		Diagnostics: []protocol.Diagnostic{{Message: "boom"}},
	}, nil, "")
	if len(b.diags) != 1 {
		t.Fatalf("cache = %d paths, want 1", len(b.diags))
	}
	b.onDiagnostics("/x/a.go", protocol.PublishDiagnosticsParams{}, nil, "")
	if len(b.diags) != 0 {
		t.Fatalf("empty publish must drop the key, cache = %v", b.diags)
	}
	// Never-seen path with an empty publish never creates a key.
	b.onDiagnostics("/x/b.go", protocol.PublishDiagnosticsParams{}, nil, "")
	if len(b.diags) != 0 {
		t.Fatalf("empty publish for an unknown path must not create a key, cache = %v", b.diags)
	}
}

// TestNoChangeRepublishDropped (#2402): a server republishing an unchanged
// set — including "still empty" for a path never delivered — produces no
// message and no render pass; a real retraction still goes through once.
func TestNoChangeRepublishDropped(t *testing.T) {
	var mu sync.Mutex
	var batches []ilsp.DiagnosticsBatchMsg
	h := host.New(nil)
	h.SetSender(func(m tea.Msg) {
		if v, ok := m.(ilsp.DiagnosticsBatchMsg); ok {
			mu.Lock()
			batches = append(batches, v)
			mu.Unlock()
		}
	})
	b := &bridge{h: h}
	const p = "/x/a.go"
	publish := func(diags ...protocol.Diagnostic) {
		b.onDiagnostics(p, protocol.PublishDiagnosticsParams{Diagnostics: diags}, []string{"x"}, "")
	}
	waitBatches := func(want int, what string) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			n := len(batches)
			mu.Unlock()
			if n >= want {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(batches) != want {
			t.Fatalf("%s: %d batches, want %d", what, len(batches), want)
		}
	}

	// Empty publish for a never-delivered path: dropped entirely.
	publish()
	time.Sleep(3 * diagCoalesce)
	waitBatches(0, "empty publish for unknown path")

	// First real set: delivered.
	publish(protocol.Diagnostic{Message: "boom"})
	waitBatches(1, "first publish")

	// Identical republish: dropped.
	publish(protocol.Diagnostic{Message: "boom"})
	time.Sleep(3 * diagCoalesce)
	waitBatches(1, "identical republish")

	// Changed set: delivered.
	publish(protocol.Diagnostic{Message: "worse"})
	waitBatches(2, "changed set")

	// Retraction of a delivered set: delivered once...
	publish()
	waitBatches(3, "retraction")
	// ...and the repeat retraction is dropped again.
	publish()
	time.Sleep(3 * diagCoalesce)
	waitBatches(3, "repeat retraction")
}

// TestFileClosedDropsPerPathState (#1543): closing a file's last view releases
// every per-path bridge map entry and the completion cache it owns.
func TestFileClosedDropsPerPathState(t *testing.T) {
	const p = "/x/a.go"
	b := &bridge{
		diags:           map[string][]protocol.Diagnostic{p: {{Message: "boom"}}},
		sigActive:       map[string]bool{p: true},
		semInFlight:     map[string]bool{p: true},
		semPending:      map[string]bool{p: true},
		hintInFlight:    map[string]bool{p: true},
		hintPending:     map[string]bool{p: true},
		inheritInFlight: map[string]bool{p: true},
		inheritPending:  map[string]bool{p: true},
		inheritTimer:    map[string]*time.Timer{p: time.AfterFunc(time.Hour, func() {})},
		compItems:       []protocol.CompletionItem{{Label: "x"}},
		compPath:        p,
		compResolved:    map[int]bool{0: true},
	}
	b.fileClosed(p)
	for name, n := range map[string]int{
		"diags":           len(b.diags),
		"sigActive":       len(b.sigActive),
		"semInFlight":     len(b.semInFlight),
		"semPending":      len(b.semPending),
		"hintInFlight":    len(b.hintInFlight),
		"hintPending":     len(b.hintPending),
		"inheritInFlight": len(b.inheritInFlight),
		"inheritPending":  len(b.inheritPending),
		"inheritTimer":    len(b.inheritTimer),
		"compItems":       len(b.compItems),
		"compResolved":    len(b.compResolved),
	} {
		if n != 0 {
			t.Errorf("%s not released on fileClosed: %d entries", name, n)
		}
	}
	if b.compPath != "" {
		t.Errorf("compPath not cleared: %q", b.compPath)
	}
}

// TestReopenedPathDeliversUnchangedRepublish (#2492): a didOpen re-arms
// delivery for the path — after a project switch the fresh model's Problems
// store is empty while the bridge still remembers the delivered set, so the
// first republish must reach the app even when nothing changed (it also
// closes the switch op's warm-up phase). One delivery per open, then the
// #2402 suppression resumes.
func TestReopenedPathDeliversUnchangedRepublish(t *testing.T) {
	var mu sync.Mutex
	var batches []ilsp.DiagnosticsBatchMsg
	h := host.New(nil)
	h.SetSender(func(m tea.Msg) {
		if v, ok := m.(ilsp.DiagnosticsBatchMsg); ok {
			mu.Lock()
			batches = append(batches, v)
			mu.Unlock()
		}
	})
	b := &bridge{h: h}
	const p = "/x/a.go"
	publish := func(diags ...protocol.Diagnostic) {
		b.onDiagnostics(p, protocol.PublishDiagnosticsParams{Diagnostics: diags}, []string{"x"}, "")
	}
	waitBatches := func(want int, what string) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			n := len(batches)
			mu.Unlock()
			if n >= want {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(batches) != want {
			t.Fatalf("%s: %d batches, want %d", what, len(batches), want)
		}
	}

	publish(protocol.Diagnostic{Message: "boom"})
	waitBatches(1, "first publish")

	// The re-open marker (what fileOpened arms before the didOpen goes out):
	// the identical republish is delivered exactly once more.
	b.mu.Lock()
	b.deliverDiags = map[string]bool{p: true}
	b.mu.Unlock()
	publish(protocol.Diagnostic{Message: "boom"})
	waitBatches(2, "republish after re-open")

	// The marker is consumed: the next identical republish is dropped again.
	publish(protocol.Diagnostic{Message: "boom"})
	time.Sleep(3 * diagCoalesce)
	waitBatches(2, "identical republish after redelivery")

	// A still-empty publish for a never-delivered path is delivered too when
	// the path was just opened — the listening model must learn "clean file"
	// to close its warm-up wait.
	const q = "/x/b.go"
	b.mu.Lock()
	b.deliverDiags[q] = true
	b.mu.Unlock()
	b.onDiagnostics(q, protocol.PublishDiagnosticsParams{}, []string{"x"}, "")
	waitBatches(3, "empty publish for just-opened path")
}
