package keymap

// Context names a focus scope a binding applies in. The zero value Global
// matches everywhere; the others match only when the focused pane advertises the
// matching context id. The string values intentionally equal the context ids the
// panes advertise (see internal/pane), so the value is shared with the palette
// (Roadmap 0070) and the registry's scope matching. Since #1794 every
// focusable pane kind has a context here, so a chord can be bound per pane —
// the same key doing one thing in a terminal and another in an editor.
type Context string

const (
	// Global applies in every context; it is the least specific and is shadowed
	// by any pane-scoped binding for the same chord.
	Global   Context = ""
	Editor   Context = "editor"
	Explorer Context = "explorer"
	Palette  Context = "palette"
	// Diff scopes bindings to a focused diff viewer pane (0340, #495).
	Diff Context = "diff"
	// Terminal scopes bindings to a focused live terminal — a dedicated
	// terminal pane or an editor pane whose active tab is a terminal (#573).
	// Terminal-scoped bindings resolve before PTY forwarding (#1794); see
	// internal/app's terminalContextChord for the shell-reserved guard rails.
	Terminal Context = "terminal"
	// Preview covers the markdown preview and the image viewer, which both
	// advertise the shared "preview" context id (internal/pane).
	Preview Context = "preview"
	// The tool-window and viewer pane contexts (#1794), one per advertised id.
	VCS         Context = "vcs"
	Debug       Context = "debug"
	Problems    Context = "problems"
	Structure   Context = "structure"
	Usages      Context = "usages"
	HTTP        Context = "http"
	Breakpoints Context = "breakpoints"
	Archive     Context = "archive"
	Data        Context = "data"
)

// Matches reports whether a binding in context c is active for the focused pane
// context active. Global bindings always match; a pane-scoped binding matches
// only its own context.
func (c Context) Matches(active Context) bool {
	return c == Global || c == active
}

// MoreSpecific reports whether c is strictly more specific than o for the same
// chord — a pane-scoped binding shadows a Global one while that pane is focused.
// The specificity order has exactly two levels: Global below every pane
// context, and all pane contexts mutually disjoint (two pane-scoped bindings
// on the same chord never compete — at most one matches the focused pane).
func (c Context) MoreSpecific(o Context) bool {
	return c != Global && o == Global
}
