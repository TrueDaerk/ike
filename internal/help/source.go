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
type Entry struct {
	ID       string
	Title    string
	Shortcut string // empty when the command has no binding
}

// Group is a titled cluster of entries sharing a scope label (e.g. "global",
// "editor", "explorer"). Entries are sorted deterministically.
type Group struct {
	Label   string
	Entries []Entry
}

// CommandSource is the read-only registry view help needs: every registered
// command, regardless of focus. The cheat sheet is a full reference — it lists
// all scopes (Global, Editor, Explorer, …) at once, not just the focused pane's.
// *registry.Registry satisfies it.
type CommandSource interface {
	Commands() []registry.OwnedCommand
}

// Snapshot pulls every registered command from src, joins each with its shortcut
// from res, groups them by scope label, and returns the groups in deterministic
// order. A command with no resolver binding falls back to its documentation-only
// Shortcut hint (plugin.Command.Shortcut); a nil res leaves resolver lookups out
// but the doc hints still apply.
func Snapshot(src CommandSource, res BindingResolver) []Group {
	byLabel := map[string][]Entry{}
	for _, c := range src.Commands() {
		e := Entry{ID: c.ID, Title: c.Title}
		if res != nil {
			if s, ok := res.Binding(c.ID); ok {
				e.Shortcut = s
			}
		}
		// Fall back to the command's own documentation hint when no live binding
		// resolved (vim ex-commands and modal keys live outside the keymap layer).
		if e.Shortcut == "" {
			e.Shortcut = c.Shortcut
		}
		label := groupLabel(c.Scope)
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
