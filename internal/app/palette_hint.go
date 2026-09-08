package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
)

// palette_hint.go is the learnable-shortcuts hint behind a palette pick
// (#2549). The local usage log showed nearly every command arriving by
// keybind and a handful — lsp.doctor, diff.files, scratch.new.http — coming
// from the palette repeatedly although a chord existed or could have. After a
// pick of a command bound in the focused context a short toast names the
// chord ("also: cmd+shift+a"); after the third pick in a session of a command
// with no binding at all, a toast offers palette.bindLastPick, which opens the
// settings keymap page on that command. Both are gated by
// palette.hint_keybind (default on).

// unboundPickOfferAt is the per-session pick count of one unbound command at
// which the bind-a-key offer is raised — once, on exactly that pick, so the
// toast never nags on every later use.
const unboundPickOfferAt = 3

// bindLastPickCommand is the command the offer names.
const bindLastPickCommand = "palette.bindLastPick"

// paletteHintKeybind reads palette.hint_keybind (default on).
func paletteHintKeybind(cfg host.Config) bool {
	if cfg == nil {
		return true
	}
	if v, ok := cfg.Get("palette.hint_keybind"); ok {
		return v != "false"
	}
	return true
}

// paletteKeybindHint records id as the last palette pick and raises the
// matching toast: the command's chord in the focused context when it has
// one, the bind-a-key offer on the third pick of a command bound nowhere.
// A command bound only in another context gets neither — it is not usable
// from here, and it is not unbound either.
func (m *Model) paletteKeybindHint(id string) {
	// The bind command itself is never the pick to bind: running it from
	// the palette must still target the command picked before it.
	if id != bindLastPickCommand {
		m.lastPalettePick = id
	}
	if !paletteHintKeybind(m.host.Config()) || m.bindings == nil {
		return
	}
	if chord, ok := m.bindings.BindingIn(id, m.keyContext()); ok {
		m.host.Notify(host.Info, "also: "+chord)
		return
	}
	if _, bound := m.bindings.Binding(id); bound {
		return
	}
	if m.unboundPicks == nil {
		m.unboundPicks = map[string]int{}
	}
	m.unboundPicks[id]++
	if m.unboundPicks[id] != unboundPickOfferAt {
		return
	}
	title := id
	if c, ok := m.reg.Command(id); ok && c.Title != "" {
		title = c.Title
	}
	text := fmt.Sprintf("%s: picked %d× from the palette without a key — bind one", title, unboundPickOfferAt)
	if chord, ok := m.bindings.BindingIn(bindLastPickCommand, m.keyContext()); ok {
		text += ": " + chord
	}
	m.host.Notify(host.Info, text)
}

// openBindLastPick opens the settings keymap page narrowed to the last
// palette pick (#2549). Without one — the command ran before any palette
// pick this session — it says so instead of opening an empty list.
func (m *Model) openBindLastPick() tea.Cmd {
	if m.lastPalettePick == "" {
		m.host.Notify(host.Info, "bind a key: no command run from the palette yet")
		return nil
	}
	w, h := m.settingsSize()
	m.settings.SetSize(w, h)
	if !m.settings.OpenKeymapOn(m.lastPalettePick) {
		m.host.Notify(host.Warn, "bind a key: no keymap settings page")
	}
	return nil
}
