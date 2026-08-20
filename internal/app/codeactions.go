package app

import (
	"sort"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"ike/internal/fuzzy"
	"ike/internal/intention"
	ilsp "ike/internal/lsp"
	"ike/internal/palette"
)

// codeactions.go renders the intention popup (lsp.codeAction, #8/#2020)
// through the command palette, like the references list: the bridge delivers
// a CodeActionsMsg, the root model merges it with the built-in intention
// providers, fills this static mode and opens the palette locked to it. An
// LSP entry activates the bridge-built Apply continuation for its offer
// index — the app never touches the manager; a built-in entry dispatches its
// registered command.

// actionsPrefix selects the code-actions mode inside the palette; only ever
// opened locked (no user-facing prefix story).
const actionsPrefix = '!'

// actionPickedMsg is the activation msg of one list entry; index addresses
// the merged entry list, not the raw LSP offer.
type actionPickedMsg struct{ index int }

// actionEntry is one merged row's activation: an index into the LSP offer
// (run via apply) or a registered command id (dispatched by the root model).
type actionEntry struct {
	lspIndex  int    // ≥ 0: index into the offer's Apply continuation
	commandID string // else: the command to dispatch
}

// actionsMode is a palette Mode over the latest merged intention offer.
type actionsMode struct {
	items   []palette.Item
	entries []actionEntry
	apply   func(int) tea.Cmd
}

// Set replaces the offer with the LSP actions alone — the code-lens picker's
// path, and the base the intention merge builds on. Preferred actions sort
// first (stable otherwise, the server's order carries meaning); the detail
// chip shows the action kind.
func (a *actionsMode) Set(msg ilsp.CodeActionsMsg) {
	a.SetMerged(msg, nil)
}

// SetMerged replaces the offer with the full intention list (#2020): LSP
// preferred actions first, then the remaining LSP actions, then the built-in
// items grouped by kind (kinds in first-appearance order, stable within one).
func (a *actionsMode) SetMerged(msg ilsp.CodeActionsMsg, builtins []intention.Item) {
	a.apply = msg.Apply
	idx := make([]int, len(msg.Actions))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(x, y int) bool {
		return msg.Actions[idx[x]].Preferred && !msg.Actions[idx[y]].Preferred
	})
	a.items = a.items[:0]
	a.entries = a.entries[:0]
	for _, i := range idx {
		act := msg.Actions[i]
		title := act.Title
		if act.Preferred {
			title = "★ " + title
		}
		a.push(palette.Item{Title: title, Detail: actionKindLabel(act.Kind)}, actionEntry{lspIndex: i})
	}
	var kinds []string
	byKind := map[string][]intention.Item{}
	for _, it := range builtins {
		if _, seen := byKind[it.Kind]; !seen {
			kinds = append(kinds, it.Kind)
		}
		byKind[it.Kind] = append(byKind[it.Kind], it)
	}
	for _, kind := range kinds {
		for _, it := range byKind[kind] {
			a.push(palette.Item{Title: it.Title, Detail: actionKindLabel(it.Kind)}, actionEntry{lspIndex: -1, commandID: it.CommandID})
		}
	}
}

// push appends one row, wiring its activation msg to the entry index.
func (a *actionsMode) push(item palette.Item, entry actionEntry) {
	item.Msg = actionPickedMsg{index: len(a.entries)}
	a.items = append(a.items, item)
	a.entries = append(a.entries, entry)
}

// Len is the merged row count — the app's empty-offer check before opening.
func (a *actionsMode) Len() int { return len(a.entries) }

// actionKindLabel renders an action kind readably (#309): "quickfix" →
// "quick fix", "source.organizeImports" → "source · organize imports",
// "refactor.extract" → "refactor · extract". The built-in intention kinds
// (#2020: "copy", "http", "vcs", "test", …) are already lower-case words and
// pass through unchanged. Servers may omit the kind entirely; the row then
// shows a generic "action" so the chip column stays meaningful.
func actionKindLabel(kind string) string {
	if kind == "" {
		return "action"
	}
	if kind == "quickfix" {
		return "quick fix"
	}
	parts := strings.Split(kind, ".")
	for i, p := range parts {
		var out []rune
		for j, r := range p {
			if unicode.IsUpper(r) && j > 0 {
				out = append(out, ' ')
			}
			out = append(out, unicode.ToLower(r))
		}
		parts[i] = string(out)
	}
	return strings.Join(parts, " · ")
}

// CommandFor resolves a picked built-in entry to its command id ("" for an
// LSP entry, which Run handles instead).
func (a *actionsMode) CommandFor(msg actionPickedMsg) string {
	if msg.index < 0 || msg.index >= len(a.entries) {
		return ""
	}
	return a.entries[msg.index].commandID
}

// Run resolves a picked LSP entry to the bridge continuation.
func (a *actionsMode) Run(msg actionPickedMsg) tea.Cmd {
	if a.apply == nil || msg.index < 0 || msg.index >= len(a.entries) {
		return nil
	}
	e := a.entries[msg.index]
	if e.lspIndex < 0 {
		return nil
	}
	return a.apply(e.lspIndex)
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
