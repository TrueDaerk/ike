// Package tour implements the Welcome Tour (#657): a passive, paged
// first-orientation walkthrough rendered inside the reusable floating shell.
// The tour executes nothing — it is a ui.Content provider like help, but its
// paging keys are handled host-level (internal/app), never by the shell's
// scroller, so each page must fit the shell without vertical overflow.
//
// Shortcuts render through a BindingResolver (the same seam help uses) so a
// user remap displays truthfully; multi-bound or fragile commands carry a
// curated preferred-order default ("shift shift · cmd+shift+a") that is never
// replaced by the resolver when the resolved chord is already part of it —
// the tour must never teach a possibly-dead chord alone.
package tour

import (
	"strconv"
	"strings"
)

// BindingResolver maps a command id to its current shortcut string; it is the
// narrow seam onto the keymap resolver, identical to help's.
type BindingResolver interface {
	Binding(commandID string) (shortcut string, ok bool)
}

// Tour is the paged welcome content. It implements ui.Content (Title/Render);
// paging is driven by the host via Next/Prev.
type Tour struct {
	res  BindingResolver
	page int
}

// New returns a tour on its first page. res may be nil; every shortcut then
// falls back to its curated default text.
func New(res BindingResolver) *Tour { return &Tour{res: res} }

// PageCount is the number of tour pages.
func (t *Tour) PageCount() int { return len(pages) }

// Page returns the current zero-based page index.
func (t *Tour) Page() int { return t.page }

// Next advances one page, clamping at the last; it reports whether the page
// changed (false on the last page — the host closes the tour instead).
func (t *Tour) Next() bool {
	if t.page >= len(pages)-1 {
		return false
	}
	t.page++
	return true
}

// Prev steps one page back, clamping at the first.
func (t *Tour) Prev() bool {
	if t.page <= 0 {
		return false
	}
	t.page--
	return true
}

// Title implements ui.Content; it carries the page indicator.
func (t *Tour) Title() string {
	return "WELCOME TO IKE — " + strconv.Itoa(t.page+1) + "/" + strconv.Itoa(len(pages))
}

// Render implements ui.Content: the current page plus the paging legend. The
// pages are prose + short key lists (no wide diagrams), each within ~72×16,
// so the body never overflows the shell at 80×24 and space stays unambiguous.
func (t *Tour) Render(int) string {
	var b strings.Builder
	b.WriteString(pages[t.page](t))
	b.WriteString("\n")
	legend := "[→/space] next   [←] back   [esc] close"
	if t.page == len(pages)-1 {
		legend = "[enter/space] finish   [←] back   [esc] close"
	}
	b.WriteString(legend + "\nreopen anytime: \"Welcome Tour\" in the palette")
	return b.String()
}

// chord resolves the display shortcut for a command: the curated default
// (which may list several chords in preferred order) unless the resolver
// reports a binding that is neither in that list nor among the command's
// other known defaults (leader mnemonics, delivered secondaries) — i.e. a
// real user remap (#665). Without the known set, the resolver returning the
// space-space leader default would masquerade as a remap and replace the
// curated display.
func (t *Tour) chord(id, curated string, known ...string) string {
	if t.res == nil {
		return curated
	}
	s, ok := t.res.Binding(id)
	if !ok || s == "" || strings.Contains(curated, s) {
		return curated
	}
	for _, k := range known {
		if s == k {
			return curated
		}
	}
	return s
}

// key renders one "title   chord" row with the chord column aligned.
func key(title, chord string) string {
	const col = 26
	gap := col - len([]rune(title))
	if gap < 2 {
		gap = 2
	}
	return "  " + title + strings.Repeat(" ", gap) + chord + "\n"
}

// pages are the tour's page renderers, in order.
var pages = []func(*Tour) string{
	pageWelcome,
	pageEditor,
	pageLayout,
	pageTools,
	pageCustomize,
}

func pageWelcome(t *Tour) string {
	var b strings.Builder
	b.WriteString("IKE is a terminal IDE: JetBrains-style keybindings around a vim\n")
	b.WriteString("modal editor.\n\n")
	b.WriteString("The keys that open everything:\n\n")
	b.WriteString(key("Search everywhere", t.chord("palette.searchEverywhere", "shift shift · cmd+shift+a", "space space", "space A")))
	b.WriteString(key("Help cheat sheet", "? · f1"))
	b.WriteString(key("Switch project", t.chord("project.switch", "cmd+shift+p", "ctrl+shift+p", "space p")))
	b.WriteString("\nTo quit IKE: press q (in the file tree, or in an editor while not\n")
	b.WriteString("typing) or ctrl+c — unsaved changes always prompt first.\n")
	return b.String()
}

func pageEditor(t *Tour) string {
	var b strings.Builder
	b.WriteString("The editor is modal, like vim.\n\n")
	b.WriteString("If you type and nothing appears: you are in NORMAL mode. Press i\n")
	b.WriteString("to insert text; esc returns to normal mode. The current mode is\n")
	b.WriteString("always shown at the left of the status bar.\n\n")
	b.WriteString(key("Save", t.chord("editor.write", "cmd+s · :w", "ctrl+s", "space w")))
	b.WriteString(key("Undo", t.chord("editor.undo", "ctrl+z · u")))
	b.WriteString(key("Find in file", t.chord("editor.find", "cmd+f · /")))
	b.WriteString(key("Comment line", t.chord("editor.commentLine", "cmd+7", "cmd+k cmd+c", "space c")))
	return b.String()
}

func pageLayout(t *Tour) string {
	var b strings.Builder
	b.WriteString("Everything lives in panes: the file tree, editors with tabs, and\n")
	b.WriteString("tool windows. Any pane can be split, moved, and resized.\n\n")
	b.WriteString(key("Toggle file tree", t.chord("explorer.toggle", "cmd+1", "space e")))
	b.WriteString(key("Switch pane focus", t.chord("pane.switcher", "ctrl+tab · ctrl+arrows")))
	b.WriteString(key("Go to file", t.chord("project.goToFile", "cmd+shift+o", "space f")))
	b.WriteString(key("Recent files", t.chord("palette.recentFiles", "cmd+e", "space m")))
	b.WriteString(key("Split right", t.chord("pane.splitRight", "cmd+k right")))
	b.WriteString(key("Maximize pane", t.chord("pane.maximize", "cmd+k z")))
	return b.String()
}

func pageTools(t *Tour) string {
	var b strings.Builder
	b.WriteString("The tool windows, all also reachable from the palette:\n\n")
	b.WriteString(key("Terminal", t.chord("terminal.toggle", "alt+f12", "space t")))
	b.WriteString(key("Run file", t.chord("run.file", "shift+f10")))
	b.WriteString(key("Debug file", t.chord("debug.start", "shift+f9")))
	b.WriteString(key("Git tool window", t.chord("vcs.panel", "space v v")))
	b.WriteString(key("Find in path", t.chord("project.findInPath", "cmd+shift+f", "space g")))
	b.WriteString("\nInside a focused terminal every key goes to the shell. To get\n")
	b.WriteString("out, toggle it again (" + t.chord("terminal.toggle", "alt+f12", "space t") + ") or move focus with\n")
	b.WriteString("ctrl+arrows.\n")
	return b.String()
}

func pageCustomize(t *Tour) string {
	var b strings.Builder
	b.WriteString("Make it yours:\n\n")
	b.WriteString(key("Settings", t.chord("settings.open", "cmd+,", "space ,")))
	b.WriteString(key("Menu bar", t.chord("menu.open", "f10")))
	b.WriteString("\nThemes, keybindings, and plugins live in Settings and in\n")
	b.WriteString("~/.ike/settings.toml; the palette finds every action by name.\n")
	b.WriteString("The help sheet (?) opens on the essentials — tab shows all.\n\n")
	b.WriteString("Next: pick language servers to install.\n")
	return b.String()
}
