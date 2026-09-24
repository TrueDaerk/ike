package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/lang"
)

// The editor half of Complete Current Statement (#2726) is exercised against
// two throwaway languages rather than the shipped plugins: a colon language
// (Python-shaped rule, no closing line) and a brace language (the shared
// BraceCompletion with semicolons), plus one with a Toolchain that lacks the
// seam. Per-language rules live with their plugins.

type colonStmtToolchain struct{}

func (colonStmtToolchain) Detect(string) (map[string]any, bool) { return nil, false }
func (colonStmtToolchain) CompleteStatement(line string) (lang.StatementCompletion, bool) {
	head := strings.TrimRight(line, " \t")
	stmt := strings.TrimSpace(head)
	if !lang.LeadingKeyword(stmt, nil, []string{"def", "if"}) {
		return lang.StatementCompletion{}, false
	}
	if strings.HasSuffix(stmt, ":") {
		return lang.StatementCompletion{Head: head, Body: true}, true
	}
	return lang.StatementCompletion{Head: lang.CloseBrackets(head, "([{") + ":", Body: true}, true
}

type braceStmtToolchain struct{}

func (braceStmtToolchain) Detect(string) (map[string]any, bool) { return nil, false }
func (braceStmtToolchain) CompleteStatement(line string) (lang.StatementCompletion, bool) {
	return lang.BraceCompletion(line, func(stmt string) (string, bool) {
		if lang.LeadingKeyword(stmt, nil, []string{"function", "if"}) {
			return "}", true
		}
		return "", false
	}, true)
}

type noStmtToolchain struct{}

func (noStmtToolchain) Detect(string) (map[string]any, bool) { return nil, false }

func init() {
	lang.Register(lang.Language{ID: "stmt-colon", Extensions: []string{"stmc"}, Toolchain: colonStmtToolchain{}, IndentAfter: []string{":"}})
	lang.Register(lang.Language{ID: "stmt-brace", Extensions: []string{"stmb"}, Toolchain: braceStmtToolchain{}, IndentAfter: []string{"{"}})
	lang.Register(lang.Language{ID: "stmt-none", Extensions: []string{"stmn"}, Toolchain: noStmtToolchain{}})
}

// stmtModel loads content under ext with four-space indentation.
func stmtModel(t *testing.T, ext, content string) Model {
	t.Helper()
	m := loadedExt(t, ext, content)
	m.useSpaces = true
	m.tabWidth = 4
	m.autoIndent = true
	m.autoClosePairs = true
	return m
}

func complete(m Model) (Model, tea.Cmd) { return m.runAction("complete_statement") }

func text(m Model) string { return strings.Join(m.buf.Lines(), "\n") + "\n" }

func leaveInsert(m Model) Model { return send(m, esc()) }

func TestCompleteStatementColonHeader(t *testing.T) {
	m := stmtModel(t, "stmc", "def abc()\n")
	m = send(m, key('$'))
	m, _ = complete(m)
	if got, want := text(m), "def abc():\n    \n"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if m.cursor != (buffer.Position{Line: 1, Col: 4}) {
		t.Fatalf("cursor = %+v, want line 1 col 4", m.cursor)
	}
	if m.mode != Insert {
		t.Fatalf("mode = %v, want insert (ready to type the body)", m.mode)
	}
	// The body types in the fresh insert session.
	m = typeKeys(m, "pass")
	if got := line(m, 1); got != "    pass" {
		t.Fatalf("body line = %q", got)
	}
}

func TestCompleteStatementBalancesBracketsAndConsumesAutoPair(t *testing.T) {
	// "def abc(" typed with auto-close leaves "def abc(|)": the auto-inserted
	// closer is part of the line, so it is consumed rather than doubled.
	m := stmtModel(t, "stmc", "\n")
	m = send(m, key('i'))
	m = typeKeys(m, "def abc(")
	if got := line(m, 0); got != "def abc()" {
		t.Fatalf("auto-close precondition: %q", got)
	}
	if m.cursor.Col != 8 {
		t.Fatalf("cursor col = %d, want 8 (before the auto-closer)", m.cursor.Col)
	}
	m, _ = complete(m)
	if got, want := text(m), "def abc():\n    \n"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if m.mode != Insert {
		t.Fatalf("mode = %v, want insert", m.mode)
	}
	// A genuinely unclosed bracket is balanced before the colon.
	m = stmtModel(t, "stmc", "def abc(\n")
	m, _ = complete(m)
	if got := line(m, 0); got != "def abc():" {
		t.Fatalf("line = %q", got)
	}
}

func TestCompleteStatementIdempotent(t *testing.T) {
	m := stmtModel(t, "stmc", "def abc()\n")
	m, _ = complete(m)
	m = leaveInsert(m)
	m.cursor = buffer.Position{Line: 0, Col: 3}
	m, _ = complete(m)
	if got, want := text(m), "def abc():\n    \n"; got != want {
		t.Fatalf("second press changed the text: %q, want %q", got, want)
	}
	if m.cursor != (buffer.Position{Line: 1, Col: 4}) {
		t.Fatalf("cursor = %+v, want the empty body line", m.cursor)
	}
	// With a body present the caret lands at the end of its first line.
	m = stmtModel(t, "stmc", "def abc():\n    x = 1\n    y = 2\n")
	m, _ = complete(m)
	if got, want := text(m), "def abc():\n    x = 1\n    y = 2\n"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if m.cursor != (buffer.Position{Line: 1, Col: 9}) {
		t.Fatalf("cursor = %+v, want end of the first body line", m.cursor)
	}
}

func TestCompleteStatementBraceHeader(t *testing.T) {
	m := stmtModel(t, "stmb", "    function query()\n")
	m, _ = complete(m)
	if got, want := text(m), "    function query() {\n        \n    }\n"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if m.cursor != (buffer.Position{Line: 1, Col: 8}) {
		t.Fatalf("cursor = %+v, want line 1 col 8", m.cursor)
	}
	// Pressing again on the finished header with its closer directly
	// below (empty body) opens the body line without a second brace.
	m = stmtModel(t, "stmb", "function query() {\n}\n")
	m, _ = complete(m)
	if got, want := text(m), "function query() {\n    \n}\n"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	m = leaveInsert(m)
	m.cursor = buffer.Position{Line: 0, Col: 0}
	m, _ = complete(m)
	if got, want := text(m), "function query() {\n    \n}\n"; got != want {
		t.Fatalf("third press changed the text: %q, want %q", got, want)
	}
	if m.cursor.Line != 1 {
		t.Fatalf("cursor = %+v, want the body line", m.cursor)
	}
	// A nested header does not mistake the enclosing block's closer for
	// its own.
	m = stmtModel(t, "stmb", "function a() {\n    if (x\n}\n")
	m.cursor = buffer.Position{Line: 1, Col: 4}
	m, _ = complete(m)
	if got, want := text(m), "function a() {\n    if (x) {\n        \n    }\n}\n"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func TestCompleteStatementSimpleStatement(t *testing.T) {
	m := stmtModel(t, "stmb", "    $x = foo()\n")
	m, _ = complete(m)
	if got, want := text(m), "    $x = foo();\n    \n"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if m.cursor != (buffer.Position{Line: 1, Col: 4}) {
		t.Fatalf("cursor = %+v, want the next line at the same indent", m.cursor)
	}
}

func TestCompleteStatementUndoIsOneStep(t *testing.T) {
	m := stmtModel(t, "stmb", "function query()\n")
	m, _ = complete(m)
	m = leaveInsert(m)
	m = send(m, key('u'))
	if got, want := text(m), "function query()\n"; got != want {
		t.Fatalf("after undo: %q, want %q", got, want)
	}
	// Typed in insert mode: the typed text commits first, so the completion
	// is still its own step.
	m = stmtModel(t, "stmb", "\n")
	m = send(m, key('i'))
	m = typeKeys(m, "if (x")
	m, _ = complete(m)
	m = leaveInsert(m)
	if got, want := text(m), "if (x) {\n    \n}\n"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	m = send(m, key('u'))
	if got, want := text(m), "if (x)\n"; got != want {
		t.Fatalf("after undo: %q, want %q", got, want)
	}
}

func TestCompleteStatementHonoursIndentSettings(t *testing.T) {
	for _, tc := range []struct {
		useSpaces bool
		width     int
		want      string
	}{
		{true, 2, "\tif x:\n\t  \n"},
		{true, 4, "\tif x:\n\t    \n"},
		{false, 8, "\tif x:\n\t\t\n"},
	} {
		m := stmtModel(t, "stmc", "\tif x\n")
		m.useSpaces = tc.useSpaces
		m.tabWidth = tc.width
		m, _ = complete(m)
		if got := text(m); got != tc.want {
			t.Errorf("spaces=%v width=%d: text = %q, want %q", tc.useSpaces, tc.width, got, tc.want)
		}
	}
	m := stmtModel(t, "stmb", "\tif (x)\n")
	m.useSpaces = false
	m, _ = complete(m)
	if got, want := text(m), "\tif (x) {\n\t\t\n\t}\n"; got != want {
		t.Fatalf("tabs: text = %q, want %q", got, want)
	}
}

func TestCompleteStatementNothingToComplete(t *testing.T) {
	m := stmtModel(t, "stmc", "x = 1\n")
	m, cmd := complete(m)
	if cmd != nil {
		t.Fatalf("expected no notice, got %v", cmd())
	}
	if got := text(m); got != "x = 1\n" {
		t.Fatalf("text changed: %q", got)
	}
	if m.mode != Normal {
		t.Fatalf("mode = %v, want normal (no edit, no insert)", m.mode)
	}
}

func TestCompleteStatementUnsupportedLanguage(t *testing.T) {
	for _, ext := range []string{"stmn", "txt"} {
		m := stmtModel(t, ext, "if x\n")
		m, cmd := complete(m)
		if cmd == nil {
			t.Fatalf("%s: expected a notice", ext)
		}
		n, ok := cmd().(NoticeMsg)
		if !ok || !strings.HasPrefix(n.Text, "complete statement: not supported for ") {
			t.Fatalf("%s: notice = %#v", ext, cmd())
		}
		if got := text(m); got != "if x\n" {
			t.Fatalf("%s: text changed: %q", ext, got)
		}
	}
}

func TestCompleteStatementDotRepeat(t *testing.T) {
	m := stmtModel(t, "stmc", "def a()\ndef b()\n")
	m, _ = complete(m)
	m = leaveInsert(m)
	m.cursor = buffer.Position{Line: 2, Col: 0}
	m = send(m, key('.'))
	if got, want := text(m), "def a():\n    \ndef b():\n    \n"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

func TestCompleteStatementReadOnlyRefuses(t *testing.T) {
	m := stmtModel(t, "stmc", "def abc()\n")
	m.SetReadOnly(true)
	m, _ = complete(m)
	if got := text(m); got != "def abc()\n" {
		t.Fatalf("text changed: %q", got)
	}
	if m.mode != Normal || m.cursor.Line != 0 {
		t.Fatalf("mode = %v cursor = %+v, want normal on line 0", m.mode, m.cursor)
	}
}
