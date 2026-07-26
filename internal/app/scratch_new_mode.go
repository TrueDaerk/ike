package app

import (
	"sort"

	"ike/internal/fuzzy"
	"ike/internal/lang"
	"ike/internal/palette"
)

// scratch_new_mode.go is the language picker behind scratch.new (#1223).
// Before, scratch.new always produced a .txt file and the language variants
// lived as separate palette commands only — so the bound entry point
// (cmd+shift+n, File → New Scratch File) could never create a Python or PHP
// scratch. The picker lists the registered languages and delegates to the
// very same scratch.new.<id> commands, so there is still exactly one creation
// path.

// scratchNewPrefix selects the language picker inside the palette; the root
// model only ever opens it locked, so the rune has no user-facing prefix story
// — it merely has to be unique among the modes.
const scratchNewPrefix = '+'

// ShowNewScratchMsg asks the root model to open the scratch-language picker.
type ShowNewScratchMsg struct{}

// scratchNewMode is the palette Mode listing the scratch languages: plain text
// first, then every registered language that has an extension. The registry is
// read on every Results call (lazy, like scratchCommands), so late-registered
// languages — plugins, tests — appear without ordering constraints.
type scratchNewMode struct{}

// Prefix implements palette.Mode.
func (scratchNewMode) Prefix() rune { return scratchNewPrefix }

// Placeholder implements palette.Mode.
func (scratchNewMode) Placeholder() string { return "New scratch file: language…" }

// Results implements palette.Mode: rows are fuzzy-matched on the language
// title, "Plain Text" is pinned to the top of the unfiltered list (the
// JetBrains default), and the detail column shows the extension the scratch
// will get. Activating a row runs the matching scratch.new[.<id>] command, so
// the picker owns no creation logic of its own.
func (scratchNewMode) Results(query string, _ palette.Context) []palette.Item {
	type row struct {
		title, ext, id string
		// rank breaks ties for equal fuzzy scores: plain text first, then
		// alphabetically by title.
		rank int
	}
	rows := []row{{title: "Plain Text", ext: "txt", id: scratchTextCommandID, rank: 0}}
	for _, l := range lang.All() {
		if len(l.Extensions) == 0 {
			continue
		}
		rows = append(rows, row{
			title: langTitle(l.ID),
			ext:   l.Extensions[0],
			id:    "scratch.new." + l.ID,
			rank:  1,
		})
	}
	type scored struct {
		item  palette.Item
		score int
		rank  int
		title string
	}
	var out []scored
	for _, r := range rows {
		m, ok := fuzzy.Match(query, r.title)
		if !ok {
			continue
		}
		out = append(out, scored{
			item: palette.Item{
				Title:  r.title,
				Detail: "." + r.ext,
				Spans:  m.Positions,
				Score:  m.Score,
				Msg:    palette.RunCommandMsg{ID: r.id},
			},
			score: m.Score,
			rank:  r.rank,
			title: r.title,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		if out[i].rank != out[j].rank {
			return out[i].rank < out[j].rank
		}
		return out[i].title < out[j].title
	})
	items := make([]palette.Item, len(out))
	for i, sc := range out {
		items[i] = sc.item
	}
	return items
}
