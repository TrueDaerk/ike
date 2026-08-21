package app

import (
	"testing"

	"ike/internal/highlight"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
	"ike/internal/palette"
)

// bufferLangLangs registers the minimal languages the buffer-language tests
// pick from — id and extension only, deliberately not the language plugins
// (whose ServerSpec would open the missing-server prompt in every test model,
// like intentionModel's json registration).
func bufferLangLangs() {
	lang.Register(lang.Language{ID: "markdown", Extensions: []string{"md"}})
	lang.Register(lang.Language{ID: "http", Extensions: []string{"http"}})
}

// offeredTitles returns the intention popup's row titles for the model's
// current caret.
func offeredTitles(t *testing.T, m Model) []string {
	t.Helper()
	out, _ := m.Update(ilsp.CodeActionsMsg{Intentions: true})
	m = out.(Model)
	var titles []string
	for _, it := range m.actions.Results("", palette.Context{}) {
		titles = append(titles, it.Title)
	}
	return titles
}

func hasTitle(titles []string, want string) bool {
	for _, t := range titles {
		if t == want {
			return true
		}
	}
	return false
}

// TestBufferLangIntentionOfferedInFilelessBuffer guards the first acceptance
// criterion of #2033: alt+enter in a buffer with no file offers the pick.
func TestBufferLangIntentionOfferedInFilelessBuffer(t *testing.T) {
	m := filelessModel(t, "some pasted text", 0, 0)
	if titles := offeredTitles(t, m); !hasTitle(titles, "Treat Buffer as…") {
		t.Fatalf("the buffer-language intention is missing from %v", titles)
	}
}

// TestBufferLangIntentionHiddenWithFile guards the documented decision: a
// buffer with a file is classified by its name, so the pick is not offered
// there — no override, not even as an escape hatch.
func TestBufferLangIntentionHiddenWithFile(t *testing.T) {
	m := intentionModel(t, "x.json", `{"name": "value"}`, 0, 11)
	for _, title := range offeredTitles(t, m) {
		if title == "Treat Buffer as…" {
			t.Fatal("a buffer with a file must not offer the buffer-language pick")
		}
	}
}

// TestBufferLangIntentionNamesCurrentType: once a type is chosen the entry
// says which — the popup is where the buffer's type is both read and changed.
func TestBufferLangIntentionNamesCurrentType(t *testing.T) {
	bufferLangLangs()
	m := filelessModel(t, "# Title", 0, 0)
	out, _ := m.Update(SetBufferLangMsg{ID: "markdown"})
	m = out.(Model)
	if titles := offeredTitles(t, m); !hasTitle(titles, "Treat Buffer as… (now markdown)") {
		t.Fatalf("the entry must name the current type, got %v", titles)
	}
}

// TestSetBufferLangAppliesAndShowsInStatusLine covers the pick's two visible
// effects: the buffer resolves as the language, and the status line says so
// — the "type is recognizable and changeable" acceptance criterion.
func TestSetBufferLangAppliesAndShowsInStatusLine(t *testing.T) {
	bufferLangLangs()
	m := filelessModel(t, "# Title", 0, 0)
	out, _ := m.Update(SetBufferLangMsg{ID: "markdown"})
	m = out.(Model)
	ed := m.activeEditor()
	if got := ed.LangID(); got != "markdown" {
		t.Fatalf("buffer language = %q, want markdown", got)
	}
	if got := bufferLangSegment(m, ed); got != "as Markdown" {
		t.Fatalf("status segment = %q, want \"as Markdown\"", got)
	}
	if !noticed(m, "treating this buffer as Markdown") {
		t.Fatalf("missing confirmation notice, history = %+v", m.history)
	}
	// Changeable: back to plain text, and the segment disappears with the type.
	out, _ = m.Update(SetBufferLangMsg{ID: ""})
	m = out.(Model)
	ed = m.activeEditor()
	if got := ed.LangID(); got != "" {
		t.Fatalf("cleared buffer language = %q, want empty", got)
	}
	if got := bufferLangSegment(m, ed); got != "" {
		t.Fatalf("cleared status segment = %q, want empty", got)
	}
}

// TestBufferLangPickerRefusesBufferWithFile: opening the picker where the
// pick could not apply says why instead of showing a dead list.
func TestBufferLangPickerRefusesBufferWithFile(t *testing.T) {
	m := intentionModel(t, "x.json", `{"a": 1}`, 0, 0)
	out, _ := m.Update(ShowBufferLangMsg{})
	m = out.(Model)
	if m.palette.IsOpen() {
		t.Fatal("a buffer with a file must not open the language picker")
	}
	if !noticed(m, "classified by its file name") {
		t.Fatalf("missing refusal notice, history = %+v", m.history)
	}
}

// TestBufferLangPickerOpensLocked: from a file-less buffer the command opens
// the picker locked to its mode, listing plain text plus the languages.
func TestBufferLangPickerOpensLocked(t *testing.T) {
	bufferLangLangs()
	m := filelessModel(t, "", 0, 0)
	out, _ := m.Update(ShowBufferLangMsg{})
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("the language picker must open over a file-less buffer")
	}
	items := bufferLangMode{}.Results("", palette.Context{})
	if len(items) == 0 || items[0].Title != "Plain Text" {
		t.Fatalf("Plain Text must head the unfiltered list, got %+v", items)
	}
	found := false
	for _, it := range items {
		if it.Title == "Markdown" && it.Msg == (SetBufferLangMsg{ID: "markdown"}) {
			found = true
		}
	}
	if !found {
		t.Fatalf("markdown row missing from %+v", items)
	}
}

// TestBufferLangEnablesHTTPIntentions guards the type-specific-intentions
// criterion: a pasted request block in a file-less buffer offers "Run
// Request" once the buffer is treated as HTTP — and does not before.
func TestBufferLangEnablesHTTPIntentions(t *testing.T) {
	bufferLangLangs()
	m := filelessModel(t, "### thing\nGET https://example.com/things\n", 1, 0)
	if titles := offeredTitles(t, m); hasTitle(titles, "Run Request") {
		t.Fatalf("a typeless buffer must not offer the HTTP actions, got %v", titles)
	}
	out, _ := m.Update(SetBufferLangMsg{ID: "http"})
	m = out.(Model)
	if titles := offeredTitles(t, m); !hasTitle(titles, "Run Request") {
		t.Fatalf("an HTTP buffer must offer Run Request, got %v", titles)
	}
	if !isHTTPBuffer(m.activeEditor()) {
		t.Fatal("the HTTP gates must accept a file-less buffer treated as HTTP")
	}
	if got := httpSource(m.activeEditor()); got != "buffer.http" {
		t.Fatalf("dispatch source = %q, want the synthetic buffer name", got)
	}
}

// TestFilelessParseReachesItsBuffer guards the routing half of #2033: parse
// results are delivered by editor.ParseKey, so the buffer that scheduled one
// actually gets it. Routing by path — the rule before — dropped every
// file-less buffer's result, which left a chosen language unhighlighted no
// matter how well it resolved.
func TestFilelessParseReachesItsBuffer(t *testing.T) {
	bufferLangLangs()
	m := filelessModel(t, "# Title", 0, 0)
	out, _ := m.Update(SetBufferLangMsg{ID: "markdown"})
	m = out.(Model)
	ed := m.activeEditor()
	out, _ = m.Update(highlight.SpansMsg{
		Path:    ed.ParseKey(),
		Version: ed.DocVersion(),
		Spans:   []highlight.Span{{Line: 0, StartCol: 2, EndCol: 7, Capture: "label"}},
	})
	m = out.(Model)
	if got := m.activeEditor().SyntaxCapture(0, 2); got != "label" {
		t.Fatalf("the parse must reach the file-less buffer, capture = %q", got)
	}
}
