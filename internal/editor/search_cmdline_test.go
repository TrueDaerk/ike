package editor

// Tests for the search command line's cursor editing (#1110) and the
// case-sensitivity toggle / default-insensitive setting (#1111).

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/search"
	"ike/internal/host"
)

func ctrlC() tea.KeyPressMsg { return modKey('c', tea.ModCtrl) }

// --- #1110: cursor editing on the / line -----------------------------------

func TestSearchLineCursorInsertMidQuery(t *testing.T) {
	m, _ := loaded(t, "fo fxo bar\n")
	m = send(m, key('/'))
	m = typeKeys(m, "fo")
	m = send(m, modKey(tea.KeyLeft, 0))
	if m.cmdCur != 1 {
		t.Fatalf("left: cmdCur=%d want 1", m.cmdCur)
	}
	m = typeKeys(m, "x")
	if m.cmdline != "fxo" || m.cmdCur != 2 {
		t.Fatalf("mid insert: cmdline=%q cmdCur=%d want %q 2", m.cmdline, m.cmdCur, "fxo")
	}
	// Incremental highlighting keeps tracking the mid-query edit: the preview
	// recompiled to the new pattern and the cursor sits on its match.
	if m.preview.Pattern != "fxo" {
		t.Fatalf("preview pattern=%q want %q", m.preview.Pattern, "fxo")
	}
	if m.cursor.Col != 3 {
		t.Fatalf("preview landing col=%d want 3", m.cursor.Col)
	}
	m = send(m, modKey(tea.KeyRight, 0))
	if m.cmdCur != 3 {
		t.Fatalf("right: cmdCur=%d want 3", m.cmdCur)
	}
}

func TestSearchLineWordAndWholeDelete(t *testing.T) {
	m, _ := loaded(t, "alpha beta\n")
	m = send(m, key('/'))
	m = typeKeys(m, "alpha beta")
	m = send(m, tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt})
	if m.cmdline != "alpha " {
		t.Fatalf("alt+backspace: cmdline=%q want %q", m.cmdline, "alpha ")
	}
	if m.preview.Pattern != "alpha " {
		t.Fatalf("alt+backspace preview=%q want %q", m.preview.Pattern, "alpha ")
	}
	m = send(m, tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModSuper})
	if m.cmdline != "" || m.cmdCur != 0 {
		t.Fatalf("cmd+backspace: cmdline=%q cmdCur=%d want empty at 0", m.cmdline, m.cmdCur)
	}
	// Still on the search line: clearing the text must not leave command mode.
	if m.mode != Command || !m.searching {
		t.Fatal("cmd+backspace must clear the query, not close the search line")
	}
	// A further backspace on the now-empty line closes it, as before.
	m = send(m, special(tea.KeyBackspace))
	if m.mode != Normal || m.searching {
		t.Fatal("backspace on the empty line must leave the search")
	}
}

func TestSearchLineMidQueryCommit(t *testing.T) {
	m, _ := loaded(t, "foo bar foo\n")
	m = send(m, key('/'))
	m = typeKeys(m, "fo")
	m = send(m, modKey(tea.KeyLeft, 0), modKey(tea.KeyLeft, 0))
	m = typeKeys(m, "x") // "xfo", cursor after x
	m = send(m, special(tea.KeyBackspace))
	if m.cmdline != "fo" || m.cmdCur != 0 {
		t.Fatalf("backspace at cursor: cmdline=%q cmdCur=%d want %q 0", m.cmdline, m.cmdCur, "fo")
	}
	m = send(m, special(tea.KeyEnter))
	if m.query.Pattern != "fo" {
		t.Fatalf("committed pattern=%q want %q", m.query.Pattern, "fo")
	}
}

func TestCommandLineRowRendersMidLineCursor(t *testing.T) {
	m, _ := loaded(t, "foo\n")
	m = send(m, key('/'))
	m = typeKeys(m, "ab")
	m = send(m, modKey(tea.KeyLeft, 0))
	row := m.commandLineRow()
	// The reverse-video cursor sits on "b", not appended after the text.
	if !strings.Contains(row, "a\x1b") || !strings.Contains(row, "b") {
		t.Fatalf("row %q must render the cursor on the b", row)
	}
	if strings.Contains(strings.TrimSuffix(row, "\x1b[0m"), "ab\x1b[7m") {
		t.Fatalf("row %q still renders an appended end-of-line cursor", row)
	}
}

func TestExLineCursorEditing(t *testing.T) {
	// The ":" line shares the helper: mid-line insertion works there too.
	m, _ := loaded(t, "foo\n")
	m = typeKeys(m, ":e")
	m = send(m, modKey(tea.KeyLeft, 0))
	m = typeKeys(m, "s")
	if m.cmdline != "se" || m.cmdCur != 1 {
		t.Fatalf("ex mid insert: cmdline=%q cmdCur=%d want %q 1", m.cmdline, m.cmdCur, "se")
	}
}

// --- #1111: ctrl+c toggle + default-insensitive setting --------------------

func TestSearchCaseToggleRoundTrip(t *testing.T) {
	m, _ := loaded(t, "FOO foo\n")
	m = send(m, key('/'))
	m = typeKeys(m, "FOO")
	// Smartcase: an uppercase pattern matches exactly — one match.
	if got := len(m.preview.AllMatches(m.buf)); got != 1 {
		t.Fatalf("exact FOO matches=%d want 1", got)
	}
	m = send(m, ctrlC())
	if m.cmdline != `\cFOO` {
		t.Fatalf("ctrl+c: cmdline=%q want %q", m.cmdline, `\cFOO`)
	}
	if got := len(m.preview.AllMatches(m.buf)); got != 2 {
		t.Fatalf(`\cFOO matches=%d want 2`, got)
	}
	m = send(m, ctrlC())
	if m.cmdline != "FOO" {
		t.Fatalf("ctrl+c round trip: cmdline=%q want %q", m.cmdline, "FOO")
	}
	if got := len(m.preview.AllMatches(m.buf)); got != 1 {
		t.Fatalf("round-trip matches=%d want 1", got)
	}
	// The cursor tracks the marker insertion/removal.
	if m.cmdCur != 3 {
		t.Fatalf("cmdCur=%d want 3 after round trip", m.cmdCur)
	}
}

func TestSearchCaseToggleReplacesForcedExact(t *testing.T) {
	m, _ := loaded(t, "FOO foo\n")
	m = send(m, key('/'))
	m = typeKeys(m, `\Cfoo`)
	if got := len(m.preview.AllMatches(m.buf)); got != 1 {
		t.Fatalf(`\Cfoo matches=%d want 1`, got)
	}
	m = send(m, ctrlC())
	if m.cmdline != `\cfoo` {
		t.Fatalf(`ctrl+c on \C: cmdline=%q want %q`, m.cmdline, `\cfoo`)
	}
	if got := len(m.preview.AllMatches(m.buf)); got != 2 {
		t.Fatalf(`\cfoo matches=%d want 2`, got)
	}
}

// insensitiveEditor loads content with editor.search_ignore_case enabled.
func insensitiveEditor(t *testing.T, content string) Model {
	t.Helper()
	m, _ := loadedWith(t, host.MapConfig{"editor.search_ignore_case": "true"}, "f.txt", content)
	return m
}

func TestSearchIgnoreCaseSettingFoldsWithoutMarker(t *testing.T) {
	m := insensitiveEditor(t, "FOO foo\n")
	m = send(m, key('/'))
	m = typeKeys(m, "FOO")
	// Setting on: even an uppercase pattern folds case without a marker.
	if got := len(m.preview.AllMatches(m.buf)); got != 2 {
		t.Fatalf("setting on: FOO matches=%d want 2", got)
	}
	// \C forces exact matching over the setting.
	m = send(m, special(tea.KeyEscape), key('/'))
	m = typeKeys(m, `\CFOO`)
	if got := len(m.preview.AllMatches(m.buf)); got != 1 {
		t.Fatalf(`setting on: \CFOO matches=%d want 1`, got)
	}
}

func TestSearchCaseToggleWithSettingOn(t *testing.T) {
	m := insensitiveEditor(t, "FOO foo\n")
	m = send(m, key('/'))
	m = typeKeys(m, "foo")
	// With the setting on the unmarked toggle target is the sensitive side.
	m = send(m, ctrlC())
	if m.cmdline != `\Cfoo` {
		t.Fatalf("setting on, ctrl+c: cmdline=%q want %q", m.cmdline, `\Cfoo`)
	}
	if got := len(m.preview.AllMatches(m.buf)); got != 1 {
		t.Fatalf(`setting on: \Cfoo matches=%d want 1`, got)
	}
	m = send(m, ctrlC())
	if m.cmdline != `\cfoo` {
		t.Fatalf("second ctrl+c: cmdline=%q want %q", m.cmdline, `\cfoo`)
	}
	m = send(m, ctrlC())
	if m.cmdline != `\Cfoo` {
		t.Fatalf("third ctrl+c: cmdline=%q want %q", m.cmdline, `\Cfoo`)
	}
}

// TestParseSearchPatternMatrix pins the marker/setting/smartcase semantics
// (#1111): \c forces folding, \C forces exact, the setting flips the unmarked
// default, and \v composes with either in any order.
func TestParseSearchPatternMatrix(t *testing.T) {
	cases := []struct {
		line    string
		setting bool
		pattern string
		regex   bool
		cs      search.Case
	}{
		{"foo", false, "foo", false, search.CaseSmart},
		{"foo", true, "foo", false, search.CaseFold},
		{`\cFoo`, false, "Foo", false, search.CaseFold},
		{`\CFoo`, true, "Foo", false, search.CaseExact},
		{`\v[a-z]+`, false, "[a-z]+", true, search.CaseSmart},
		{`\c\vfoo`, false, "foo", true, search.CaseFold},
		{`\v\Cfoo`, true, "foo", true, search.CaseExact},
	}
	for _, c := range cases {
		m := Model{searchIgnoreCase: c.setting}
		pat, regex, cs := m.parseSearchPattern(c.line)
		if pat != c.pattern || regex != c.regex || cs != c.cs {
			t.Errorf("parse(%q, setting=%v) = (%q, %v, %v) want (%q, %v, %v)",
				c.line, c.setting, pat, regex, cs, c.pattern, c.regex, c.cs)
		}
	}
}
