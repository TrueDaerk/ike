package app

import (
	"os"
	"sort"
	"strconv"
	"strings"

	"ike/internal/editor"
	"ike/internal/fuzzy"
	"ike/internal/marks"
	"ike/internal/palette"
	"ike/internal/pane"
)

// bookmarks.go implements the bookmarks picker (#1151, nav.bookmarks): a
// palette mode listing the focused editor's local vim marks (m{a-z}) and the
// persistent global marks (m{A-Z}). Enter jumps — global rows through the
// standard open funnel, so the navigation history records — and shift+delete
// (or the row's "✕" zone) removes the mark, the #842/#1113 prune pattern.

// bookmarksPrefix selects the bookmarks mode. Only ever opened locked, so
// the rune has no user-facing prefix story.
const bookmarksPrefix = '\''

// ShowBookmarksMsg asks the root model to open the palette locked to the
// bookmarks picker. Dispatched by nav.bookmarks (palette-only).
type ShowBookmarksMsg struct{}

// BookmarkJumpMsg jumps to a picked mark: local marks resolve on the focused
// editor, global marks open Path through the standard open funnel.
type BookmarkJumpMsg struct {
	Local  bool
	Letter rune
	Path   string
	Line   int
	Col    int
}

// BookmarkRemoveMsg is the aux action of a bookmarks row: remove the mark
// without closing the palette.
type BookmarkRemoveMsg struct {
	Local  bool
	Letter rune
}

// bookmarksMode is a palette Mode over the mark snapshot taken when the
// picker opened (Set); removal rebuilds the snapshot in place.
type bookmarksMode struct {
	items []palette.Item
}

// Set replaces the mode's rows: the focused editor's local marks (ed may be
// nil) followed by the global store's, letters sorted within each group.
func (b *bookmarksMode) Set(ed *editor.Model, store *marks.Store) {
	b.items = nil
	if ed != nil {
		for _, lm := range ed.LocalMarks() {
			b.items = append(b.items, palette.Item{
				Title:  markTitle(lm.Letter, ed.Path(), lm.Line),
				Detail: markPreview(ed.LineText(lm.Line)),
				Msg:    BookmarkJumpMsg{Local: true, Letter: lm.Letter, Line: lm.Line, Col: lm.Col},
				Aux:    BookmarkRemoveMsg{Local: true, Letter: lm.Letter},
			})
		}
	}
	if store != nil {
		for _, e := range store.All() {
			b.items = append(b.items, palette.Item{
				Title:  markTitle(e.Letter, e.Path, e.Line),
				Detail: markPreview(fileLine(e.Path, e.Line)),
				Msg:    BookmarkJumpMsg{Letter: e.Letter, Path: e.Path, Line: e.Line, Col: e.Col},
				Aux:    BookmarkRemoveMsg{Letter: e.Letter},
			})
		}
	}
	sort.SliceStable(b.items, func(i, j int) bool { return b.items[i].Title < b.items[j].Title })
}

// markTitle renders the row label: "'a  path:12" (1-based line). A pathless
// scratch buffer shows only the letter.
func markTitle(letter rune, path string, line int) string {
	t := "'" + string(letter)
	if path != "" {
		t += "  " + displayPath(path) + ":" + strconv.Itoa(line+1)
	}
	return t
}

// markPreview compacts a marked line for the row's detail chip, the
// paste-history preview treatment.
func markPreview(line string) string {
	line = strings.TrimSpace(strings.ReplaceAll(line, "\t", " "))
	if runes := []rune(line); len(runes) > previewMax {
		line = string(runes[:previewMax-1]) + "…"
	}
	return line
}

// fileLineMaxBytes caps the preview read of a global mark's file: previews
// are a nicety, not worth loading a huge file for.
const fileLineMaxBytes = 1 << 20 // 1 MiB

// fileLine reads 0-based line of path best-effort ("" on any miss) for the
// preview of a global mark whose file is not open.
func fileLine(path string, line int) string {
	info, err := os.Stat(path)
	if err != nil || info.Size() > fileLineMaxBytes {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if line < 0 || line >= len(lines) {
		return ""
	}
	return strings.TrimSuffix(lines[line], "\r")
}

// Prefix implements palette.Mode.
func (b *bookmarksMode) Prefix() rune { return bookmarksPrefix }

// Placeholder implements palette.Mode.
func (b *bookmarksMode) Placeholder() string { return "Jump to bookmark…" }

// Results implements palette.Mode: the snapshot fuzzy-matched on the row
// title; an empty query lists all, letters sorted.
func (b *bookmarksMode) Results(query string, cx palette.Context) []palette.Item {
	type scored struct {
		item  palette.Item
		score int
	}
	var out []scored
	for _, it := range b.items {
		m, ok := fuzzy.Match(query, it.Title)
		if !ok {
			continue
		}
		it.Spans = m.Positions
		out = append(out, scored{item: it, score: m.Score})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	items := make([]palette.Item, len(out))
	for i, s := range out {
		items[i] = s.item
	}
	return items
}

// markHooks returns the editor-facing global-mark closures (#1151), the
// breakpointHooks pattern: they capture the store pointer so every view
// shares the live, persisted set.
func markHooks(store *marks.Store) (set func(r rune, path string, line, col int), lines func(path string) []int, adjust func(path string, cursorAfter, delta int)) {
	set = store.Set
	lines = store.Lines
	adjust = store.AdjustEdit
	return set, lines, adjust
}

// focusedEditor returns the focused pane's editor, nil when the focus is not
// an editor pane.
func (m *Model) focusedEditor() *editor.Model {
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindEditor {
		return nil
	}
	return inst.Editor()
}
