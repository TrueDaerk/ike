package complete

import (
	"context"
	"sync"
	"testing"
	"time"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
)

// langoverride_test.go covers #2048: a buffer with no file that was given a
// language through "Treat Buffer as …" must reach the sources as a buffer of
// that language, keyed by its own identity rather than the empty path.

// recordingSource captures the request it was dispatched with.
type recordingSource struct {
	mu  sync.Mutex
	got Request
}

func (*recordingSource) Name() string  { return "rec" }
func (*recordingSource) Priority() int { return ilsp.PriorityWords }
func (r *recordingSource) Complete(_ context.Context, req Request) ([]ilsp.CompletionItem, error) {
	r.mu.Lock()
	r.got = req
	r.mu.Unlock()
	return []ilsp.CompletionItem{{Label: "x", InsertText: "x"}}, nil
}

func (r *recordingSource) request() Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.got
}

// bufferTrigger is the completion trigger a file-less buffer treated as Go
// emits: no path, its own view key, the language's synthetic name.
func bufferTrigger(char string) host.EditorEvent {
	return host.EditorEvent{
		Kind: host.EditorCompletionTrigger,
		Key:  "\x00buffer/7", LangPath: "buffer.go",
		Line: 1, Col: 2, Char: char,
	}
}

// TestRequestCarriesBufferKeyAndLanguage: the engine hands the source the
// buffer's identity and its language name, and tags the batch with the key so
// it can be routed back to a view no path could find.
func TestRequestCarriesBufferKeyAndLanguage(t *testing.T) {
	e, ch := newTestEngine()
	rec := &recordingSource{}
	e.Register(rec)
	e.Emit(bufferTrigger("a"))
	got := collect(t, ch, 1)

	req := rec.request()
	if req.BufKey() != "\x00buffer/7" {
		t.Errorf("BufKey() = %q, want the view key", req.BufKey())
	}
	if req.LangName() != "buffer.go" {
		t.Errorf("LangName() = %q, want buffer.go", req.LangName())
	}
	if req.Path != "" {
		t.Errorf("Path = %q, want empty — the buffer has no file", req.Path)
	}
	if got[0].Key != "\x00buffer/7" || got[0].Path != "" {
		t.Errorf("batch route = %+v, want the key set and the path empty", got[0])
	}
	if got[0].RouteKey() != "\x00buffer/7" {
		t.Errorf("RouteKey() = %q, want the view key", got[0].RouteKey())
	}
}

// TestRequestFallsBackToPath: a file buffer sets neither field, and both
// accessors answer with the path — the pre-#2048 behaviour of every source.
func TestRequestFallsBackToPath(t *testing.T) {
	e, ch := newTestEngine()
	rec := &recordingSource{}
	e.Register(rec)
	e.Emit(trigger("a"))
	got := collect(t, ch, 1)

	req := rec.request()
	if req.BufKey() != "/f.go" || req.LangName() != "/f.go" {
		t.Errorf("request = %+v, want both accessors to answer /f.go", req)
	}
	if got[0].RouteKey() != "/f.go" {
		t.Errorf("RouteKey() = %q, want /f.go", got[0].RouteKey())
	}
}

// TestExclusiveClaimFollowsTheBufferLanguage: an exclusive source claims by
// language name, so a file-less buffer treated as its language is claimed the
// same way one of its files is — and a plain buffer is not.
func TestExclusiveClaimFollowsTheBufferLanguage(t *testing.T) {
	e, ch := newTestEngine()
	e.Register(fakeSource{name: "words", prio: ilsp.PriorityWords})
	e.Register(exclusiveFake{fakeSource{name: "http", prio: ilsp.PriorityEmmet}})

	e.Emit(host.EditorEvent{
		Kind: host.EditorCompletionTrigger,
		Key:  "\x00buffer/3", LangPath: "buffer.http",
		Line: 1, Col: 2, Char: "a",
	})
	got := collect(t, ch, 1)
	if got[0].Source != "http" {
		t.Fatalf("batch source = %q, want the claiming source", got[0].Source)
	}
	select {
	case m := <-ch:
		t.Fatalf("a buffer treated as .http must not dispatch %q", m.Source)
	case <-time.After(150 * time.Millisecond):
	}

	// Plain Text again: no claim, the full merged popup is back.
	e.Emit(host.EditorEvent{
		Kind: host.EditorCompletionTrigger,
		Key:  "\x00buffer/3",
		Line: 1, Col: 2, Char: "a",
	})
	if len(collect(t, ch, 2)) != 2 {
		t.Fatal("a typeless buffer must dispatch every source again")
	}
}
