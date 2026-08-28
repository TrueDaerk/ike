package lsp

import (
	"testing"
	"time"
)

// TestRefreshRateLimit (#2193): a server spamming workspace/*/refresh must not
// drive re-request rounds at wire speed — a burst collapses to one leading
// round plus one trailing round, and the limiter releases afterwards.
func TestRefreshRateLimit(t *testing.T) {
	b := &bridge{}
	for i := 0; i < 25; i++ {
		b.onRefresh("semanticTokens")
	}
	b.mu.Lock()
	cooling, pending := b.refreshCooling["semanticTokens"], b.refreshPending["semanticTokens"]
	// Kinds are limited independently.
	otherCooling := b.refreshCooling["codeLens"]
	b.mu.Unlock()
	if !cooling {
		t.Fatal("a refresh burst must arm the cooldown")
	}
	if !pending {
		t.Fatal("refreshes during the cooldown must coalesce into one pending round")
	}
	if otherCooling {
		t.Fatal("an untouched kind must not be cooling")
	}
	// The trailing round runs after one interval and, with nothing further
	// pending, the limiter releases after the next.
	deadline := time.Now().Add(3 * refreshMinInterval)
	for {
		b.mu.Lock()
		cooling = b.refreshCooling["semanticTokens"]
		b.mu.Unlock()
		if !cooling {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the limiter must release after the trailing round")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// A refresh after release runs a fresh leading round immediately (cooldown
	// re-armed, nothing pending).
	b.onRefresh("semanticTokens")
	b.mu.Lock()
	cooling, pending = b.refreshCooling["semanticTokens"], b.refreshPending["semanticTokens"]
	b.mu.Unlock()
	if !cooling || pending {
		t.Fatalf("post-release refresh: cooling=%v pending=%v, want true/false", cooling, pending)
	}
}
