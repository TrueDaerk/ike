package keymap

import "sort"

// HelpEntry is one row of the keymap cheatsheet.
type HelpEntry struct {
	Chord   string
	Command string
	Title   string
	Owner   string
	Fragile bool
}

// HelpGroup is the cheatsheet bindings for one context, sorted by chord.
type HelpGroup struct {
	Context Context
	Label   string
	Entries []HelpEntry
}

// Help returns the effective bindings grouped by context (Global first, then
// pane contexts alphabetically), each group sorted by chord. It drives the
// palette.keymapHelp overlay; fragile entries are flagged so the view can show
// the palette fallback.
func (t *BindingTable) Help() []HelpGroup {
	byCtx := map[Context][]HelpEntry{}
	for _, b := range t.bindings {
		byCtx[b.Context] = append(byCtx[b.Context], HelpEntry{
			Chord:   b.Chord.String(),
			Command: b.Command,
			Title:   b.Title,
			Owner:   b.Owner,
			Fragile: b.Fragile,
		})
	}
	ctxs := make([]Context, 0, len(byCtx))
	for c := range byCtx {
		ctxs = append(ctxs, c)
	}
	sort.Slice(ctxs, func(i, j int) bool {
		// Global sorts first; the rest alphabetically.
		if ctxs[i] == Global {
			return true
		}
		if ctxs[j] == Global {
			return false
		}
		return ctxs[i] < ctxs[j]
	})
	groups := make([]HelpGroup, 0, len(ctxs))
	for _, c := range ctxs {
		entries := byCtx[c]
		sort.Slice(entries, func(i, j int) bool { return entries[i].Chord < entries[j].Chord })
		groups = append(groups, HelpGroup{Context: c, Label: contextLabel(c), Entries: entries})
	}
	return groups
}
