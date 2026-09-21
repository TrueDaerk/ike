package editor

import (
	"regexp"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/highlight"
	"ike/internal/lang"
)

func init() {
	// A stand-in for Go/Python with the #2654 data and no grammar: the
	// tests install highlight spans by hand where a capture matters.
	lang.Register(lang.Language{
		ID:           "cctest",
		Extensions:   []string{"cctest"},
		LineComment:  "//",
		DeclKeywords: []string{"func", "def"},
		ImportLine:   regexp.MustCompile(`^\s*import\b`),
	})
}

// collectTriggerEvents records every completion-trigger event in full.
func collectTriggerEvents(m *Model) *[]Event {
	var got []Event
	m.SetEmitter(EmitterFunc(func(e Event) {
		if e.Kind == EventCompletionTrigger {
			got = append(got, e)
		}
	}))
	return &got
}

var ctrlSpace = tea.KeyPressMsg{Code: ' ', Mod: tea.ModCtrl}

// TestDeclarationPositionSkipsAutoTrigger (#2654): an identifier typed right
// after a declaring keyword — `func na`, `def na` — opens no popup, while
// ctrl+space at the same position still requests, tagged as a declaration
// so the engine dispatches its sources normally.
func TestDeclarationPositionSkipsAutoTrigger(t *testing.T) {
	for _, kw := range []string{"func", "def"} {
		m := loadedExt(t, "cctest", kw+" \n")
		got := collectTriggerEvents(&m)
		m = insertModeAt(m, 0, len(kw)+1)
		m = typeKeys(m, "na")
		if len(*got) != 0 {
			t.Fatalf("%s: typing after the keyword emitted %d triggers, want none", kw, len(*got))
		}
		m = send(m, ctrlSpace)
		if len(*got) != 1 || (*got)[0].Char != "" || (*got)[0].Context != lang.CtxDecl {
			t.Fatalf("%s: ctrl+space events = %+v, want one manual trigger in the declaration context", kw, *got)
		}
	}
}

// TestCodePositionStillAutoTriggers: the gate leaves ordinary code alone —
// `x := na` triggers per character, tagged as code.
func TestCodePositionStillAutoTriggers(t *testing.T) {
	m := loadedExt(t, "cctest", "x := \n")
	got := collectTriggerEvents(&m)
	m = insertModeAt(m, 0, 5)
	m = typeKeys(m, "na")
	if len(*got) != 2 {
		t.Fatalf("typing in code emitted %d triggers, want 2", len(*got))
	}
	for _, e := range *got {
		if e.Context != lang.CtxCode {
			t.Fatalf("code trigger context = %q, want code", e.Context)
		}
	}
}

// TestCommentPositionSkipsAutoTrigger (#2654): typing inside a `//` comment
// opens no popup; ctrl+space there emits a manual trigger in the comment
// context, which the engine answers with current-buffer words only.
func TestCommentPositionSkipsAutoTrigger(t *testing.T) {
	m := loadedExt(t, "cctest", "// hel\nfoo\n")
	m.hlIndex = highlight.NewIndex([]highlight.Span{{Line: 0, StartCol: 0, EndCol: 6, Capture: "comment"}})
	got := collectTriggerEvents(&m)
	m = insertModeAt(m, 0, 6)
	m = typeKeys(m, "lo")
	if len(*got) != 0 {
		t.Fatalf("typing in a comment emitted %d triggers, want none", len(*got))
	}
	m = send(m, ctrlSpace)
	if len(*got) != 1 || (*got)[0].Context != lang.CtxComment {
		t.Fatalf("ctrl+space events = %+v, want one trigger in the comment context", *got)
	}
}

// TestStringPositionSkipsAutoTrigger (#2654): typing inside a string literal
// opens no popup; ctrl+space emits a string-context trigger (the server is
// still asked, the local indexes are not).
func TestStringPositionSkipsAutoTrigger(t *testing.T) {
	m := loadedExt(t, "cctest", `s := "he"`+"\n")
	m.hlIndex = highlight.NewIndex([]highlight.Span{{Line: 0, StartCol: 5, EndCol: 9, Capture: "string"}})
	got := collectTriggerEvents(&m)
	m = insertModeAt(m, 0, 8)
	m = typeKeys(m, "l")
	if len(*got) != 0 {
		t.Fatalf("typing in a string emitted %d triggers, want none", len(*got))
	}
	m = send(m, ctrlSpace)
	if len(*got) != 1 || (*got)[0].Context != lang.CtxString {
		t.Fatalf("ctrl+space events = %+v, want one trigger in the string context", *got)
	}
}

// TestImportLineTriggersWithContext (#2654): an import line still
// auto-triggers — the server has the answer — but the event carries the
// import context so the engine skips the local indexes.
func TestImportLineTriggersWithContext(t *testing.T) {
	m := loadedExt(t, "cctest", "import lo\n")
	got := collectTriggerEvents(&m)
	m = insertModeAt(m, 0, 9)
	m = typeKeys(m, "g")
	if len(*got) != 1 || (*got)[0].Context != lang.CtxImport {
		t.Fatalf("import-line events = %+v, want one trigger in the import context", *got)
	}
}

// TestNoGrammarNoLanguageReadsAsCode: a plain-text buffer has neither
// captures nor keywords, so the gate never withholds anything there.
func TestNoGrammarNoLanguageReadsAsCode(t *testing.T) {
	m, _ := loaded(t, "func \n")
	got := collectTriggerEvents(&m)
	m = insertModeAt(m, 0, 5)
	m = typeKeys(m, "na")
	if len(*got) != 2 {
		t.Fatalf("plain text after 'func' emitted %d triggers, want 2", len(*got))
	}
}
