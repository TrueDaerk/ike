package app

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/diff"
	ilsp "ike/internal/lsp"
	"ike/internal/palette"
	"ike/internal/theme"
)

// actionpreview.go is the intention popup's diff preview (#2252): the row the
// highlight rests on shows, under the list, what running it would change.
// Nothing is applied to get there — an LSP action is resolved through
// codeAction/resolve and rendered from copies of the affected files, a
// built-in intention hands over the text its command would produce through the
// provider seam (intention.Item.Preview). Actions with no resolvable edit —
// commands, pickers, pure side effects — say "no preview" instead of showing
// an empty diff, and apply exactly as before.
//
// Resolution is debounced by the palette (palette.SelectionMode): walking the
// list with a held arrow key resolves only the row the highlight settles on.

// actionPreviewMaxLines caps the rendered diff. The popup is a decision aid
// anchored at the caret, not a diff viewer: a wide-reaching action shows its
// first lines and says how many were left out.
const actionPreviewMaxLines = 8

// noPreviewNote is what a row without a resolvable edit shows.
const noPreviewNote = "no preview"

// SetPalette threads the theme into the popup's footer so the preview's +/-
// lines use the same colors as every other diff in the app.
func (a *actionsMode) SetPalette(pal *theme.Palette) { a.pal = pal }

// SelectionChanged implements palette.SelectionMode (#2252): the highlight has
// settled on a row, so resolve its preview once. A built-in entry's preview is
// a pure local computation and lands immediately; an LSP entry costs a
// resolve round trip, whose reply arrives as an ActionPreviewMsg. A row that
// is already resolved — or already waiting — asks for nothing again.
func (a *actionsMode) SelectionChanged(sel palette.Item, _ palette.Context) tea.Cmd {
	i, ok := a.entryIndex(sel)
	if !ok || !a.previewable {
		return nil
	}
	if _, done := a.previews[i]; done || a.pending[i] {
		return nil
	}
	e := a.entries[i]
	if e.lspIndex < 0 {
		a.setPreview(i, builtinPreview(e))
		return nil
	}
	if a.preview == nil {
		a.setPreview(i, actionPreview{note: noPreviewNote})
		return nil
	}
	if a.pending == nil {
		a.pending = map[int]bool{}
	}
	a.pending[i] = true
	return a.preview(e.lspIndex)
}

// builtinPreview computes a built-in entry's preview through the provider
// seam. An entry without one — every command-style intention — resolves to
// "no preview"; one whose edit turns out to change nothing says so rather
// than showing an empty diff.
func builtinPreview(e actionEntry) actionPreview {
	if e.preview == nil {
		return actionPreview{note: noPreviewNote}
	}
	edit, ok := e.preview()
	if !ok {
		return actionPreview{note: noPreviewNote}
	}
	res := diff.Compute(edit.Before, edit.After)
	if len(res.Hunks) == 0 {
		return actionPreview{note: "changes nothing here"}
	}
	return actionPreview{res: res}
}

// SetActionPreview records a bridge preview reply (#2252). It is matched by
// path and offer index, so a reply for an offer the popup has since replaced
// is dropped instead of shown against whatever row now sits at that index.
func (a *actionsMode) SetActionPreview(msg ilsp.ActionPreviewMsg) {
	if msg.Path != a.path {
		return
	}
	i, ok := a.entryForLSP(msg.Index)
	if !ok {
		return
	}
	delete(a.pending, i)
	if len(msg.Files) == 0 {
		note := msg.Note
		if note == "" {
			note = noPreviewNote
		}
		a.setPreview(i, actionPreview{note: note})
		return
	}
	// The popup previews the buffer the caret sits in; an action reaching
	// further shows that file's diff and names the others it would touch.
	f := msg.Files[0]
	for _, cand := range msg.Files {
		if cand.Path == a.path {
			f = cand
			break
		}
	}
	p := actionPreview{res: diff.Compute(f.Before, f.After)}
	if n := len(msg.Files) - 1; n > 0 {
		p.note = "and " + renameFileCount(n) + " more"
	}
	a.setPreview(i, p)
}

// setPreview stores one row's resolved preview.
func (a *actionsMode) setPreview(i int, p actionPreview) {
	if a.previews == nil {
		a.previews = map[int]actionPreview{}
	}
	a.previews[i] = p
}

// entryIndex resolves a palette row back to its entry index.
func (a *actionsMode) entryIndex(sel palette.Item) (int, bool) {
	msg, ok := sel.Msg.(actionPickedMsg)
	if !ok || msg.index < 0 || msg.index >= len(a.entries) {
		return 0, false
	}
	return msg.index, true
}

// entryForLSP maps an offer index back to the merged row holding it — the
// two differ, since preferred actions sort first and the built-ins follow.
func (a *actionsMode) entryForLSP(lspIndex int) (int, bool) {
	for i, e := range a.entries {
		if e.lspIndex == lspIndex {
			return i, true
		}
	}
	return 0, false
}

// Footer implements palette.FooterMode: the highlighted row's preview under
// the list — its diff, the note saying there is none, or the dim "resolving"
// line while the round trip is out.
func (a *actionsMode) Footer(sel palette.Item, width int) []string {
	i, ok := a.entryIndex(sel)
	if !ok || width <= 0 || !a.previewable {
		return nil
	}
	pal := a.pal
	if pal == nil {
		pal = theme.DefaultPalette()
	}
	dim := lipgloss.NewStyle().Foreground(pal.Hint)
	p, done := a.previews[i]
	if !done {
		return []string{dim.Render(ansi.Truncate("resolving preview…", width, "…"))}
	}
	if len(p.res.Hunks) == 0 {
		return []string{dim.Render(ansi.Truncate(p.note, width, "…"))}
	}
	lines := miniDiffLines(pal, p.res, width)
	if len(lines) > actionPreviewMaxLines {
		rest := len(lines) - actionPreviewMaxLines
		lines = append(lines[:actionPreviewMaxLines:actionPreviewMaxLines],
			dim.Render(ansi.Truncate("… "+plural(rest, "more diff line", "more diff lines"), width, "…")))
	}
	if p.note != "" {
		lines = append(lines, dim.Render(ansi.Truncate(p.note, width, "…")))
	}
	return lines
}
