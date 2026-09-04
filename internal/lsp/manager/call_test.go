package manager

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"ike/internal/lsp"
	"ike/internal/lsp/client"
	"ike/internal/lsp/protocol"
)

// callManager opens one Go document on a fake server and returns the manager
// plus the document path, so the generic forwarder can be exercised directly.
func callManager(t *testing.T, opts fakeOpts) (*Manager, string) {
	t.Helper()
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnectorOpts(opts), Callbacks{})
	t.Cleanup(m.Shutdown)

	path := filepath.Join(t.TempDir(), "main.go")
	if err := m.Open(path, "go", "package main\n"); err != nil {
		t.Fatal(err)
	}
	return m, path
}

func TestCallSkipsUnknownDocument(t *testing.T) {
	m, _ := callManager(t, fakeOpts{syncKind: protocol.SyncFull})

	called := false
	got, err := call(m, context.Background(), "/nowhere/main.go", func(client.Capabilities) bool { return true },
		func(context.Context, *server, *document) ([]protocol.Location, error) {
			called = true
			return []protocol.Location{{URI: "file:///x"}}, nil
		})
	if err != nil || got != nil {
		t.Fatalf("unknown document should no-op, got %v / %v", got, err)
	}
	if called {
		t.Fatal("do must not run without a document")
	}
}

func TestCallSkipsMissingCapability(t *testing.T) {
	m, path := callManager(t, fakeOpts{syncKind: protocol.SyncFull})

	called := false
	got, err := call(m, context.Background(), path, func(client.Capabilities) bool { return false },
		func(context.Context, *server, *document) ([]protocol.Location, error) {
			called = true
			return []protocol.Location{{URI: "file:///x"}}, nil
		})
	if err != nil || got != nil {
		t.Fatalf("missing capability should no-op, got %v / %v", got, err)
	}
	if called {
		t.Fatal("do must not run — a request the server cannot answer would hang until the timeout")
	}
}

func TestCallZeroValueIsNotSliceOnly(t *testing.T) {
	m, path := callManager(t, fakeOpts{syncKind: protocol.SyncFull})

	got, err := call(m, context.Background(), path, func(client.Capabilities) bool { return false },
		func(context.Context, *server, *document) (int, error) { return 42, nil })
	if err != nil || got != 0 {
		t.Fatalf("gated call should return the zero value, got %v / %v", got, err)
	}
}

func TestCallPassesCapabilitiesServerAndDocument(t *testing.T) {
	m, path := callManager(t, fakeOpts{syncKind: protocol.SyncFull})

	var seen client.Capabilities
	_, err := call(m, context.Background(), path, func(c client.Capabilities) bool {
		seen = c
		return c.Implementation
	}, func(_ context.Context, srv *server, doc *document) (int, error) {
		if srv == nil || srv.cl == nil {
			t.Fatal("do should receive the resolved server")
		}
		if doc == nil || len(doc.lines) == 0 {
			t.Fatalf("do should receive the synced document, got %+v", doc)
		}
		return 1, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !seen.Implementation {
		t.Fatal("capable should see the negotiated capabilities")
	}
}

func TestCallAppliesRequestTimeout(t *testing.T) {
	m, path := callManager(t, fakeOpts{syncKind: protocol.SyncFull})

	_, err := call(m, context.Background(), path, func(client.Capabilities) bool { return true },
		func(ctx context.Context, _ *server, _ *document) (int, error) {
			dl, ok := ctx.Deadline()
			if !ok {
				t.Fatal("do should run under a deadline")
			}
			if d := time.Until(dl); d <= 0 || d > requestTimeout {
				t.Fatalf("deadline should be within requestTimeout, got %v", d)
			}
			return 0, nil
		})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCallForwardsError(t *testing.T) {
	m, path := callManager(t, fakeOpts{syncKind: protocol.SyncFull})

	want := errors.New("boom")
	got, err := call(m, context.Background(), path, func(client.Capabilities) bool { return true },
		func(context.Context, *server, *document) ([]protocol.Location, error) { return nil, want })
	if !errors.Is(err, want) {
		t.Fatalf("error should pass through verbatim, got %v", err)
	}
	if got != nil {
		t.Fatalf("failed call should return no result, got %v", got)
	}
}

// TestCallCancelsAfterReturn pins the deferred cancel: the context handed to do
// is done once call returned, so no timer outlives the request.
func TestCallCancelsAfterReturn(t *testing.T) {
	m, path := callManager(t, fakeOpts{syncKind: protocol.SyncFull})

	var inner context.Context
	if _, err := call(m, context.Background(), path, func(client.Capabilities) bool { return true },
		func(ctx context.Context, _ *server, _ *document) (int, error) {
			inner = ctx
			return 0, nil
		}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-inner.Done():
	default:
		t.Fatal("the request context should be cancelled once call returned")
	}
}
