package editor

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/editor/mode"
	"ike/internal/lang"
)

// escapeedit_test.go covers the escape/unescape commands (#2338): the
// per-language form, the round trip, the caret fallback, the single undo unit
// and the refusals.

// bs is one backslash — escape forms are built from it so the expectations
// below are not themselves resolved by the Go compiler.
const bs = "\x5c"

// escLoaded loads content under name with the language id registered for its
// extension, so the buffer resolves to the dialect under test.
func escLoaded(t *testing.T, id, name, content string) Model {
	t.Helper()
	ext := filepath.Ext(name)
	lang.Register(lang.Language{ID: id, Extensions: []string{ext[1:]}})
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(80, 10)
	m.SetFocused(true)
	return m
}

// selectCols puts the model in charwise visual mode over [from, to] on line 0
// (inclusive, like every vim visual selection).
func selectCols(m *Model, from, to int) {
	m.mode = Visual
	m.anchor = buffer.Position{Line: 0, Col: from}
	m.cursor = buffer.Position{Line: 0, Col: to}
}

// TestEscapeSelectionPerLanguage: the same selection escapes into the form the
// buffer's language writes — the acceptance criterion across Python, JS, Go,
// JSON and YAML.
func TestEscapeSelectionPerLanguage(t *testing.T) {
	tests := []struct {
		name, id, file string
		line           string
		from, to       int
		want           string
	}{
		{
			name: "go", id: "go", file: "main.go",
			line: `s := "über"`, from: 6, to: 9,
			want: `s := "` + bs + `u00fcber"`,
		},
		{
			name: "python", id: "python", file: "app.py",
			line: `s = "über"`, from: 5, to: 8,
			want: `s = "` + bs + `u00fcber"`,
		},
		{
			name: "javascript", id: "typescript", file: "app.js",
			line: `const s = "über"`, from: 11, to: 14,
			want: `const s = "` + bs + `u00fcber"`,
		},
		{
			name: "json", id: "json", file: "i18n.json",
			line: `{"greeting": "über"}`, from: 14, to: 17,
			want: `{"greeting": "` + bs + `u00fcber"}`,
		},
		{
			name: "yaml", id: "yaml", file: "values.yaml",
			line: `greeting: "über"`, from: 11, to: 14,
			want: `greeting: "` + bs + `u00fcber"`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := escLoaded(t, tc.id, tc.file, tc.line+"\n")
			selectCols(&m, tc.from, tc.to)
			if cmd := m.escapeSelection(false); cmd != nil {
				t.Fatalf("escape emitted %#v", cmd())
			}
			if got := m.buf.Line(0); got != tc.want {
				t.Fatalf("escaped line = %q, want %q", got, tc.want)
			}
			// …and unescaping the rewritten range is the exact inverse.
			selectCols(&m, tc.from, tc.from+len([]rune(bs+"u00fcber"))-1)
			if cmd := m.escapeSelection(true); cmd != nil {
				t.Fatalf("unescape emitted %#v", cmd())
			}
			if got := m.buf.Line(0); got != tc.line {
				t.Fatalf("round trip = %q, want %q", got, tc.line)
			}
		})
	}
}

// TestEscapeSelectionAstralPerLanguage: above the BMP the languages differ —
// Go takes the long form, JS the braces, JSON a surrogate pair.
func TestEscapeSelectionAstralPerLanguage(t *testing.T) {
	tests := []struct{ name, id, file, want string }{
		{"go", "go", "main.go", bs + "U0001f600"},
		{"javascript", "typescript", "app.js", bs + "u{1f600}"},
		{"json", "json", "data.json", bs + "ud83d" + bs + "ude00"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := escLoaded(t, tc.id, tc.file, "\"\U0001F600\"\n")
			selectCols(&m, 1, 1)
			if cmd := m.escapeSelection(false); cmd != nil {
				t.Fatalf("escape emitted %#v", cmd())
			}
			if got, want := m.buf.Line(0), `"`+tc.want+`"`; got != want {
				t.Fatalf("escaped line = %q, want %q", got, want)
			}
		})
	}
}

// TestEscapeSelectionLinewise: a linewise selection covers its lines whole,
// across the line break.
func TestEscapeSelectionLinewise(t *testing.T) {
	m := escLoaded(t, "json", "i18n.json", "{\n  \"a\": \"ü\",\n  \"b\": \"ö\"\n}\n")
	m.mode = mode.VisualLine
	m.anchor = buffer.Position{Line: 1}
	m.cursor = buffer.Position{Line: 2}
	if cmd := m.escapeSelection(false); cmd != nil {
		t.Fatalf("escape emitted %#v", cmd())
	}
	if got, want := m.buf.Line(1), `  "a": "`+bs+`u00fc",`; got != want {
		t.Fatalf("line 1 = %q, want %q", got, want)
	}
	if got, want := m.buf.Line(2), `  "b": "`+bs+`u00f6"`; got != want {
		t.Fatalf("line 2 = %q, want %q", got, want)
	}
	if got, want := m.buf.Line(0), "{"; got != want {
		t.Fatalf("line 0 = %q, want it untouched", got)
	}
}

// TestEscapeWithoutSelectionUsesLiteralAtCaret: no selection means the string
// literal under the caret, quotes excluded.
func TestEscapeWithoutSelectionUsesLiteralAtCaret(t *testing.T) {
	m := escLoaded(t, "go", "main.go", "s := \"grüße\" + x\n")
	m.cursor = buffer.Position{Line: 0, Col: 8}
	if cmd := m.escapeSelection(false); cmd != nil {
		t.Fatalf("escape emitted %#v", cmd())
	}
	want := `s := "gr` + bs + `u00fc` + bs + `u00dfe" + x`
	if got := m.buf.Line(0); got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
}

// TestEscapeOutsideLiteralNotices: with neither a selection nor a literal at
// the caret there is nothing to act on, and the command says so.
func TestEscapeOutsideLiteralNotices(t *testing.T) {
	m := escLoaded(t, "go", "main.go", "x := über\n")
	m.cursor = buffer.Position{Line: 0, Col: 0}
	cmd := m.escapeSelection(false)
	if cmd == nil {
		t.Fatal("escape outside a literal must notice, not edit")
	}
	if _, ok := cmd().(NoticeMsg); !ok {
		t.Fatalf("expected a NoticeMsg, got %#v", cmd())
	}
	if got, want := m.buf.Line(0), "x := über"; got != want {
		t.Fatalf("line = %q, want it untouched", got)
	}
}

// TestEscapeInRawLiteralNotices: a Go raw literal keeps a backslash as text,
// so escaping into it would change what the file says.
func TestEscapeInRawLiteralNotices(t *testing.T) {
	m := escLoaded(t, "go", "main.go", "s := `über`\n")
	m.cursor = buffer.Position{Line: 0, Col: 7}
	cmd := m.escapeSelection(false)
	if cmd == nil {
		t.Fatal("escape inside a raw literal must notice, not edit")
	}
	if _, ok := cmd().(NoticeMsg); !ok {
		t.Fatalf("expected a NoticeMsg, got %#v", cmd())
	}
}

// TestEscapeAlreadyEscapedIsNoOp: an existing escape is plain ASCII and stays
// exactly as written — no double escaping.
func TestEscapeAlreadyEscapedIsNoOp(t *testing.T) {
	line := `s := "` + bs + bs + `u00fc"`
	m := escLoaded(t, "go", "main.go", line+"\n")
	selectCols(&m, 6, len([]rune(line))-2)
	cmd := m.escapeSelection(false)
	if cmd == nil {
		t.Fatal("an ASCII-only selection must notice, not edit")
	}
	if _, ok := cmd().(NoticeMsg); !ok {
		t.Fatalf("expected a NoticeMsg, got %#v", cmd())
	}
	if got := m.buf.Line(0); got != line {
		t.Fatalf("line = %q, want it untouched", got)
	}
}

// TestUnescapeKeepsEscapedBackslash: `\\u00fc` is an escaped backslash
// followed by text, so unescaping leaves it alone.
func TestUnescapeKeepsEscapedBackslash(t *testing.T) {
	line := `s := "` + bs + bs + `u00fc"`
	m := escLoaded(t, "go", "main.go", line+"\n")
	selectCols(&m, 6, len([]rune(line))-2)
	if cmd := m.escapeSelection(true); cmd == nil {
		t.Fatal("nothing decodable: expected a notice")
	}
	if got := m.buf.Line(0); got != line {
		t.Fatalf("line = %q, want it untouched", got)
	}
}

// TestEscapeSelectionIsOneUndoUnit: however many characters the command
// rewrites, undo restores the original in one step.
func TestEscapeSelectionIsOneUndoUnit(t *testing.T) {
	const line = `s := "Grüße über"`
	m := escLoaded(t, "go", "main.go", line+"\n")
	selectCols(&m, 6, len([]rune(line))-2)
	if cmd := m.escapeSelection(false); cmd != nil {
		t.Fatalf("escape emitted %#v", cmd())
	}
	escaped := m.buf.Line(0)
	if escaped == line {
		t.Fatal("escape changed nothing")
	}
	m, _ = m.runAction("undo")
	if got := m.buf.Line(0); got != line {
		t.Fatalf("after one undo = %q, want %q", got, line)
	}
	m, _ = m.runAction("redo")
	if got := m.buf.Line(0); got != escaped {
		t.Fatalf("after redo = %q, want %q", got, escaped)
	}
}

// TestEscapeSelectionRefusesMultiCaret: a caret is a position, not a range —
// the command declines instead of fanning one undo unit over places the user
// cannot see at once.
func TestEscapeSelectionRefusesMultiCaret(t *testing.T) {
	const line = `s := "über"`
	m := escLoaded(t, "go", "main.go", line+"\n"+line+"\n")
	m.cursor = buffer.Position{Line: 0, Col: 7}
	m.carets = []caret{{pos: buffer.Position{Line: 1, Col: 7}}}
	cmd := m.escapeSelection(false)
	if cmd == nil {
		t.Fatal("multi-caret must be refused with a notice")
	}
	if _, ok := cmd().(NoticeMsg); !ok {
		t.Fatalf("expected a NoticeMsg, got %#v", cmd())
	}
	if got := m.buf.Line(0); got != line {
		t.Fatalf("line = %q, want it untouched", got)
	}
}

// TestEscapeFallbackDialect: a buffer whose language has no escape syntax
// (a log excerpt) still escapes and unescapes, in the universal \uXXXX form.
func TestEscapeFallbackDialect(t *testing.T) {
	m := escLoaded(t, "log", "server.log", `msg="über"`+"\n")
	m.cursor = buffer.Position{Line: 0, Col: 6}
	if cmd := m.escapeSelection(false); cmd != nil {
		t.Fatalf("escape emitted %#v", cmd())
	}
	want := `msg="` + bs + `u00fcber"`
	if got := m.buf.Line(0); got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
	if cmd := m.escapeSelection(true); cmd != nil {
		t.Fatalf("unescape emitted %#v", cmd())
	}
	if got, want := m.buf.Line(0), `msg="über"`; got != want {
		t.Fatalf("round trip = %q, want %q", got, want)
	}
}

// TestEscapeSelectionActions: both ids reach the model through the action
// dispatch the palette and the keymap use.
func TestEscapeSelectionActions(t *testing.T) {
	const line = `s := "über"`
	m := escLoaded(t, "go", "main.go", line+"\n")
	selectCols(&m, 6, 9)
	m, _ = m.runAction("escape_selection")
	want := `s := "` + bs + `u00fcber"`
	if got := m.buf.Line(0); got != want {
		t.Fatalf("escape_selection = %q, want %q", got, want)
	}
	selectCols(&m, 6, 14)
	m, _ = m.runAction("unescape_selection")
	if got := m.buf.Line(0); got != line {
		t.Fatalf("unescape_selection = %q, want %q", got, line)
	}
}
