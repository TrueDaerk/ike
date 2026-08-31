package editor

// Tests for the structural jq search mode on the "/" line (#2363): the \j
// marker, the ctrl+x toggle, match navigation and the inline error.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

const usersJSON = "{\n  \"users\": [\n    {\"name\": \"ada\"},\n    {\"name\": \"grace\"}\n  ]\n}"

// TestStructuralSearchCommitAndRepeat (#2363): a jq query typed behind the \j
// marker previews its matches, commits like a text search, and n steps
// through the selected nodes.
func TestStructuralSearchCommitAndRepeat(t *testing.T) {
	m := jsonLoaded(t, usersJSON)
	m = send(m, key('/'))
	m = typeKeys(m, `\j.users[].name`)
	if !m.preview.IsStructural() {
		t.Fatal("preview must be structural behind the \\j marker")
	}
	if m.cursor.Line != 2 || m.cursor.Col != 13 {
		t.Fatalf("incsearch landing = %v, want line 2 col 13", m.cursor)
	}
	m = send(m, special(tea.KeyEnter))
	if !m.HasSearch() {
		t.Fatal("commit must install the structural query")
	}
	m = send(m, key('n'))
	if m.cursor.Line != 3 || m.cursor.Col != 13 {
		t.Fatalf("n landing = %v, want line 3 col 13", m.cursor)
	}
	m = send(m, key('n'))
	if m.cursor.Line != 2 {
		t.Fatalf("n past the last match must wrap, cursor = %v", m.cursor)
	}
}

// TestStructuralSearchCounter (#2363): the match tally counts structural
// matches like text ones.
func TestStructuralSearchCounter(t *testing.T) {
	m := jsonLoaded(t, usersJSON)
	m = send(m, key('/'))
	m = typeKeys(m, `\j.users[].name`)
	if got := m.SearchCounter(); got != "1/2" {
		t.Fatalf("SearchCounter = %q, want 1/2", got)
	}
}

// TestStructuralToggleRoundTrip (#2363): ctrl+x flips the \j marker on and
// off, ctrl+c-style, keeping the typed query.
func TestStructuralToggleRoundTrip(t *testing.T) {
	m := jsonLoaded(t, usersJSON)
	m = send(m, key('/'))
	m = typeKeys(m, ".users")
	m = send(m, modKey('x', tea.ModCtrl))
	if m.cmdline != `\j.users` {
		t.Fatalf("toggle on: cmdline = %q, want \\j.users", m.cmdline)
	}
	if !m.preview.IsStructural() {
		t.Fatal("toggle on must recompile the preview structurally")
	}
	m = send(m, modKey('x', tea.ModCtrl))
	if m.cmdline != ".users" {
		t.Fatalf("toggle off: cmdline = %q, want .users", m.cmdline)
	}
	if m.preview.IsStructural() {
		t.Fatal("toggle off must fall back to the text search")
	}
}

// TestStructuralSearchInvalidQuery (#2363): an invalid query reports inline
// while typing and as an ex-line error on commit — never zero silent matches.
func TestStructuralSearchInvalidQuery(t *testing.T) {
	m := jsonLoaded(t, usersJSON)
	m = send(m, key('/'))
	m = typeKeys(m, `\j.users[`)
	if err := m.preview.StructuralErr(); !strings.HasPrefix(err, "jq: ") {
		t.Fatalf("preview error = %q, want a jq error", err)
	}
	m = send(m, special(tea.KeyEnter))
	if !strings.HasPrefix(m.cmdMsg, "E: jq: ") {
		t.Fatalf("cmdMsg = %q, want an E: jq: error", m.cmdMsg)
	}
	if m.HasSearch() {
		t.Fatal("a failed structural commit must leave nothing for n/N")
	}
}

// TestStructuralSearchNeedsDocLang (#2363): the mode reports the JSON/YAML
// gate in a buffer without a document language.
func TestStructuralSearchNeedsDocLang(t *testing.T) {
	m, _ := loaded(t, "plain text\n")
	m = send(m, key('/'))
	m = typeKeys(m, `\j.users`)
	if err := m.preview.StructuralErr(); !strings.Contains(err, "JSON or YAML") {
		t.Fatalf("preview error = %q, want the language gate", err)
	}
}

// TestStructuralSearchYAML (#2363): the same machinery serves YAML buffers.
func TestStructuralSearchYAML(t *testing.T) {
	m := yamlLoaded(t, manifest)
	m = send(m, key('/'))
	m = typeKeys(m, `\j.spec.template.containers[].name`)
	m = send(m, special(tea.KeyEnter))
	if m.cursor.Line != 3 {
		t.Fatalf("cursor = %v, want the name scalar on line 3", m.cursor)
	}
}
