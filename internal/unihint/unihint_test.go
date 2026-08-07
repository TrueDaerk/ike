package unihint

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"ike/internal/lang"
)

// TestPlaceholderClasses: every character class in the issue has a one-cell
// stand-in; ordinary runes have none.
func TestPlaceholderClasses(t *testing.T) {
	cases := []struct {
		r    rune
		want string
	}{
		{0x200B, "∅"}, // zero-width space
		{0x200C, "∅"}, // zero-width non-joiner
		{0x200D, "∅"}, // zero-width joiner
		{0x2060, "∅"}, // word joiner
		{0xFEFF, "∅"}, // BOM
		{0x00A0, "⍽"}, // NBSP — distinct from the space glyph ·
		{0x202F, "⍽"}, // narrow NBSP
		{0x00AD, "-"}, // soft hyphen
		{0x202E, "◊"}, // right-to-left override
		{0x2066, "◊"}, // left-to-right isolate
		{0x2069, "◊"}, // pop directional isolate
		{0x200E, "◊"}, // left-to-right mark
		{0x061C, "◊"}, // arabic letter mark
	}
	for _, c := range cases {
		got, ok := Placeholder(c.r)
		if !ok || got != c.want {
			t.Errorf("Placeholder(%U) = %q, %v; want %q", c.r, got, ok, c.want)
		}
	}
	for _, r := range []rune{'a', 'ß', '中', 'а', ' ', '\t'} {
		if g, ok := Placeholder(r); ok {
			t.Errorf("Placeholder(%U) = %q; ordinary runes must have none", r, g)
		}
	}
}

// TestPlaceholderWidths guards the render loop's one-rune-one-cell invariant:
// every glyph in the table measures exactly one display cell.
func TestPlaceholderWidths(t *testing.T) {
	for r, iv := range invisibles {
		if w := lipgloss.Width(iv.glyph); w != 1 {
			t.Errorf("glyph %q for %U measures %d cells; want 1", iv.glyph, r, w)
		}
	}
}

// TestNotesPureASCII: an ASCII buffer produces nothing.
func TestNotesPureASCII(t *testing.T) {
	if ns := Notes([]string{"plain text", "x := y + 1", ""}); ns != nil {
		t.Errorf("Notes(ascii) = %+v; want nil", ns)
	}
}

// noteFor returns the single note covering col on line 0 of a one-line buffer.
func noteFor(t *testing.T, line string, col int) lang.Note {
	t.Helper()
	for _, n := range Notes([]string{line}) {
		if n.Line == 0 && col >= n.StartCol && col < n.EndCol {
			return n
		}
	}
	t.Fatalf("no note covering col %d in %q (notes: %+v)", col, line, Notes([]string{line}))
	return lang.Note{}
}

// TestNotesInvisibleClasses: each invisible class yields a note with the
// character's name and codepoint, at the right severity.
func TestNotesInvisibleClasses(t *testing.T) {
	cases := []struct {
		line    string
		col     int
		sev     int
		message string
	}{
		{"a\u200bb", 1, lang.NoteWarn, "zero-width space (U+200B)"},
		{"a\ufeffb", 1, lang.NoteWarn, "zero-width no-break space (BOM) (U+FEFF)"},
		{"a\u2060b", 1, lang.NoteWarn, "word joiner (U+2060)"},
		{"a\u00a0b", 1, lang.NoteInfo, "no-break space (U+00A0)"},
		{"a\u00adb", 1, lang.NoteInfo, "soft hyphen (U+00AD)"},
		{"a\u200cb", 1, lang.NoteWarn, "zero-width non-joiner (U+200C)"},
	}
	for _, c := range cases {
		n := noteFor(t, c.line, c.col)
		if n.Severity != c.sev || !strings.Contains(n.Message, c.message) {
			t.Errorf("note for %q = sev %d %q; want sev %d containing %q",
				c.line, n.Severity, n.Message, c.sev, c.message)
		}
	}
}

// TestNotesBidiControls: every bidi control warns and says why — the
// Trojan-Source class.
func TestNotesBidiControls(t *testing.T) {
	for _, r := range []rune{0x202A, 0x202B, 0x202C, 0x202D, 0x202E, 0x2066, 0x2067, 0x2068, 0x2069, 0x200E, 0x200F, 0x061C} {
		n := noteFor(t, "x"+string(r)+"y", 1)
		if n.Severity != lang.NoteWarn {
			t.Errorf("bidi %U: severity %d; want warning", r, n.Severity)
		}
		if !strings.Contains(n.Message, "bidi control character") || !strings.Contains(n.Message, "reorder") {
			t.Errorf("bidi %U: message %q; want the bidi explanation", r, n.Message)
		}
	}
}

// TestNotesRunCollapse: a stretch of identical invisibles is one note with a
// count, not one note per rune.
func TestNotesRunCollapse(t *testing.T) {
	ns := Notes([]string{"a\u00a0\u00a0\u00a0b"})
	if len(ns) != 1 {
		t.Fatalf("got %d notes; want 1 collapsed run", len(ns))
	}
	if ns[0].StartCol != 1 || ns[0].EndCol != 4 || !strings.Contains(ns[0].Message, "× 3") {
		t.Errorf("run note = %+v; want cols [1,4) with × 3", ns[0])
	}
}

// TestJoinerContextDowngrades: ZWNJ/ZWJ between non-ASCII runes is legitimate
// typography (Persian, emoji families) — info, not warning; the same rune
// spliced into ASCII stays a warning.
func TestJoinerContextDowngrades(t *testing.T) {
	if n := noteFor(t, "می\u200cخواهم", 2); n.Severity != lang.NoteInfo {
		t.Errorf("ZWNJ in Persian: severity %d; want info", n.Severity)
	}
	if n := noteFor(t, "a\u200db", 1); n.Severity != lang.NoteWarn {
		t.Errorf("ZWJ between ASCII: severity %d; want warning", n.Severity)
	}
}

// TestConfusableMixedToken: a Cyrillic look-alike inside an ASCII identifier
// flags the whole token and names the offender.
func TestConfusableMixedToken(t *testing.T) {
	n := noteFor(t, "if pаssword == input {", 3) // Cyrillic а at index 5
	if n.StartCol != 3 || n.EndCol != 11 {
		t.Errorf("token range = [%d,%d); want [3,11)", n.StartCol, n.EndCol)
	}
	if n.Severity != lang.NoteWarn {
		t.Errorf("severity %d; want warning", n.Severity)
	}
	for _, want := range []string{"mixed-script identifier", "Cyrillic 'а' (U+0430)"} {
		if !strings.Contains(n.Message, want) {
			t.Errorf("message %q; want it to contain %q", n.Message, want)
		}
	}
}

// TestConfusableGreek: Greek look-alikes flag too, attributed to Greek.
func TestConfusableGreek(t *testing.T) {
	n := noteFor(t, "tοken := 1", 0) // Greek omicron at index 1
	if !strings.Contains(n.Message, "Greek 'ο' (U+03BF)") {
		t.Errorf("message %q; want the Greek attribution", n.Message)
	}
}

// TestLegitimateTextNotFlagged: pure non-Latin words — even ones written
// entirely in look-alike letters — and non-look-alike mixes stay silent.
func TestLegitimateTextNotFlagged(t *testing.T) {
	for _, line := range []string{
		"// привет мир",       // ordinary Russian
		"оно",                 // Cyrillic word made entirely of look-alikes
		"δ := 0.1",            // pure Greek identifier
		"Δt := 3",             // Greek letter that resembles no ASCII, mixed with ASCII
		"straße := 1",         // Latin-script non-ASCII
		"München",             // Latin-script non-ASCII
		"名前 := \"日本語\"",       // CJK
		"ασφάλεια := \"key\"", // pure Greek word next to ASCII in a *different* token
	} {
		for _, n := range Notes([]string{line}) {
			if strings.Contains(n.Message, "mixed-script") {
				t.Errorf("line %q flagged: %q", line, n.Message)
			}
		}
	}
}

// TestUTF8ColumnMapping: columns are rune columns, not byte offsets — a
// multi-byte rune before the finding must not skew the range.
func TestUTF8ColumnMapping(t *testing.T) {
	// "日本" is two runes (6 bytes); the ZWSP sits at rune column 2.
	n := noteFor(t, "日本\u200bx", 2)
	if n.StartCol != 2 || n.EndCol != 3 {
		t.Errorf("range = [%d,%d); want rune columns [2,3)", n.StartCol, n.EndCol)
	}
}

// TestMultipleFindingsOneLine: invisibles and confusables coexist on one line.
func TestMultipleFindingsOneLine(t *testing.T) {
	ns := Notes([]string{"pаss\u200bword"})
	var mixed, invis bool
	for _, n := range ns {
		if strings.Contains(n.Message, "mixed-script") {
			mixed = true
		}
		if strings.Contains(n.Message, "zero-width space") {
			invis = true
		}
	}
	if !mixed || !invis {
		t.Errorf("notes = %+v; want both a mixed-script and an invisible finding", ns)
	}
}
