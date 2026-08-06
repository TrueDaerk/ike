package langenv

import (
	"strings"
	"testing"

	"ike/internal/lang"
)

func TestLintFlagsShadowedKeys(t *testing.T) {
	notes := envLint([]string{
		"PORT=3000",
		"DEBUG=1",
		"PORT=8080",
		"PORT=9090",
	})
	if len(notes) != 2 {
		t.Fatalf("got %d notes, want the two shadowed PORT assignments: %+v", len(notes), notes)
	}
	if notes[0].Line != 0 || notes[1].Line != 2 {
		t.Errorf("notes on lines %d/%d, want 0/2 — the last occurrence wins and is not flagged",
			notes[0].Line, notes[1].Line)
	}
	if notes[0].Severity != lang.NoteWarn {
		t.Errorf("severity = %d, want a warning", notes[0].Severity)
	}
	if !strings.Contains(notes[0].Message, "line 4") {
		t.Errorf("message %q must name the winning line (4)", notes[0].Message)
	}
	if got := "PORT=3000"[notes[0].StartCol:notes[0].EndCol]; got != "PORT" {
		t.Errorf("note covers %q, want the key", got)
	}
}

func TestLintIgnoresCommentsBlanksAndFlags(t *testing.T) {
	notes := envLint([]string{
		"# PORT=1",
		"# PORT=2",
		"",
		"   ",
		"BARE",
		"BARE",
	})
	if len(notes) != 0 {
		t.Errorf("got %+v, want no notes: comments, blanks and lines without = are not assignments", notes)
	}
}

// TestLintSeesThroughExportAndSpacing: the same key assigned with an export
// prefix or padded spacing is still the same key.
func TestLintSeesThroughExportAndSpacing(t *testing.T) {
	notes := envLint([]string{"export TOKEN=a", "  TOKEN = b"})
	if len(notes) != 1 {
		t.Fatalf("got %d notes, want 1: %+v", len(notes), notes)
	}
	if notes[0].Line != 0 {
		t.Errorf("flagged line %d, want the earlier occurrence (0)", notes[0].Line)
	}
	if got := "export TOKEN=a"[notes[0].StartCol:notes[0].EndCol]; got != "TOKEN" {
		t.Errorf("note covers %q, want the key after export", got)
	}
}

// TestLintIsCaseSensitive: dotenv lookup is case-sensitive, so PORT and port
// are two different variables, not a duplicate.
func TestLintIsCaseSensitive(t *testing.T) {
	if notes := envLint([]string{"PORT=1", "port=2"}); len(notes) != 0 {
		t.Errorf("got %+v, want no notes", notes)
	}
}

func TestLintSingleAssignmentsAreQuiet(t *testing.T) {
	if notes := envLint([]string{"A=1", "B=2", "C=3"}); len(notes) != 0 {
		t.Errorf("got %+v, want no notes", notes)
	}
}

// TestLintRegistered: the language carries the linter, so the editor's
// highlight pass picks it up for .env files.
func TestLintRegistered(t *testing.T) {
	l, ok := lang.ByPath("/p/.env")
	if !ok || l.Lint == nil {
		t.Fatal("the dotenv language must register a Lint")
	}
	if notes := l.Lint([]string{"A=1", "A=2"}); len(notes) != 1 {
		t.Errorf("registered Lint returned %d notes, want 1", len(notes))
	}
}
