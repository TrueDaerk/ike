package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/lang"
)

// Test languages for the space-after-punctuation aid (#1326): one that
// declares ":" like JSON does, one with a line comment, and a host language
// whose embedded regions are the declaring language (the .http body shape).
func init() {
	lang.Register(lang.Language{ID: "sptest", Extensions: []string{"sptest"}, SpaceAfter: []rune{':'}})
	lang.Register(lang.Language{
		ID: "sptest-c", Extensions: []string{"sptestc"},
		SpaceAfter: []rune{':'}, LineComment: "//",
	})
	lang.Register(lang.Language{ID: "sptest-plain", Extensions: []string{"sptestplain"}})
	lang.Register(lang.Language{
		ID: "sptest-host", Extensions: []string{"sptesthost"},
		Regions: func(lines []string) []lang.Region {
			// Everything from a "BODY" line onward is an embedded sptest region.
			for i, ln := range lines {
				if ln == "BODY" {
					return []lang.Region{{Lang: "sptest", StartLine: i + 1, EndLine: len(lines) - 1}}
				}
			}
			return nil
		},
	})
}

func TestSpaceAfterColonInsertsSpace(t *testing.T) {
	m := loadedExt(t, "sptest", "{}\n")
	m.cursor = buffer.Position{Line: 0, Col: 1}
	m = send(m, key('i'), key('"'), key('a'))
	// The auto-closed quote leaves the caret inside; step past it and type ":".
	m = send(m, key('"'), key(':'))
	if got := line(m, 0); got != `{"a": }` {
		t.Fatalf("line = %q, want %q", got, `{"a": }`)
	}
	if m.cursor.Col != 6 {
		t.Fatalf("cursor col = %d, want 6 (after the inserted space)", m.cursor.Col)
	}
}

func TestSpaceAfterColonAtLineEnd(t *testing.T) {
	m := loadedExt(t, "sptest", "\n")
	m = send(m, key('i'), key(':'))
	if got := line(m, 0); got != ": " {
		t.Fatalf("line = %q, want %q", got, ": ")
	}
}

func TestSpaceAfterColonSuppressedInStringsAndComments(t *testing.T) {
	for _, tc := range []struct{ name, ext, content, want string }{
		{"inside a string", "sptest", `"a`, `"a:`},
		{"after a closed string", "sptest", `"a"`, `"a": `},
		{"inside a line comment", "sptestc", `// note`, `// note:`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := loadedExt(t, tc.ext, tc.content+"\n")
			m = send(m, key('A'), key(':'))
			if got := line(m, 0); got != tc.want {
				t.Fatalf("line = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSpaceAfterColonSkipsExistingSeparator(t *testing.T) {
	m := loadedExt(t, "sptest", "a b\n")
	m.cursor = buffer.Position{Line: 0, Col: 1}
	m = send(m, key('i'), key(':'))
	if got := line(m, 0); got != "a: b" {
		t.Fatalf("line = %q, want %q", got, "a: b")
	}
}

func TestSpaceAfterColonNeedsTheLanguageAndTheSetting(t *testing.T) {
	// A language that declares nothing types the bare colon.
	m := loadedExt(t, "sptestplain", "\n")
	m = send(m, key('i'), key(':'))
	if got := line(m, 0); got != ":" {
		t.Fatalf("undeclared language: line = %q, want %q", got, ":")
	}
	// The setting turns the aid off for declaring languages too.
	m = loadedExt(t, "sptest", "\n")
	m.spaceAfterPunct = false
	m = send(m, key('i'), key(':'))
	if got := line(m, 0); got != ":" {
		t.Fatalf("aid off: line = %q, want %q", got, ":")
	}
}

// The aid follows the embedded region's language (#1304), which is the case
// the issue came from: a JSON body inside a .http request.
func TestSpaceAfterColonInEmbeddedRegion(t *testing.T) {
	m := loadedExt(t, "sptesthost", "HEAD\nBODY\nx\n")
	// Line 0 is host text (no rules), line 2 is inside the region.
	m.cursor = buffer.Position{Line: 0, Col: 0}
	m = send(m, key('A'), key(':'))
	if got := line(m, 0); got != "HEAD:" {
		t.Fatalf("host line = %q, want %q", got, "HEAD:")
	}
	m = send(m, special(tea.KeyEscape))
	m.cursor = buffer.Position{Line: 2, Col: 0}
	m = send(m, key('A'), key(':'))
	if got := line(m, 2); got != "x: " {
		t.Fatalf("region line = %q, want %q", got, "x: ")
	}
}

func TestSpaceAfterColonMultiCaret(t *testing.T) {
	m := loadedExt(t, "sptest", "a\nb\n")
	m.cursor = buffer.Position{Line: 0, Col: 1}
	m.addCaret(buffer.Position{Line: 1, Col: 1})
	m = send(m, key('i'), key(':'))
	for i, want := range []string{"a: ", "b: "} {
		if got := line(m, i); got != want {
			t.Fatalf("line %d = %q, want %q", i, got, want)
		}
	}
}
