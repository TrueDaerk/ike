package complete

import (
	"context"
	"testing"
	"time"

	"ike/internal/host"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
)

// claimingSource is a fakeSource that answers in the contexts it lists
// (complete.ContextSource, #2654).
type claimingSource struct {
	fakeSource
	in []lang.CompletionContext
}

func (c claimingSource) CompletesIn(ctx lang.CompletionContext) bool {
	for _, x := range c.in {
		if x == ctx {
			return true
		}
	}
	return false
}

func contextTrigger(char string, ctx lang.CompletionContext) host.EditorEvent {
	ev := trigger(char)
	ev.Context = string(ctx)
	return ev
}

// expectSilence fails when a batch arrives within the window.
func expectSilence(t *testing.T, ch <-chan ilsp.CompletionMsg, what string) {
	t.Helper()
	select {
	case m := <-ch:
		t.Fatalf("%s: got a batch from %q, want no dispatch", what, m.Source)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestImportContextDispatchesNoLocalSource (#2654): on an import line the
// local indexes have only noise to add, so a request in that context
// dispatches nothing — the LSP bridge, which is not a Source, still asks.
func TestImportContextDispatchesNoLocalSource(t *testing.T) {
	e, ch := newTestEngine()
	e.Register(fakeSource{name: "words", prio: ilsp.PriorityWords})
	e.Register(fakeSource{name: "symbols", prio: ilsp.PrioritySymbols})
	e.Emit(contextTrigger("g", lang.CtxImport))
	expectSilence(t, ch, "import context, auto trigger")
	e.Emit(contextTrigger("", lang.CtxImport))
	expectSilence(t, ch, "import context, manual trigger")
}

// TestStringContextKeepsOnlyClaimingSources (#2654): inside a string
// literal only a source claiming the context runs — the `.http` `{{`
// claim and path completion stay, the word index goes.
func TestStringContextKeepsOnlyClaimingSources(t *testing.T) {
	e, ch := newTestEngine()
	e.Register(fakeSource{name: "words", prio: ilsp.PriorityWords})
	e.Register(claimingSource{fakeSource{name: "paths", prio: ilsp.PriorityEmmet, delay: 0}, []lang.CompletionContext{lang.CtxString}})
	e.Emit(contextTrigger("", lang.CtxString))
	got := collect(t, ch, 1)
	if got[0].Source != "paths" {
		t.Fatalf("string context dispatched %q, want the claiming source only", got[0].Source)
	}
	expectSilence(t, ch, "string context, non-claiming source")
}

// TestCommentContextKeepsOnlyClaimingSources (#2654): a manual request in a
// comment reaches the sources claiming comments (the word index) and no
// other, with the request carrying the context so the source can narrow
// its answer.
func TestCommentContextKeepsOnlyClaimingSources(t *testing.T) {
	e, ch := newTestEngine()
	seen := make(chan lang.CompletionContext, 1)
	e.Register(ctxRecordingSource{name: "words", seen: seen, in: []lang.CompletionContext{lang.CtxComment}})
	e.Register(fakeSource{name: "symbols", prio: ilsp.PrioritySymbols})
	e.Emit(contextTrigger("", lang.CtxComment))
	got := collect(t, ch, 1)
	if got[0].Source != "words" {
		t.Fatalf("comment context dispatched %q, want words only", got[0].Source)
	}
	if ctx := <-seen; ctx != lang.CtxComment {
		t.Fatalf("request context = %q, want comment", ctx)
	}
	expectSilence(t, ch, "comment context, non-claiming source")
}

// TestDeclarationAndCodeContextsDispatchEverything (#2654): the declaration
// context withholds only the editor's auto-trigger; a manual request there
// dispatches every source, exactly like code.
func TestDeclarationAndCodeContextsDispatchEverything(t *testing.T) {
	for _, ctx := range []lang.CompletionContext{lang.CtxCode, lang.CtxDecl} {
		e, ch := newTestEngine()
		e.Register(fakeSource{name: "words", prio: ilsp.PriorityWords})
		e.Register(fakeSource{name: "symbols", prio: ilsp.PrioritySymbols})
		e.Emit(contextTrigger("", ctx))
		got := collect(t, ch, 2)
		names := map[string]bool{got[0].Source: true, got[1].Source: true}
		if !names["words"] || !names["symbols"] {
			t.Fatalf("%q context dispatched %v, want both sources", ctx, names)
		}
	}
}

// ctxRecordingSource reports the request context it was dispatched with.
type ctxRecordingSource struct {
	name string
	seen chan lang.CompletionContext
	in   []lang.CompletionContext
}

func (r ctxRecordingSource) Name() string  { return r.name }
func (r ctxRecordingSource) Priority() int { return ilsp.PriorityWords }
func (r ctxRecordingSource) CompletesIn(ctx lang.CompletionContext) bool {
	return claimingSource{in: r.in}.CompletesIn(ctx)
}
func (r ctxRecordingSource) Complete(_ context.Context, req Request) ([]ilsp.CompletionItem, error) {
	r.seen <- req.Context
	return []ilsp.CompletionItem{{Label: "w", InsertText: "w"}}, nil
}
