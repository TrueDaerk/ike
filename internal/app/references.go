package app

import (
	"sort"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"ike/internal/fuzzy"
	ilsp "ike/internal/lsp"
	"ike/internal/palette"
)

// references.go renders find-references results (lsp.references, #5) through
// the command palette: the bridge delivers a ReferencesMsg, the root model
// fills this static mode and opens the palette locked to it. Selecting an
// entry emits the same DefinitionMsg go-to-definition navigates with, so the
// open-file-and-place-cursor path is shared, not forked.

// refsPrefix selects the references mode inside the palette. Like the
// project picker it is only ever opened locked, so the rune has no
// user-facing prefix story.
const refsPrefix = '&'

// previewMax caps the preview chip: the palette row pins Detail to the right
// untruncated, so an over-long source line would crowd out the location.
const previewMax = 32

// refsMode is a palette Mode over the latest find-references results. It
// doubles as the definition-candidates picker (#279) — same rows, same
// activation path — with only the placeholder swapped.
type refsMode struct {
	items       []palette.Item
	placeholder string // "" renders the default usages hint
	count       int    // usages behind the default hint (#860)
}

// SetPlaceholder overrides the input hint for the next open ("Definitions —
// pick a target…"); Set resets it to the usages default.
func (r *refsMode) SetPlaceholder(s string) { r.placeholder = s }

// Set replaces the mode's items with the given references, kept in server
// order (grouped by file, ascending positions for every mainstream server).
// It resets the placeholder to the usages default.
func (r *refsMode) Set(refs []ilsp.Reference) { r.set(refs, false) }

// SetPeek fills the mode as the peek-definition candidate picker (#1154):
// same rows, but activating one peeks the target (PeekDefinitionMsg) instead
// of jumping.
func (r *refsMode) SetPeek(refs []ilsp.Reference) { r.set(refs, true) }

func (r *refsMode) set(refs []ilsp.Reference, peek bool) {
	r.placeholder = ""
	r.count = len(refs)
	r.items = make([]palette.Item, len(refs))
	for i, ref := range refs {
		preview := ref.Preview
		if runes := []rune(preview); len(runes) > previewMax {
			preview = string(runes[:previewMax-1]) + "…"
		}
		var msg tea.Msg = ilsp.DefinitionMsg{Path: ref.Path, Line: ref.Line, Col: ref.Col}
		if peek {
			msg = ilsp.PeekDefinitionMsg{Path: ref.Path, Line: ref.Line, Col: ref.Col}
		}
		r.items[i] = palette.Item{
			Title:  displayPath(ref.Path) + ":" + strconv.Itoa(ref.Line+1),
			Detail: preview,
			Msg:    msg,
		}
	}
}

// Prefix implements palette.Mode.
func (r *refsMode) Prefix() rune { return refsPrefix }

// Placeholder implements palette.Mode.
func (r *refsMode) Placeholder() string {
	if r.placeholder != "" {
		return r.placeholder
	}
	// The count answers "how often is this used" at a glance (#860).
	return strconv.Itoa(r.count) + " usages — filter by file or text…"
}

// Results implements palette.Mode: the stored references fuzzy-matched on
// location and preview (an empty query lists all, in server order).
func (r *refsMode) Results(query string, cx palette.Context) []palette.Item {
	type scored struct {
		item  palette.Item
		score int
	}
	var out []scored
	for _, it := range r.items {
		if m, ok := fuzzy.Match(query, it.Title); ok {
			it.Spans = m.Positions
			out = append(out, scored{item: it, score: m.Score})
			continue
		}
		// Fall back to the preview text; spans index the rendered title, so a
		// preview match highlights nothing.
		if m, ok := fuzzy.Match(query, it.Detail); ok {
			it.Spans = nil
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
