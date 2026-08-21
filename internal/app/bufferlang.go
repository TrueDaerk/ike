package app

import (
	"sort"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/fuzzy"
	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/palette"
)

// bufferlang.go is the user-facing half of "Treat Buffer as …" (#2033): the
// language picker for a buffer that has no file, and the handler that installs
// the pick.
//
// A file-less buffer — a fresh tab, a split, the target of a paste — used to be
// typeless: language resolution is path-keyed all the way down, so nothing
// highlighted, nothing concealed, no markdown or CSV rendering, no
// type-specific intentions. The editor's buffer-level override
// (editor.SetLangOverride, #2033) closes that gap; this file is the door to it:
// alt+enter offers the intention, the picker lists the registered languages,
// and the chosen row calls the override. Nothing is written to disk — the type
// lives as long as the buffer does, and a later save under a real name hands
// the classification back to the path.

// bufferLangPrefix selects the buffer-language picker inside the palette. The
// root model only ever opens it locked, so the rune carries no user-facing
// prefix story — it merely has to be unique among the modes.
const bufferLangPrefix = '['

// ShowBufferLangMsg opens the buffer-language picker (editor.setBufferLanguage).
type ShowBufferLangMsg struct{}

// SetBufferLangMsg installs one language on the focused file-less buffer. The
// empty ID is the "Plain Text" row: it drops the override again.
type SetBufferLangMsg struct{ ID string }

// bufferLangMode is the palette Mode listing the languages a buffer can be
// treated as: "Plain Text" (the no-override row) first, then every registered
// language that a path lookup could resolve — one with an extension or an
// exact base name. The registry is read on every Results call (lazy, like
// scratchNewMode), so late-registered languages appear without ordering
// constraints.
type bufferLangMode struct{}

// Prefix implements palette.Mode.
func (bufferLangMode) Prefix() rune { return bufferLangPrefix }

// Placeholder implements palette.Mode.
func (bufferLangMode) Placeholder() string { return "Treat buffer as: language…" }

// Results implements palette.Mode: rows fuzzy-matched on the language title,
// "Plain Text" pinned to the top of the unfiltered list, the detail column
// showing the extension the buffer is treated as. Activation dispatches
// SetBufferLangMsg, so the picker owns no language logic of its own.
func (bufferLangMode) Results(query string, _ palette.Context) []palette.Item {
	type row struct {
		title, detail, id string
		// rank breaks ties for equal fuzzy scores: plain text first, then
		// alphabetically by title.
		rank int
	}
	rows := []row{{title: "Plain Text", detail: "no language", id: "", rank: 0}}
	for _, l := range lang.All() {
		detail := ""
		switch {
		case len(l.Extensions) > 0:
			detail = "." + l.Extensions[0]
		case len(l.Filenames) > 0:
			detail = l.Filenames[0]
		default:
			// Nothing a path lookup could match: the row would set a
			// language that changes nothing.
			continue
		}
		rows = append(rows, row{title: langTitle(l.ID), detail: detail, id: l.ID, rank: 1})
	}
	type scored struct {
		item  palette.Item
		score int
		rank  int
		title string
	}
	var out []scored
	for _, r := range rows {
		match, ok := fuzzy.Match(query, r.title)
		if !ok {
			continue
		}
		out = append(out, scored{
			item: palette.Item{
				Title:  r.title,
				Detail: r.detail,
				Spans:  match.Positions,
				Score:  match.Score,
				Msg:    SetBufferLangMsg{ID: r.id},
			},
			score: match.Score,
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

// openBufferLangPicker opens the picker locked to its mode. A buffer that has
// a file is refused with the reason: its path already classifies it, and
// silently opening a picker whose pick could not apply would be worse than the
// notice (#2033 — the override is deliberately file-less only).
func (m *Model) openBufferLangPicker() {
	ed := m.activeEditor()
	if ed == nil {
		m.host.Notify(host.Info, "buffer language: focus an editor first")
		return
	}
	if ed.HasFile() {
		m.host.Notify(host.Info, "buffer language: "+baseName(ed.Path())+" is classified by its file name")
		return
	}
	m.palette.SetSize(m.width, m.height)
	m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, bufferLangPrefix)
}

// setBufferLang installs the picked language on the focused file-less buffer
// and reports what the buffer is now — the pick has no other visible echo than
// the status-line segment and whatever the type turns on.
func (m *Model) setBufferLang(msg SetBufferLangMsg) tea.Cmd {
	ed := m.activeEditor()
	if ed == nil {
		return nil
	}
	cmd, ok := ed.SetLangOverride(msg.ID)
	if !ok {
		m.host.Notify(host.Info, "buffer language: this buffer is classified by its file name")
		return nil
	}
	if msg.ID == "" {
		m.host.Notify(host.Info, "buffer language: plain text")
		return cmd
	}
	m.host.Notify(host.Info, "buffer language: treating this buffer as "+langTitle(msg.ID))
	return cmd
}

// bufferLangSegment is the status-line marker of a chosen buffer language
// (#2033): "as Markdown", shown only while an override applies. Clicking it
// reopens the picker (statusSegmentCommands), so the type is changeable from
// where it is read.
func bufferLangSegment(_ Model, ed *editor.Model) string {
	if ed == nil {
		return ""
	}
	id := ed.LangOverrideTitle()
	if id == "" {
		return ""
	}
	return "as " + langTitle(id)
}
