package editor

import (
	"testing"

	"ike/internal/lang"
	ilsp "ike/internal/lsp"
)

// langoverride_complete_test.go covers #2048: the completion path of a
// file-less buffer given a language through "Treat Buffer as …". Two seams
// have to hold — the events the editor emits must carry the buffer's identity
// and its language name, and a batch answered under that identity must reach
// the view a path route could never find.

// TestCompletionEventCarriesBufferKeyAndLanguage guards the emitted side: a
// file-less buffer told to be markdown emits triggers whose LangPath is the
// synthetic name the completion sources resolve, and whose Key is the view's
// own ParseKey — Path stays empty, because there is no file.
func TestCompletionEventCarriesBufferKeyAndLanguage(t *testing.T) {
	lang.Register(lang.Language{ID: "markdown", Extensions: []string{"md", "markdown"}})
	m := fileless(t, "word\n")
	if _, ok := m.SetLangOverride("markdown"); !ok {
		t.Fatal("SetLangOverride(markdown) refused on a file-less buffer")
	}
	var got []Event
	m.SetEmitter(EmitterFunc(func(e Event) { got = append(got, e) }))
	m = insertModeAt(m, 0, 4)
	m = send(m, key('s'))

	var trig *Event
	for i := range got {
		if got[i].Kind == EventCompletionTrigger {
			trig = &got[i]
		}
	}
	if trig == nil {
		t.Fatalf("no completion trigger emitted, got %d events", len(got))
	}
	if trig.Path != "" {
		t.Errorf("Path = %q, want empty — the buffer has no file", trig.Path)
	}
	if trig.LangPath != "buffer.md" {
		t.Errorf("LangPath = %q, want buffer.md", trig.LangPath)
	}
	if trig.Key != m.ParseKey() || trig.Key == "" {
		t.Errorf("Key = %q, want the view's ParseKey %q", trig.Key, m.ParseKey())
	}

	// Back to plain text: the language name is gone, the identity is not.
	if _, ok := m.SetLangOverride(""); !ok {
		t.Fatal("clearing the override failed")
	}
	got = nil
	m = send(m, key('t'))
	for i := range got {
		if got[i].Kind == EventCompletionTrigger && got[i].LangPath != "" {
			t.Fatalf("after Plain Text LangPath = %q, want empty", got[i].LangPath)
		}
	}
}

// TestCompletionBatchReachesFilelessBuffer guards the return route: a batch
// tagged with the view's key opens the popup in a buffer with no file, and a
// batch for another key never does.
func TestCompletionBatchReachesFilelessBuffer(t *testing.T) {
	m := fileless(t, "word\n")
	m = insertModeAt(m, 0, 4)
	batch := func(key string) ilsp.CompletionMsg {
		return ilsp.CompletionMsg{
			Key: key, Line: 0, Col: 4,
			Items:  []ilsp.CompletionItem{{Label: "wordy", InsertText: "wordy"}},
			Source: "words",
		}
	}
	m, _ = m.Update(batch(m.ParseKey() + "-other"))
	if m.comp != nil {
		t.Fatal("a batch for another view's key must not open the popup")
	}
	m, _ = m.Update(batch(m.ParseKey()))
	if m.comp == nil {
		t.Fatal("a batch under the view's own key must open the popup (#2048)")
	}
}

// TestCompletionBatchStillRoutesByPath keeps the file case on the old route:
// a batch carrying only a Path reaches the buffer holding that file, which is
// how the LSP bridge (which never sets Key) keeps working.
func TestCompletionBatchStillRoutesByPath(t *testing.T) {
	m, path := loaded(t, "word\n")
	m = insertModeAt(m, 0, 4)
	m, _ = m.Update(ilsp.CompletionMsg{
		Path: path, Line: 0, Col: 4,
		Items:  []ilsp.CompletionItem{{Label: "wordy", InsertText: "wordy"}},
		Source: ilsp.SourceLSP,
	})
	if m.comp == nil {
		t.Fatal("a path-tagged batch must still open the popup of that file")
	}
}
