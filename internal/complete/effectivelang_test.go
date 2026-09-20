package complete

import (
	"testing"

	"ike/internal/host"
	"ike/internal/lang"

	// The fence test needs the Markdown grammar (fence detection) and the
	// Python language; a no-cgo build registers Markdown without a grammar
	// and that test skips.
	_ "ike/plugins/languages/markdown"
	_ "ike/plugins/languages/python"
)

// effectivelang_test.go covers #2652: the engine resolves the effective
// language at the cursor — the embedded fragment's language inside one, the
// buffer's otherwise — and hands it to every source as Request.Lang.

const fencedReadme = "# Title\n\nprose here\n\n```python\ndef f():\n    return x\n```\n\nmore prose\n"

// TestEffectiveLanguageInsideFence: a cursor inside a ```python fence of a
// Markdown buffer dispatches with Lang "python"; outside it, "markdown".
func TestEffectiveLanguageInsideFence(t *testing.T) {
	if l, ok := lang.ByID("markdown"); !ok || l.Grammar == nil {
		t.Skip("no markdown grammar (no-cgo build)")
	}
	e, ch := newTestEngine()
	rec := &recordingSource{}
	e.Register(rec)
	e.Emit(host.EditorEvent{Kind: host.EditorChange, Path: "/README.md", Text: fencedReadme})

	e.Emit(host.EditorEvent{Kind: host.EditorCompletionTrigger, Path: "/README.md", Line: 6, Col: 12, Char: "x"})
	collect(t, ch, 1)
	if got := rec.request().Lang; got != "python" {
		t.Fatalf("inside the fence Lang = %q, want python", got)
	}

	e.Emit(host.EditorEvent{Kind: host.EditorCompletionTrigger, Path: "/README.md", Line: 2, Col: 5, Char: "e"})
	collect(t, ch, 1)
	if got := rec.request().Lang; got != "markdown" {
		t.Fatalf("in prose Lang = %q, want markdown (markdown_inline is not a buffer language)", got)
	}
}

// TestEffectiveLanguageFromRegions: the resolution needs no grammar — a
// host with a Go-level region detector (#1303) resolves the same way, and a
// region of an unregistered language keeps the host's.
func TestEffectiveLanguageFromRegions(t *testing.T) {
	lang.Register(lang.Language{ID: "efhost", Extensions: []string{"efh"}, Regions: func(lines []string) []lang.Region {
		return []lang.Region{
			{Lang: "efembed", StartLine: 1, StartCol: 0, EndLine: 1, EndCol: 5},
			{Lang: "nosuchlang", StartLine: 2, StartCol: 0, EndLine: 2, EndCol: 5},
		}
	}})
	lang.Register(lang.Language{ID: "efembed", Extensions: []string{"efe"}})
	e, ch := newTestEngine()
	rec := &recordingSource{}
	e.Register(rec)
	e.Emit(host.EditorEvent{Kind: host.EditorChange, Path: "/a.efh", Text: "host\nembed\nother\n"})

	for _, tc := range []struct {
		line, col int
		want      string
	}{
		{0, 2, "efhost"},
		{1, 3, "efembed"},
		{1, 5, "efembed"}, // the end of a fragment is where typing appends
		{2, 3, "efhost"},  // unregistered region language: the host's
	} {
		e.Emit(host.EditorEvent{Kind: host.EditorCompletionTrigger, Path: "/a.efh", Line: tc.line, Col: tc.col, Char: "a"})
		collect(t, ch, 1)
		if got := rec.request().Lang; got != tc.want {
			t.Errorf("(%d,%d) Lang = %q, want %q", tc.line, tc.col, got, tc.want)
		}
	}
}

// TestLangIDFallsBackToPath: a request built without Lang (a source test,
// a plugin calling a source directly) still resolves the buffer language.
func TestLangIDFallsBackToPath(t *testing.T) {
	lang.Register(lang.Language{ID: "lidlang", Extensions: []string{"lid"}, CompletionPeers: []string{"lidpeer"}})
	req := Request{Path: "/a.lid"}
	if got := req.LangID(); got != "lidlang" {
		t.Fatalf("LangID() = %q, want lidlang", got)
	}
	if got := req.Langs(); len(got) != 2 || got[0] != "lidlang" || got[1] != "lidpeer" {
		t.Fatalf("Langs() = %v, want [lidlang lidpeer]", got)
	}
	if got := (Request{Path: "/a.lid", Lang: "other"}).LangID(); got != "other" {
		t.Fatalf("an explicit Lang must win, got %q", got)
	}
	if got := (Request{Path: "/nolang.zzz"}).Langs(); len(got) != 1 || got[0] != "" {
		t.Fatalf("no language: Langs() = %v, want [\"\"]", got)
	}
}
