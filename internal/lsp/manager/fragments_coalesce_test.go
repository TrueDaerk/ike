package manager

import (
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ike/internal/highlight"
	"ike/internal/lsp/protocol"
)

// Fragment detection coalesces per host (#2770): a burst of changes while a
// detection run is in flight re-detects once from the latest text after that
// run, not once per change.
func TestFragmentSyncCoalescesBurst(t *testing.T) {
	opens := make(chan protocol.DidOpenTextDocumentParams, 8)
	changes := make(chan protocol.DidChangeTextDocumentParams, 64)
	m := New(multiResolver(fragmentSpecs()...), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, didOpens: opens, didChanges: changes}), Callbacks{})
	defer m.Shutdown()

	var runs atomic.Int32
	release := make(chan struct{})
	var lastSeen atomic.Value
	m.SetFragmentDetector(func(lang string, lines []string) []highlight.Fragment {
		if runs.Add(1) == 2 {
			// The second run blocks: the burst below lands while it is in
			// flight.
			<-release
		}
		lastSeen.Store(strings.Join(lines, "\n"))
		return lineDetector(lang, lines)
	})

	path := filepath.Join(t.TempDir(), "app.py")
	if err := m.Open(path, "python", "sql>SELECT 1"); err != nil {
		t.Fatal(err)
	}
	waitOpen(t, opens, func(p protocol.DidOpenTextDocumentParams) bool {
		return isFragmentURI(p.TextDocument.URI)
	})
	waitRuns := func(n int32) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for runs.Load() < n {
			if time.Now().After(deadline) {
				t.Fatalf("detector ran %d times, want %d", runs.Load(), n)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	waitRuns(1)

	// The second change starts the run that blocks; the rest of the burst
	// arrives while it is in flight.
	for i := 2; i <= 6; i++ {
		if err := m.Change(path, "sql>SELECT "+strings.Repeat("2", i)); err != nil {
			t.Fatal(err)
		}
	}
	waitRuns(2)
	if got := runs.Load(); got != 2 {
		t.Fatalf("detector ran %d times during the burst, want 2 (one in flight)", got)
	}
	close(release)
	waitRuns(3)
	// Let a hypothetical per-change run surface before asserting.
	time.Sleep(50 * time.Millisecond)
	if got := runs.Load(); got != 3 {
		t.Fatalf("detector ran %d times after the burst, want 3 (blocked run + one catch-up)", got)
	}
	if got := lastSeen.Load(); got != "sql>SELECT 222222" {
		t.Fatalf("catch-up run saw %q, want the latest text", got)
	}
}
