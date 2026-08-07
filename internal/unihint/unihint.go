// Package unihint makes invisible and deceptive Unicode visible (#1654): the
// classic "code looks identical but does not compile / string comparison
// fails" trap. It serves two consumers. The editor's render loop asks
// Placeholder for a one-cell stand-in so an invisible rune never draws as
// nothing (and never desyncs the one-rune-one-cell mapping by reaching the
// terminal as a zero-width glyph). The highlight pass asks Notes for
// diagnostics — invisible characters, bidi controls (the Trojan-Source attack
// class), and mixed-script identifiers hiding ASCII look-alikes — so the
// gutter, underline and Problems flow list every occurrence.
//
// Legitimate non-Latin text must not light up: only invisibles and *mixed*
// tokens are flagged, never a whole script. A pure-Cyrillic word like "оно" is
// written entirely in look-alike letters, yet flagging it would mark ordinary
// Russian prose — so unlike nethint's hostname check, the whole-look-alike
// shape is deliberately not reported here; the attack a buffer scan must catch
// is the look-alike *inside* an ASCII identifier.
package unihint

import (
	"fmt"
	"strings"
	"unicode"

	"ike/internal/lang"
	"ike/internal/nethint"
)

// kind classifies an invisible rune for the note message.
type kind int

const (
	kindInvisible kind = iota // renders as nothing (or as a plain space)
	kindBidi                  // reorders the displayed text
)

// invisible describes one invisible/format rune: its Unicode name, the
// one-cell placeholder glyph the editor renders, the note severity and the
// message class. Glyphs must measure one display cell — the render loop's
// one-rune-one-cell invariant (see internal/editor/ansiescape.go) depends on
// it, and the editor's tests assert it.
type invisible struct {
	name  string
	glyph string
	sev   int
	kind  kind
}

// invisibles is the full table: the spacing characters that render like (or as
// part of) ordinary whitespace, the zero-width family, and every bidi control
// the Trojan-Source paper builds on. NBSP and the soft hyphen are common in
// legitimate prose, so they carry info severity; everything zero-width or
// directional is a warning.
var invisibles = map[rune]invisible{
	0x00A0: {"no-break space", "⍽", lang.NoteInfo, kindInvisible},
	0x202F: {"narrow no-break space", "⍽", lang.NoteInfo, kindInvisible},
	0x00AD: {"soft hyphen", "-", lang.NoteInfo, kindInvisible},
	0x200B: {"zero-width space", "∅", lang.NoteWarn, kindInvisible},
	0x200C: {"zero-width non-joiner", "∅", lang.NoteWarn, kindInvisible},
	0x200D: {"zero-width joiner", "∅", lang.NoteWarn, kindInvisible},
	0x2060: {"word joiner", "∅", lang.NoteWarn, kindInvisible},
	0xFEFF: {"zero-width no-break space (BOM)", "∅", lang.NoteWarn, kindInvisible},
	0x061C: {"arabic letter mark", "◊", lang.NoteWarn, kindBidi},
	0x200E: {"left-to-right mark", "◊", lang.NoteWarn, kindBidi},
	0x200F: {"right-to-left mark", "◊", lang.NoteWarn, kindBidi},
	0x202A: {"left-to-right embedding", "◊", lang.NoteWarn, kindBidi},
	0x202B: {"right-to-left embedding", "◊", lang.NoteWarn, kindBidi},
	0x202C: {"pop directional formatting", "◊", lang.NoteWarn, kindBidi},
	0x202D: {"left-to-right override", "◊", lang.NoteWarn, kindBidi},
	0x202E: {"right-to-left override", "◊", lang.NoteWarn, kindBidi},
	0x2066: {"left-to-right isolate", "◊", lang.NoteWarn, kindBidi},
	0x2067: {"right-to-left isolate", "◊", lang.NoteWarn, kindBidi},
	0x2068: {"first strong isolate", "◊", lang.NoteWarn, kindBidi},
	0x2069: {"pop directional isolate", "◊", lang.NoteWarn, kindBidi},
}

// Placeholder returns the one-cell glyph standing in for an invisible or bidi
// control rune, and whether r is one: ⍽ for the no-break spaces, - for the
// soft hyphen, ∅ for the zero-width family, ◊ for the bidi controls.
func Placeholder(r rune) (string, bool) {
	iv, ok := invisibles[r]
	if !ok {
		return "", false
	}
	return iv.glyph, true
}

// Notes scans a buffer and returns one note per finding: a run of identical
// invisible runes, a bidi control, or a mixed-script identifier token carrying
// ASCII look-alikes. Pure-ASCII lines cost one byte scan and produce nothing.
func Notes(lines []string) []lang.Note {
	var notes []lang.Note
	for i, line := range lines {
		if ascii(line) {
			continue
		}
		runes := []rune(line)
		notes = append(notes, invisibleNotes(i, runes)...)
		notes = append(notes, confusableNotes(i, runes)...)
	}
	return notes
}

// ascii reports whether s is pure ASCII — the fast path that keeps every
// finding class off untouched buffers (NBSP, the lowest rune flagged, is
// already non-ASCII, so no finding can hide in an ASCII line).
func ascii(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// invisibleNotes emits one note per run of identical invisible runes on a
// line — a stretch of NBSP padding is one finding, not thirty. A zero-width
// joiner/non-joiner sitting between two non-ASCII runes is legitimate
// typography (Persian needs ZWNJ, emoji families join with ZWJ), so that
// context downgrades those two runes to info severity.
func invisibleNotes(lineNo int, runes []rune) []lang.Note {
	var notes []lang.Note
	for col := 0; col < len(runes); col++ {
		iv, ok := invisibles[runes[col]]
		if !ok {
			continue
		}
		end := col + 1
		for end < len(runes) && runes[end] == runes[col] {
			end++
		}
		sev := iv.sev
		if (runes[col] == 0x200C || runes[col] == 0x200D) && joiningContext(runes, col, end) {
			sev = lang.NoteInfo
		}
		label := "invisible character"
		if iv.kind == kindBidi {
			label = "bidi control character"
		}
		msg := fmt.Sprintf("%s: %s (U+%04X)", label, iv.name, runes[col])
		if n := end - col; n > 1 {
			msg += fmt.Sprintf(" × %d", n)
		}
		if iv.kind == kindBidi {
			msg += " — can reorder the displayed text"
		}
		notes = append(notes, lang.Note{Line: lineNo, StartCol: col, EndCol: end, Severity: sev, Message: msg})
		col = end - 1
	}
	return notes
}

// joiningContext reports whether the run [start, end) sits between two
// non-ASCII runes — the shape joiner/non-joiner typography actually uses.
func joiningContext(runes []rune, start, end int) bool {
	return start > 0 && runes[start-1] >= 0x80 && end < len(runes) && runes[end] >= 0x80
}

// confusableNotes emits one note per identifier-like token that mixes ASCII
// letters with Cyrillic/Greek ASCII look-alikes — `pаssword` with a Cyrillic
// а. Tokens without ASCII letters (ordinary Russian or Greek words) and
// non-ASCII letters that resemble nothing (Δt, München) never flag.
func confusableNotes(lineNo int, runes []rune) []lang.Note {
	var notes []lang.Note
	for col := 0; col < len(runes); col++ {
		if !tokenRune(runes[col]) {
			continue
		}
		end := col + 1
		for end < len(runes) && tokenRune(runes[end]) {
			end++
		}
		if n, ok := confusableToken(lineNo, runes, col, end); ok {
			notes = append(notes, n)
		}
		col = end - 1
	}
	return notes
}

// tokenRune reports whether r belongs to an identifier-like token.
func tokenRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// confusableToken builds the note for the token [start, end) when it carries
// the homoglyph shape: at least one ASCII letter and at least one look-alike.
func confusableToken(lineNo int, runes []rune, start, end int) (lang.Note, bool) {
	hasASCII := false
	var lookAlikes []rune
	for _, r := range runes[start:end] {
		switch {
		case r < 0x80:
			if unicode.IsLetter(r) {
				hasASCII = true
			}
		case nethint.LatinLookAlike(r):
			lookAlikes = append(lookAlikes, r)
		}
	}
	if !hasASCII || len(lookAlikes) == 0 {
		return lang.Note{}, false
	}
	seen := map[rune]bool{}
	var parts []string
	for _, r := range lookAlikes {
		if seen[r] {
			continue
		}
		seen[r] = true
		parts = append(parts, fmt.Sprintf("%s '%c' (U+%04X)", scriptName(r), r, r))
	}
	msg := fmt.Sprintf("mixed-script identifier %q mixes ASCII with %s",
		string(runes[start:end]), strings.Join(parts, ", "))
	return lang.Note{Line: lineNo, StartCol: start, EndCol: end, Severity: lang.NoteWarn, Message: msg}, true
}

// scriptName names the script of a look-alike for the message; the table only
// holds Cyrillic and Greek letters.
func scriptName(r rune) string {
	if unicode.Is(unicode.Cyrillic, r) {
		return "Cyrillic"
	}
	return "Greek"
}
