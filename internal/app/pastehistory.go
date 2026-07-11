package app

import (
	"sort"
	"strings"

	"ike/internal/editor/register"
	"ike/internal/fuzzy"
	"ike/internal/palette"
)

// pastehistory.go implements paste-from-history (#57, JetBrains Cmd+Shift+V):
// a palette mode over the focused editor's yank/delete history. Picking an
// entry makes it the current clipboard and pastes it exactly like Cmd+V.

// pasteHistPrefix selects the paste-history mode. Only ever opened locked, so
// the rune has no user-facing prefix story.
const pasteHistPrefix = '^'

// PasteHistoryEntryMsg pastes history entry Index into the focused editor.
// Emitted by the paste-history palette rows.
type PasteHistoryEntryMsg struct{ Index int }

// pasteHistMode is a palette Mode over the history snapshot taken when the
// picker opened (Set); rows keep their history index so activation is stable
// under filtering.
type pasteHistMode struct {
	items []palette.Item
}

// Set replaces the mode's rows with a preview per history entry, newest first.
func (p *pasteHistMode) Set(entries []register.Entry) {
	p.items = make([]palette.Item, len(entries))
	for i, e := range entries {
		p.items[i] = palette.Item{
			Title:  entryPreview(e),
			Detail: entryDetail(e),
			Msg:    PasteHistoryEntryMsg{Index: i},
		}
	}
}

// entryPreview renders the entry's first line, whitespace-compacted and
// capped like the references preview chip.
func entryPreview(e register.Entry) string {
	line, _, _ := strings.Cut(e.Text, "\n")
	line = strings.TrimSpace(strings.ReplaceAll(line, "\t", " "))
	if line == "" {
		line = "(whitespace)"
	}
	if runes := []rune(line); len(runes) > previewMax {
		line = string(runes[:previewMax-1]) + "…"
	}
	return line
}

// entryDetail summarizes the entry's size ("3 lines" / "1 line" / "12 chars").
func entryDetail(e register.Entry) string {
	if n := strings.Count(e.Text, "\n"); n > 0 || e.Linewise {
		lines := n
		if !strings.HasSuffix(e.Text, "\n") || lines == 0 {
			lines++
		}
		return plural(lines, "line", "lines")
	}
	return plural(len([]rune(e.Text)), "char", "chars")
}

// Prefix implements palette.Mode.
func (p *pasteHistMode) Prefix() rune { return pasteHistPrefix }

// Placeholder implements palette.Mode.
func (p *pasteHistMode) Placeholder() string { return "Paste from history…" }

// Results implements palette.Mode: the snapshot fuzzy-matched on the preview
// text; an empty query lists all, newest first.
func (p *pasteHistMode) Results(query string, cx palette.Context) []palette.Item {
	type scored struct {
		item  palette.Item
		score int
	}
	var out []scored
	for _, it := range p.items {
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
