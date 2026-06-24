package keymap

// Context names a focus scope a binding applies in. The zero value Global
// matches everywhere; the others match only when the focused pane advertises the
// matching context id. The string values intentionally equal the context ids the
// panes advertise (see internal/pane), so the value is shared with the palette
// (Roadmap 0070) and the registry's scope matching.
type Context string

const (
	// Global applies in every context; it is the least specific and is shadowed
	// by any pane-scoped binding for the same chord.
	Global   Context = ""
	Editor   Context = "editor"
	Explorer Context = "explorer"
	Palette  Context = "palette"
)

// Matches reports whether a binding in context c is active for the focused pane
// context active. Global bindings always match; a pane-scoped binding matches
// only its own context.
func (c Context) Matches(active Context) bool {
	return c == Global || c == active
}

// MoreSpecific reports whether c is strictly more specific than o for the same
// chord — a pane-scoped binding shadows a Global one while that pane is focused.
func (c Context) MoreSpecific(o Context) bool {
	return c != Global && o == Global
}
