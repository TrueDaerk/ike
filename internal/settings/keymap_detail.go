package settings

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"ike/internal/keymap"
)

// keymap_detail.go is the keymap page's detail column (0460, #1298). Browsing
// without editing used to say almost nothing; on the grid the third column
// explains the selected command — every chord bound to it, with context and
// provenance, whether anything collides with it, and what free chords are
// available if it does.

// bindingsFor returns every effective binding of a command, in table order.
func (k *KeymapPage) bindingsFor(command string) []keymap.Binding {
	var out []keymap.Binding
	for _, b := range k.table().Bindings() {
		if b.Command == command {
			out = append(out, b)
		}
	}
	return out
}

// collisions lists the *other* commands sharing a chord with b — the honest
// answer to "is this binding actually going to fire".
func (k *KeymapPage) collisions(b keymapRow) []keymap.Binding {
	if b.unbound || b.nobind || b.Chord.Len() == 0 {
		return nil
	}
	cs := b.Chord.String()
	var out []keymap.Binding
	for _, other := range k.table().Bindings() {
		if other.Command == b.Command || other.Chord.String() != cs {
			continue
		}
		out = append(out, other)
	}
	return out
}

// suggestChords proposes free chords near the given one: same modifiers, a
// different final key. Only chords nothing else claims are offered, so a
// suggestion is always safe to take.
func (k *KeymapPage) suggestChords(want int) []string {
	taken := map[string]bool{}
	for _, b := range k.table().Bindings() {
		taken[b.Chord.String()] = true
	}
	var out []string
	for _, mod := range []string{"cmd+alt+", "cmd+shift+", "ctrl+alt+", "ctrl+shift+"} {
		for _, letter := range "abcdefghijklmnopqrstuvwxyz" {
			c := mod + string(letter)
			if taken[c] {
				continue
			}
			out = append(out, c)
			if len(out) == want {
				return out
			}
		}
	}
	return out
}

// renderDetail renders the detail column for the selected command: what it is,
// every chord bound to it with context and layer, its conflict state, and the
// keys that act on it.
func (k *KeymapPage) renderDetail(w, h int) string {
	pal := k.theme()
	clip := lipgloss.NewStyle().MaxWidth(w)
	title := lipgloss.NewStyle().Foreground(pal.BorderFocus).Bold(true)
	dim := lipgloss.NewStyle().Foreground(pal.Secondary)
	warn := lipgloss.NewStyle().Foreground(pal.Error)
	ok := lipgloss.NewStyle().Foreground(pal.Success)

	b, has := k.current()
	if !has {
		return strings.Join(padTo([]string{
			clip.Render(title.Render(" Keymap")),
			clip.Render(dim.Render(" Every command and the chord that runs it.")),
			"",
			clip.Render(dim.Render(" / filters · enter rebinds the selection")),
		}, h), "\n")
	}

	lines := []string{clip.Render(title.Render(" " + b.Title))}
	lines = append(lines, clip.Render(dim.Render(" "+b.Command+" · chord")))
	if reason, blocked := keymap.BlockedReason(b.Command); blocked {
		lines = append(lines, clip.Render(warn.Render(" ✗ "+reason)))
	} else if k.registered != nil && !k.registered(b.Command) {
		lines = append(lines, clip.Render(warn.Render(" ✗ not registered")))
	}
	lines = append(lines, clip.Render(dim.Render(" "+strings.Repeat("─", maxInt(w-2, 1)))))

	bound := k.bindingsFor(b.Command)
	lines = append(lines, clip.Render(dim.Render(" bindings · "+strconv.Itoa(len(bound)))))
	for _, bb := range bound {
		row := " " + pad(bb.Chord.String(), 18) + pad(string(bb.Context), 9) + "@" + bb.Layer.String()
		style := lipgloss.NewStyle().Foreground(pal.Foreground)
		if bb.Layer != keymap.LayerDefault {
			style = lipgloss.NewStyle().Foreground(pal.Info)
		}
		lines = append(lines, clip.Render(style.Render(row)))
	}
	if len(bound) == 0 {
		lines = append(lines, clip.Render(dim.Render("   (none — enter sets the first one)")))
	}

	if others := k.collisions(b); len(others) > 0 {
		names := make([]string, 0, len(others))
		for _, o := range others {
			names = append(names, o.Command)
		}
		lines = append(lines, clip.Render(warn.Render(" ⚠ "+b.Chord.String()+" also runs "+strings.Join(names, ", "))))
		if free := k.suggestChords(2); len(free) > 0 {
			lines = append(lines, clip.Render(dim.Render(" free: "+strings.Join(free, " · "))))
		}
	} else if !b.unbound && !b.nobind {
		lines = append(lines, clip.Render(ok.Render(" ✓ no conflicts")))
	}
	if b.Fragile {
		lines = append(lines, clip.Render(warn.Render(" ⚠ "+fragileWarning(b.Chord))))
	}

	foot := []string{clip.Render(dim.Render(" enter rebind · u unbind · r reset · i import"))}
	for len(lines) < h-len(foot) {
		lines = append(lines, "")
	}
	lines = append(lines, foot...)
	return strings.Join(padTo(lines, h), "\n")
}
