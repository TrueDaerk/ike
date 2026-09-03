package keymap

import "strings"

// Context names a focus scope a binding applies in. The zero value Global
// matches everywhere; the others match only when the focused pane advertises the
// matching context id. The string values intentionally equal the context ids the
// panes advertise (see internal/pane), so the value is shared with the palette
// (Roadmap 0070) and the registry's scope matching. Since #1794 every
// focusable pane kind has a context here, so a chord can be bound per pane —
// the same key doing one thing in a terminal and another in an editor.
//
// The Editor context can additionally carry a language qualifier (#1876):
// "editor[http]" scopes a binding to editors whose buffer is classified as
// that language id, narrower than the plain "editor". The active context fed
// to the resolver carries the focused editor's language the same way, so the
// two sides compare with Split/Matches below.
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
	ES          Context = "es"
	Tests       Context = "tests"
	Issues      Context = "issues"
	DOM         Context = "dom"
	Doctor      Context = "xdoctor"
	Scratch     Context = "scratch"
	// Hex scopes bindings to a focused hex viewer pane (#2420).
	Hex Context = "hex"
	// Notebook scopes bindings to a focused notebook viewer pane (#2425).
	Notebook Context = "notebook"
)

// WithLang narrows c to one buffer language (#1876): WithLang(Editor, "http")
// is the context of bindings that apply only in editors holding an http buffer,
// and of an active focus in such an editor. Only the Editor context carries a
// language scope; any other base — or an empty lang — returns c unchanged.
func WithLang(c Context, lang string) Context {
	if c != Editor || lang == "" {
		return c
	}
	return Context(string(c) + "[" + lang + "]")
}

// Split returns the pane base of c and its language qualifier ("" when
// unqualified): "editor[http]" → (Editor, "http"), "editor" → (Editor, "").
func (c Context) Split() (Context, string) {
	s := string(c)
	if i := strings.IndexByte(s, '['); i >= 0 && strings.HasSuffix(s, "]") {
		return Context(s[:i]), s[i+1 : len(s)-1]
	}
	return c, ""
}

// Base returns c without its language qualifier.
func (c Context) Base() Context { b, _ := c.Split(); return b }

// Lang returns c's language qualifier, "" when unqualified.
func (c Context) Lang() string { _, l := c.Split(); return l }

// Matches reports whether a binding in context c is active for the focused pane
// context active. Global bindings always match; a pane-scoped binding matches
// its own pane context; a language-qualified binding additionally requires the
// active context to carry the same language (#1876).
func (c Context) Matches(active Context) bool {
	if c == Global {
		return true
	}
	cb, cl := c.Split()
	ab, al := active.Split()
	return cb == ab && (cl == "" || cl == al)
}

// MoreSpecific reports whether c is strictly more specific than o for the same
// chord — the more specific binding shadows the less specific one while both
// match the focus. The specificity order has three levels (#1876):
// editor[lang] > pane context > Global. Contexts on the same level are
// mutually disjoint (two pane-scoped bindings on the same chord never compete
// — at most one matches the focused pane; two language-scoped bindings only
// compete when their language differs, and then only one matches).
func (c Context) MoreSpecific(o Context) bool {
	return c.specificity() > o.specificity()
}

// specificity ranks a context on the three-level order.
func (c Context) specificity() int {
	switch {
	case c == Global:
		return 0
	case c.Lang() != "":
		return 2
	default:
		return 1
	}
}

// Shadows reports whether a binding in context c can hide one in context o on
// the same chord (#1875, #1876): c must be strictly more specific and o's
// scope must contain c's — Global contains everything, and a pane context
// contains its language-scoped narrowings. Sibling scopes (editor[http] vs
// editor[go], editor vs diff) never both match a focus, so neither shadows.
func (c Context) Shadows(o Context) bool {
	return c.MoreSpecific(o) && (o == Global || o == c.Base())
}
