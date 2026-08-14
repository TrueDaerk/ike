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

// renderRangeDetail describes a folded run of numbered bindings (#1300): the
// individual chords it stands for, and how to open it.
func (k *KeymapPage) renderRangeDetail(b keymapRow, w, h int, clip, title, dim lipgloss.Style) string {
	chord, label := b.rangeLabel()
	lines := []string{
		clip.Render(title.Render(" " + label)),
		clip.Render(dim.Render(" " + chord + " · " + strconv.Itoa(b.rangeCount) + " bindings")),
		clip.Render(dim.Render(" " + strings.Repeat("─", maxInt(w-2, 1)))),
	}
	for _, bb := range k.rangeBindings(b) {
		lines = append(lines, clip.Render(" "+pad(bb.Chord.String(), 14)+bb.Command))
	}
	foot := []string{clip.Render(dim.Render(" z or enter unfolds the run"))}
	for len(lines) < h-len(foot) {
		lines = append(lines, "")
	}
	lines = append(lines, foot...)
	return strings.Join(padTo(lines, h), "\n")
}

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

// shadowInfo classifies the selected row against the table's detected
// cross-context shadows (#1875): the shadows this binding wins (it hides a
// global binding in its pane) and the shadows it loses (a pane-scoped binding
// hides it there).
func (k *KeymapPage) shadowInfo(b keymapRow) (shadowing, shadowedBy []keymap.Shadow) {
	if b.unbound || b.nobind || b.Chord.Len() == 0 {
		return nil, nil
	}
	cs := b.Chord.String()
	for _, s := range k.table().Shadows() {
		if s.Chord != cs {
			continue
		}
		if s.Winner.Command == b.Command && s.Winner.Context == b.Context {
			shadowing = append(shadowing, s)
		}
		if s.Hidden.Command == b.Command && s.Hidden.Context == b.Context {
			shadowedBy = append(shadowedBy, s)
		}
	}
	return shadowing, shadowedBy
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
		lines := []string{clip.Render(title.Render(" Keymap"))}
		lines = append(lines, wrapDetail(w, dim, clip, "Every command and the chord that runs it.")...)
		lines = append(lines, "")
		lines = append(lines, wrapDetail(w, dim, clip, "/ filters · enter rebinds the selection")...)
		return strings.Join(padTo(lines, h), "\n")
	}

	if b.rangeKey != "" {
		return k.renderRangeDetail(b, w, h, clip, title, dim)
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
		// 12 fits the longest context spelling ("breakpoints", #1794) untrimmed.
		row := " " + pad(bb.Chord.String(), 18) + pad(keymap.ContextName(bb.Context), 12) + "@" + bb.Layer.String()
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
		// Both sides of a shared chord are listed with their context (#1312):
		// when the contexts never overlap the chord is not a conflict at all
		// but a deliberate "keep both", and saying so beats a bare warning.
		separated := true
		names := make([]string, 0, len(others))
		for _, o := range others {
			names = append(names, o.Command)
			if !separableContexts(b.Context, o.Context) {
				separated = false
			}
		}
		head, style := " ⚠ "+b.Chord.String()+" also runs "+strings.Join(names, ", "), warn
		if separated {
			head, style = " ↔ "+b.Chord.String()+" is shared, resolved by context", ok
		}
		lines = append(lines, clip.Render(style.Render(head)))
		lines = append(lines, clip.Render(dim.Render("   "+pad(b.Command, 22)+keymap.ContextName(b.Context))))
		for _, o := range others {
			lines = append(lines, clip.Render(dim.Render("   "+pad(o.Command, 22)+keymap.ContextName(o.Context)+" @"+o.Layer.String())))
		}
		// Shadow direction and resolution (#1875): a pane-vs-global overlap is
		// kept but never wordless — say which command wins where and how to
		// untangle it.
		shadowing, shadowedBy := k.shadowInfo(b)
		for _, s := range shadowing {
			lines = append(lines, clip.Render(warn.Render(" ⊘ hides "+s.Hidden.Command+" (global) while "+
				keymap.ContextName(s.Winner.Context)+" is focused")))
			lines = append(lines, clip.Render(dim.Render("   "+s.Hidden.Command+" still wins elsewhere · rebind one, or u to unbind")))
		}
		for _, s := range shadowedBy {
			lines = append(lines, clip.Render(warn.Render(" ⊘ hidden by "+s.Winner.Command+" ("+
				keymap.ContextName(s.Winner.Context)+" @"+s.Winner.Layer.String()+") while that pane is focused")))
			lines = append(lines, clip.Render(dim.Render("   this binding still wins elsewhere · rebind one, or u to unbind")))
		}
		if free := k.suggestChords(2); len(free) > 0 && !separated {
			lines = append(lines, clip.Render(dim.Render(" free: "+strings.Join(free, " · "))))
		}
	} else if !b.unbound && !b.nobind {
		lines = append(lines, clip.Render(ok.Render(" ✓ no conflicts")))
	}
	if b.Fragile {
		lines = append(lines, clip.Render(warn.Render(" ⚠ "+fragileWarning(b.Chord))))
	}

	foot := []string{clip.Render(dim.Render(" enter rebind · u unbind · r reset · z fold runs"))}
	for len(lines) < h-len(foot) {
		lines = append(lines, "")
	}
	lines = append(lines, foot...)
	return strings.Join(padTo(lines, h), "\n")
}
