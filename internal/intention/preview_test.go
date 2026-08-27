package intention

import "testing"

// preview_test.go covers the provider seam of #2252: the entries that rewrite
// the buffer carry a lazy preview, everything else carries none, and the
// preview is only computed when the popup asks for it.

// previewCx builds a context whose preview answers for the listed command ids,
// counting the calls so laziness is observable.
func previewCx(base Context, calls *int, ids map[string]Edit) Context {
	base.Preview = func(id string) (Edit, bool) {
		*calls++
		e, ok := ids[id]
		return e, ok
	}
	return base
}

// itemFor returns the built-in item for one command id.
func itemFor(t *testing.T, cx Context, id string) Item {
	t.Helper()
	for _, p := range Builtins() {
		for _, it := range p.Items(cx) {
			if it.CommandID == id {
				return it
			}
		}
	}
	t.Fatalf("no built-in item for %q", id)
	return Item{}
}

// TestRewritingEntriesCarryAPreview: the value toggle and the conflict accepts
// hand the popup a preview closure.
func TestRewritingEntriesCarryAPreview(t *testing.T) {
	calls := 0
	cx := previewCx(Context{CanToggleValue: true, ConflictAtCaret: true}, &calls, map[string]Edit{
		"editor.toggleValue": {Before: "a = true", After: "a = false"},
		"merge.acceptOurs":   {Before: "<<<<<<<\nours\n=======\ntheirs\n>>>>>>>", After: "ours"},
		"merge.acceptTheirs": {Before: "<<<<<<<\nours\n=======\ntheirs\n>>>>>>>", After: "theirs"},
		"merge.acceptBoth":   {Before: "<<<<<<<\nours\n=======\ntheirs\n>>>>>>>", After: "ours\ntheirs"},
	})
	for id, want := range map[string]string{
		"editor.toggleValue": "a = false",
		"merge.acceptOurs":   "ours",
		"merge.acceptTheirs": "theirs",
		"merge.acceptBoth":   "ours\ntheirs",
	} {
		it := itemFor(t, cx, id)
		if it.Preview == nil {
			t.Fatalf("%s must carry a preview", id)
		}
		edit, ok := it.Preview()
		if !ok || edit.After != want {
			t.Fatalf("%s previewed %q (ok=%v), want %q", id, edit.After, ok, want)
		}
	}
}

// TestPreviewIsOnlyComputedWhenAsked: building the item list must not compute
// a single preview — that is what makes highlighting, not opening, the cost.
func TestPreviewIsOnlyComputedWhenAsked(t *testing.T) {
	calls := 0
	cx := previewCx(Context{CanToggleValue: true}, &calls, map[string]Edit{
		"editor.toggleValue": {Before: "a", After: "b"},
	})
	it := itemFor(t, cx, "editor.toggleValue")
	if calls != 0 {
		t.Fatalf("listing the items computed %d previews, want none", calls)
	}
	it.Preview()
	if calls != 1 {
		t.Fatalf("asking computed %d previews, want one", calls)
	}
}

// TestCommandEntriesHaveNoPreview: a copy entry runs a command and has nothing
// to show, so the popup's "no preview" is the honest default.
func TestCommandEntriesHaveNoPreview(t *testing.T) {
	calls := 0
	cx := previewCx(Context{DocPath: true, LangID: "json"}, &calls, nil)
	if it := itemFor(t, cx, "editor.copyDocPath"); it.Preview != nil {
		t.Fatal("a copy entry must not claim a preview")
	}
}

// TestPreviewlessContextWiresNothing: a context without the app's preview
// function — every provider test, and any host that does not offer one —
// leaves the entries preview-free instead of panicking on a nil call.
func TestPreviewlessContextWiresNothing(t *testing.T) {
	it := itemFor(t, Context{CanToggleValue: true}, "editor.toggleValue")
	if it.Preview != nil {
		t.Fatal("without Context.Preview an entry must carry no preview closure")
	}
}
