package complete

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
)

// fakeSource answers with one item after delay (0 = instant).
type fakeSource struct {
	name  string
	prio  int
	delay time.Duration
}

func (f fakeSource) Name() string  { return f.name }
func (f fakeSource) Priority() int { return f.prio }
func (f fakeSource) Complete(ctx context.Context, req Request) ([]ilsp.CompletionItem, error) {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return []ilsp.CompletionItem{{Label: f.name + "-item", InsertText: f.name + "-item"}}, nil
}

func trigger(char string) host.EditorEvent {
	return host.EditorEvent{Kind: host.EditorCompletionTrigger, Path: "/f.go", Line: 1, Col: 2, Char: char}
}

func collect(t *testing.T, ch <-chan ilsp.CompletionMsg, n int) []ilsp.CompletionMsg {
	t.Helper()
	var out []ilsp.CompletionMsg
	for len(out) < n {
		select {
		case m := <-ch:
			out = append(out, m)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out after %d/%d batches", len(out), n)
		}
	}
	return out
}

func newTestEngine() (*Engine, chan ilsp.CompletionMsg) {
	ch := make(chan ilsp.CompletionMsg, 16)
	e := NewEngine(func(msg tea.Msg) {
		if cm, ok := msg.(ilsp.CompletionMsg); ok {
			ch <- cm
		}
	})
	return e, ch
}

// TestDispatchTagsAndFansOut: every source answers as its own tagged batch,
// fast ones unblocked by slow ones.
func TestDispatchTagsAndFansOut(t *testing.T) {
	e, ch := newTestEngine()
	e.Register(fakeSource{name: "words", prio: ilsp.PriorityWords})
	e.Register(fakeSource{name: "symbols", prio: ilsp.PrioritySymbols, delay: 50 * time.Millisecond})
	e.Emit(trigger("a"))
	got := collect(t, ch, 2)
	if got[0].Source != "words" {
		t.Fatalf("first batch = %q, want the instant source first", got[0].Source)
	}
	if got[1].Source != "symbols" || got[1].SourcePriority != ilsp.PrioritySymbols {
		t.Fatalf("second batch = %+v", got[1])
	}
	if got[0].Items[0].Source != "words" {
		t.Fatalf("items must carry the source tag, got %+v", got[0].Items[0])
	}
	if got[0].Line != 1 || got[0].Col != 2 || got[0].Path != "/f.go" {
		t.Fatalf("batch position = %+v", got[0])
	}
}

// TestTimeoutDropsSlowSource: a source outliving the engine timeout sends
// nothing; the fast source still answers.
func TestTimeoutDropsSlowSource(t *testing.T) {
	e, ch := newTestEngine()
	e.Timeout = 30 * time.Millisecond
	e.Register(fakeSource{name: "fast", prio: 1})
	e.Register(fakeSource{name: "slow", prio: 2, delay: time.Second})
	e.Emit(trigger("a"))
	got := collect(t, ch, 1)
	if got[0].Source != "fast" {
		t.Fatalf("batch = %q, want fast", got[0].Source)
	}
	select {
	case m := <-ch:
		t.Fatalf("slow source must be dropped, got %+v", m)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestNewDispatchCancelsPrevious: a fresh trigger cancels the in-flight
// dispatch, so its late results never arrive.
func TestNewDispatchCancelsPrevious(t *testing.T) {
	e, ch := newTestEngine()
	e.Register(fakeSource{name: "slowish", prio: 1, delay: 80 * time.Millisecond})
	e.Emit(trigger("a"))
	e.Emit(trigger("b")) // supersedes; the first dispatch's ctx dies
	got := collect(t, ch, 1)
	if got[0].Source != "slowish" {
		t.Fatalf("batch = %+v", got[0])
	}
	select {
	case m := <-ch:
		t.Fatalf("cancelled dispatch must not deliver, got %+v", m)
	case <-time.After(150 * time.Millisecond):
	}
}

// TestNonIdentTriggersSkipLocalSources: punctuation trigger characters are the
// LSP bridge's business; manual requests ("") do dispatch.
func TestNonIdentTriggersSkipLocalSources(t *testing.T) {
	e, ch := newTestEngine()
	e.Register(fakeSource{name: "words", prio: 1})
	e.Emit(trigger("."))
	select {
	case m := <-ch:
		t.Fatalf("dot trigger must not dispatch local sources, got %+v", m)
	case <-time.After(50 * time.Millisecond):
	}
	e.Emit(trigger(""))
	collect(t, ch, 1)
}

// dotFake claims the dot as its own trigger character (#1913).
type dotFake struct{ fakeSource }

func (dotFake) TriggerChar(ch string) bool { return ch == "." }

// TestTriggerCharDispatchesOnlyClaimingSources guards #1913: a dot reaches the
// postfix-style sources that ask for it and nothing else, so the word index
// still has nothing to say after a member access.
func TestTriggerCharDispatchesOnlyClaimingSources(t *testing.T) {
	e, ch := newTestEngine()
	e.Register(fakeSource{name: "words", prio: ilsp.PriorityWords})
	e.Register(dotFake{fakeSource{name: "postfix", prio: ilsp.PriorityPostfix}})

	e.Emit(trigger("."))
	got := collect(t, ch, 1)
	if got[0].Source != "postfix" {
		t.Fatalf("batch source = %q, want postfix", got[0].Source)
	}
	select {
	case m := <-ch:
		t.Fatalf("a dot must not dispatch %q", m.Source)
	case <-time.After(100 * time.Millisecond):
	}

	// An unclaimed punctuation character still dispatches nothing at all.
	e.Emit(trigger(","))
	select {
	case m := <-ch:
		t.Fatalf("an unclaimed punctuation trigger must dispatch nothing, got %q", m.Source)
	case <-time.After(100 * time.Millisecond):
	}

	// An exclusive claim on the path (#1302) still wins: the dot reaches the
	// owning source or nobody, never the trigger-claiming outsider.
	e.Register(exclusiveFake{fakeSource{name: "http", prio: ilsp.PriorityEmmet}})
	e.Emit(host.EditorEvent{Kind: host.EditorCompletionTrigger, Path: "/r.http", Line: 1, Col: 2, Char: "."})
	select {
	case m := <-ch:
		t.Fatalf("a claimed buffer must not dispatch %q on a dot", m.Source)
	case <-time.After(100 * time.Millisecond):
	}

	// Identifier characters keep dispatching every source, claiming or not.
	e.Emit(trigger("a"))
	if len(collect(t, ch, 2)) != 2 {
		t.Fatal("an identifier trigger must dispatch every source")
	}
}

// exclusiveFake claims every path ending in .http.
type exclusiveFake struct{ fakeSource }

func (e exclusiveFake) Exclusive(path string) bool { return strings.HasSuffix(path, ".http") }

// TestExclusiveSourceSilencesTheRest guards #1302: a language source that owns
// its files keeps the generic indexes out of the popup — and only there.
func TestExclusiveSourceSilencesTheRest(t *testing.T) {
	e, ch := newTestEngine()
	e.Register(fakeSource{name: "words", prio: ilsp.PriorityWords})
	e.Register(exclusiveFake{fakeSource{name: "http", prio: ilsp.PriorityEmmet}})

	// A claimed buffer: only the claiming source answers.
	e.Emit(host.EditorEvent{Kind: host.EditorCompletionTrigger, Path: "/r.http", Line: 1, Col: 2, Char: "a"})
	got := collect(t, ch, 1)
	if got[0].Source != "http" {
		t.Fatalf("batch source = %q, want http", got[0].Source)
	}
	select {
	case m := <-ch:
		t.Fatalf("a claimed buffer must not dispatch %q", m.Source)
	case <-time.After(150 * time.Millisecond):
	}

	// Any other buffer keeps the full merged popup.
	e.Emit(trigger("a"))
	if len(collect(t, ch, 2)) != 2 {
		t.Fatal("unclaimed buffers must still dispatch every source")
	}
}
