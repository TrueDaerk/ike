// Package help implements the read-only help content: a self-documenting cheat
// sheet that lists every registered Command with its bound shortcut. It is a
// pure consumer — it owns no command or binding store, and no chrome. source.go
// snapshots Commands from the plugin registry (Roadmap 0020) and joins each with
// its shortcut from a BindingResolver (the Roadmap 0080 keymap resolver,
// consumed through a narrow interface so help builds before 08 lands). layout.go
// packs entries into width-responsive columns; help.go is the ui.Content the
// floating shell (Roadmap 0035) hosts, sizes, scrolls, and dismisses.
package help

import (
	"sort"

	"ike/internal/plugin"
	"ike/internal/registry"
)

// BindingResolver maps a command id to its current shortcut string. It is the
// seam onto the Roadmap 0080 keymap resolver; help consumes it read-only and
// never parses keys itself. Commands with no binding return ok=false and render
// title-only (graceful degradation).
type BindingResolver interface {
	// Binding returns the shortcut bound to commandID, or ok=false if unbound.
	Binding(commandID string) (shortcut string, ok bool)
}

// MapResolver is a trivial command-id -> shortcut BindingResolver, used for
// tests and as a stand-in until the 08 resolver is wired in.
type MapResolver map[string]string

// Binding implements BindingResolver.
func (m MapResolver) Binding(id string) (string, bool) {
	s, ok := m[id]
	return s, ok
}

// Entry is one command row in the overlay: its title and (optional) shortcut.
// Lang carries the language badge for file-type-gated commands (#2483): the
// gated family's canonical name (json / yaml / xml), rendered as a marker so
// the row reads as conditional on the current buffer's file type. Empty for
// ungated commands.
type Entry struct {
	ID       string
	Title    string
	Shortcut string // empty when the command has no binding
	Lang     string // language badge for file-type-gated commands (#2483)
}

// Group is a titled cluster of entries sharing a scope label (e.g. "global",
// "editor", "explorer"). Entries are sorted deterministically. Focused marks
// the group that belongs to the currently focused pane's context (#2182) so
// the heading can say so.
type Group struct {
	Label   string
	Entries []Entry
	Focused bool
}

// CommandSource is the read-only registry view help needs: every registered
// command, regardless of focus. Snapshot narrows the set to the focused pane's
// context afterwards. *registry.Registry satisfies it.
type CommandSource interface {
	Commands() []registry.OwnedCommand
}

// Snapshot pulls every registered command from src, joins each with its shortcut
// from res, groups them by scope label, and returns the groups in deterministic
// order. contextID narrows the sheet to what applies to the focused pane —
// global commands plus that context's own; an empty contextID lists every scope
// (the complete reference the flat view shows). langID is the focused buffer's
// language: a non-empty contextID additionally drops file-type-gated commands
// (plugin.Command.Languages, #2483) whose gate does not list langID, and badges
// the matching ones with their family name. The empty-contextID dump keeps the
// gated commands — badged — since it is the complete reference.
// A command with no resolver binding falls back to its documentation-only
// Shortcut hint (plugin.Command.Shortcut); a nil res leaves resolver lookups out
// but the doc hints still apply.
func Snapshot(src CommandSource, res BindingResolver, contextID, langID string) []Group {
	byLabel := map[string][]Entry{}
	for _, c := range src.Commands() {
		label := groupLabel(c.Scope)
		if contextID != "" && label != "global" && label != contextID {
			continue
		}
		if contextID != "" && !c.AppliesToLang(langID) {
			continue
		}
		e := entryFor(c, res)
		byLabel[label] = append(byLabel[label], e)
	}

	groups := make([]Group, 0, len(byLabel))
	for label, entries := range byLabel {
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
		groups = append(groups, Group{Label: label, Entries: entries})
	}
	sort.SliceStable(groups, func(i, j int) bool { return groupOrder(groups[i].Label) < groupOrder(groups[j].Label) })
	return groups
}

// entryFor joins one command with its shortcut and language badge: the live
// resolver binding first, then the command's own documentation hint (vim
// ex-commands and modal keys live outside the keymap layer). A file-type-gated
// command (#2483) carries its family's canonical name — the gate's first
// language id — as the badge.
func entryFor(c registry.OwnedCommand, res BindingResolver) Entry {
	e := Entry{ID: c.ID, Title: c.Title}
	if res != nil {
		if s, ok := res.Binding(c.ID); ok {
			e.Shortcut = s
		}
	}
	if e.Shortcut == "" {
		e.Shortcut = c.Shortcut
	}
	if len(c.Languages) > 0 {
		e.Lang = c.Languages[0]
	}
	return e
}

// groupLabel names the scope a command groups under.
func groupLabel(s plugin.Scope) string {
	if s.Global {
		return "global"
	}
	if s.ContextID != "" {
		return s.ContextID
	}
	return "other"
}

// groupOrder gives a deterministic, human-friendly ordering key for a group
// label: "global" first, then the rest alphabetically. Returning the label
// itself as the tail keeps the sort stable and total.
func groupOrder(label string) string {
	if label == "global" {
		return "\x00" + label // sort before any real label
	}
	return label
}

// ContextSnapshot builds the focused-context view (#2182, reduced by #2483):
// the focused context's own group (flagged Focused so its heading can say so),
// then the global file-type-gated commands matching the focused buffer's
// language, then the hand-curated Global essentials — and nothing else. The
// other contexts are deliberately *gone*, not reordered: the full registry
// stays one tab away in the flat view, and showing every scope here made the
// sheet a scroll-wall nobody read. File-type-gated commands (#2483) are
// narrowed by langID the way Snapshot narrows them. An empty contextID or "global" yields
// the plain full Snapshot — there is no focused context to show; a contextID
// owning no registered commands yields just the curated global section, over
// which a keyboard-owning mode's Focused extra groups (#2237) can still lead.
func ContextSnapshot(src CommandSource, res BindingResolver, contextID, langID string) []Group {
	if contextID == "" || contextID == "global" {
		return Snapshot(src, res, "", langID)
	}
	var out []Group
	for _, g := range Snapshot(src, res, contextID, langID) {
		if g.Label == contextID {
			g.Focused = true
			out = append(out, g)
			break
		}
	}
	// The global file-type-gated commands that apply to this buffer (#2483):
	// the jq playground family over a JSON buffer, the markdown preview over
	// markdown. They are global-scope, so the focused group does not carry
	// them, and they are deliberately not curated — they surface here, badged,
	// only while a matching buffer is focused.
	if ft := fileTypeGroup(src, res, langID); len(ft.Entries) > 0 {
		out = append(out, ft)
	}
	// Stub registries without any curated command leave the global section
	// empty; drop it rather than render a bare heading.
	if global := GlobalEssentials(src, res); len(global.Entries) > 0 {
		out = append(out, global)
	}
	return out
}

// fileTypeGroup collects the global-scope commands gated to the focused
// buffer's language (#2483), in deterministic id order. Context-scoped gated
// commands are not repeated here — they already sit (or are dropped) in their
// own context's group. Empty when the buffer is unclassified.
func fileTypeGroup(src CommandSource, res BindingResolver, langID string) Group {
	g := Group{Label: "This file (" + langID + ")"}
	if langID == "" {
		return g
	}
	for _, c := range src.Commands() {
		if !c.Scope.Global || len(c.Languages) == 0 || !c.AppliesToLang(langID) {
			continue
		}
		g.Entries = append(g.Entries, entryFor(c, res))
	}
	sort.SliceStable(g.Entries, func(i, j int) bool { return g.Entries[i].ID < g.Entries[j].ID })
	return g
}
