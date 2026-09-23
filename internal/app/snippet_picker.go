package app

import (
	"ike/internal/fuzzy"
	"ike/internal/host"
	"ike/internal/palette"
	"ike/internal/snippets"
)

// snippet_picker.go is the live-template picker (#2694, snippets.insert /
// cmd+j — JetBrains' Insert Live Template): the templates the focused buffer's
// language offers, listed in a locked palette mode, the picked one expanded at
// the caret exactly as typing its trigger and pressing Tab would (#1152). It
// is the doorway for the templates whose trigger one does not remember — the
// Tab expansion needs the word typed out, the picker only needs the chord.

// snippetPickerPrefix selects the picker mode inside the palette; opened
// locked only, so the rune has no user-facing prefix story.
const snippetPickerPrefix = '§'

// SnippetPickerMsg opens the live-template picker (snippets.insert).
type SnippetPickerMsg struct{}

// SnippetPickedMsg expands one picked template at the focused editor's caret.
// The body travels with the message rather than an index into the snapshot, so
// a config reload while the palette was open cannot expand a different entry
// than the one the row showed.
type SnippetPickedMsg struct{ Body string }

// snippetPickerMode is the palette Mode listing the buffer's templates; the
// model fills entries before each locked open (the runConfigsMode pattern).
type snippetPickerMode struct {
	entries []snippets.Entry
}

func newSnippetPickerMode() *snippetPickerMode { return &snippetPickerMode{} }

// Prefix implements palette.Mode.
func (s *snippetPickerMode) Prefix() rune { return snippetPickerPrefix }

// Placeholder implements palette.Mode.
func (s *snippetPickerMode) Placeholder() string { return "Insert live template…" }

// Results implements palette.Mode: the snapshot hump-matched over the trigger
// (#2650) — "ife" finds iferr the way the completion popup's matcher does —
// detailing the body preview and badging a language-scoped entry with its
// language. The entry order (language-scoped before global, user before
// built-in: the Lookup precedence) is kept rather than re-sorted by score, so
// an empty query lists exactly what Tab would resolve first.
func (s *snippetPickerMode) Results(query string, _ palette.Context) []palette.Item {
	var items []palette.Item
	for _, e := range s.entries {
		res, ok := fuzzy.MatchHumps(query, e.Trigger)
		if !ok {
			continue
		}
		item := palette.Item{
			Title:  e.Trigger,
			Spans:  res.Positions,
			Score:  res.Score,
			Detail: snippets.Preview(e.Body),
			Msg:    SnippetPickedMsg{Body: e.Body},
		}
		if e.Language != "" {
			item.Badge = e.Language
		}
		items = append(items, item)
	}
	return items
}

// openSnippetPicker fills and opens the locked picker (snippets.insert). A
// buffer whose language has no templates at all says so instead of opening an
// empty list: the notice is the answer to "why is nothing here?" — the
// [[snippets]] entries are where new ones come from.
func (m *Model) openSnippetPicker() {
	ed := m.focusedEditor()
	if ed == nil {
		m.host.Notify(host.Info, "live templates need a focused editor")
		return
	}
	entries := ed.SnippetEntries()
	if len(entries) == 0 {
		m.host.Notify(host.Info, "no live templates for this buffer — add [[snippets]] entries to the config")
		return
	}
	m.snippetPicker.entries = entries
	m.palette.SetSize(m.width, m.height)
	m.palette.OpenLocked(m.paletteContext(), snippetPickerPrefix)
}

// insertPickedSnippet expands one picked template at the focused editor's
// caret. Focus can have moved while the palette was open — without an editor
// there is nothing to expand into, and the pick is dropped.
func (m *Model) insertPickedSnippet(msg SnippetPickedMsg) {
	if ed := m.focusedEditor(); ed != nil {
		ed.InsertSnippet(msg.Body)
	}
}
