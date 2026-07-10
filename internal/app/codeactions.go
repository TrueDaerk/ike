package app

import (
	"sort"

	tea "charm.land/bubbletea/v2"

	"ike/internal/fuzzy"
	ilsp "ike/internal/lsp"
	"ike/internal/palette"
)

// codeactions.go renders LSP code actions (lsp.codeAction, #8) through the
// command palette, like the references list: the bridge delivers a
// CodeActionsMsg, the root model fills this static mode and opens the palette
// locked to it. Activation runs the bridge-built Apply continuation for the
// chosen index — the app never touches the manager.

// actionsPrefix selects the code-actions mode inside the palette; only ever
// opened locked (no user-facing prefix story).
const actionsPrefix = '!'

// actionPickedMsg is the activation msg of one list entry.
type actionPickedMsg struct{ index int }

// actionsMode is a palette Mode over the latest code-action offer.
type actionsMode struct {
	items []palette.Item
	apply func(int) tea.Cmd
}

// Set replaces the offer. Preferred actions sort first (stable otherwise, the
// server's order carries meaning); the detail chip shows the action kind.
func (a *actionsMode) Set(msg ilsp.CodeActionsMsg) {
	a.apply = msg.Apply
	idx := make([]int, len(msg.Actions))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(x, y int) bool {
		return msg.Actions[idx[x]].Preferred && !msg.Actions[idx[y]].Preferred
	})
	a.items = make([]palette.Item, len(idx))
	for n, i := range idx {
		act := msg.Actions[i]
		title := act.Title
		if act.Preferred {
			title = "★ " + title
		}
		a.items[n] = palette.Item{
			Title:  title,
			Detail: act.Kind,
			Msg:    actionPickedMsg{index: i},
		}
	}
}

// Run resolves a picked entry to the bridge continuation.
func (a *actionsMode) Run(msg actionPickedMsg) tea.Cmd {
	if a.apply == nil {
		return nil
	}
	return a.apply(msg.index)
}

// Prefix implements palette.Mode.
func (a *actionsMode) Prefix() rune { return actionsPrefix }

// Placeholder implements palette.Mode.
func (a *actionsMode) Placeholder() string { return "Code actions…" }

// Results implements palette.Mode: the offered actions fuzzy-matched on title
// (an empty query lists all, preferred first).
func (a *actionsMode) Results(query string, cx palette.Context) []palette.Item {
	type scored struct {
		item  palette.Item
		score int
	}
	var out []scored
	for _, it := range a.items {
		if m, ok := fuzzy.Match(query, it.Title); ok {
			it.Spans = m.Positions
			out = append(out, scored{item: it, score: m.Score})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	items := make([]palette.Item, len(out))
	for i, s := range out {
		items[i] = s.item
	}
	return items
}
