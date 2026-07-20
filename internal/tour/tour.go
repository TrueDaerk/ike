// Package tour implements the Welcome Tour (#657): a paged first-orientation
// walkthrough rendered inside the reusable floating shell. It is a ui.Content
// provider like help, but its paging keys are handled host-level
// (internal/app), never by the shell's scroller, so each page must fit the
// shell without vertical overflow. The tour itself executes nothing; since
// #680 selected pages carry "try it" tasks — the host lets the taught keys
// through to the app and reports executions back via NoteExecuted, which
// ticks the matching checkbox. Pages stay skippable regardless of task state.
//
// Shortcuts resolve through a BindingResolver (the same seam help uses)
// FIRST, so a user remap displays truthfully and the shown chord is always
// the live keymap's preferred one (custom > default). Curated preferred-order
// defaults ("shift shift · cmd+shift+a") are kept whenever the resolved chord
// is one of their options — the tour must never teach a possibly-dead chord
// alone — and serve as the fallback when a command is unbound. All curated
// chord text is platform-normalized for display (Meta→Ctrl off macOS, #678),
// never rendered from hardcoded mac strings.
package tour

import (
	"strconv"
	"strings"

	"ike/internal/keymap"
)

// BindingResolver maps a command id to its current shortcut string; it is the
// narrow seam onto the keymap resolver, identical to help's.
type BindingResolver interface {
	Binding(commandID string) (shortcut string, ok bool)
}

// TryTask is one interactive "try it" exercise on a tour page (#680): the
// command whose execution ticks it, its prompt line, and the curated default
// chord display (resolver-first like every other row, #678).
type TryTask struct {
	CommandID string
	Prompt    string
	Curated   string
	Known     []string
}

// Tour is the paged welcome content. It implements ui.Content (Title/Render);
// paging is driven by the host via Next/Prev.
type Tour struct {
	res  BindingResolver
	page int
	done map[string]bool // command id → try-it task completed (#680)
}

// New returns a tour on its first page. res may be nil; every shortcut then
// falls back to its curated default text.
func New(res BindingResolver) *Tour {
	return &Tour{res: res, done: map[string]bool{}}
}

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
	b.WriteString(pages[t.page].render(t))
	b.WriteString(t.renderTasks())
	b.WriteString("\n")
	legend := "[→/space] next   [←] back   [esc] close"
	if t.page == len(pages)-1 {
		legend = "[enter/space] finish   [←] back   [esc] close"
	}
	b.WriteString(legend + "\nreopen anytime: \"Welcome Tour\" in the palette")
	return b.String()
}

// optionSep separates the options of a curated preferred-order display list.
const optionSep = " · "

// normalizeOption canonicalises one curated display option for the current
// platform: an option that parses as a keymap chord is normalized (Meta→Ctrl
// off macOS) and canonically formatted; non-chord hints (vim keys like ":w",
// prose like "ctrl+arrows") pass through verbatim.
func normalizeOption(opt string) string {
	c, err := keymap.ParseChord(opt)
	if err != nil {
		return opt
	}
	return keymap.NormalizeChord(c, keymap.GOOS).String()
}

// normalizeCurated platform-normalizes every option of a curated list.
func normalizeCurated(curated string) string {
	opts := strings.Split(curated, optionSep)
	for i, o := range opts {
		opts[i] = normalizeOption(o)
	}
	return strings.Join(opts, optionSep)
}

// vimHints are curated options handled outside the keymap layer — vim
// ex-commands and modal keys, and the hardcoded "?" help key. They stay
// valid regardless of any remap, so a remapped display keeps them; every
// other curated option is a keymap chord the remap replaced. (They cannot
// be detected by parse failure: any bare token parses as a key base.)
var vimHints = map[string]bool{":w": true, "u": true, "/": true, "?": true}

// chord resolves the display shortcut for a command, resolver-first (#678):
// the live keymap value (custom > default, read from the platform-normalized
// effective table) decides what is shown. When the resolved chord is one of
// the curated options — or one of the command's other known defaults
// (delivered secondaries, #665), which would otherwise masquerade
// as remaps — the full curated preferred-order list is kept, platform-
// normalized. A resolved chord outside all known defaults is a real user
// remap and leads the display, with only the curated non-chord hints (vim
// keys, which remain valid regardless of the keymap) kept as secondary
// options. The curated list alone is the fallback when the command is
// unbound or no resolver is wired.
func (t *Tour) chord(id, curated string, known ...string) string {
	norm := normalizeCurated(curated)
	if t.res == nil {
		return norm
	}
	s, ok := t.res.Binding(id)
	if !ok || s == "" {
		return norm
	}
	opts := strings.Split(norm, optionSep)
	for _, o := range opts {
		if o == s {
			return norm
		}
	}
	for _, k := range known {
		if normalizeOption(k) == s {
			return norm
		}
	}
	out := s
	for _, o := range opts {
		if vimHints[o] {
			out += optionSep + o
		}
	}
	return out
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

// renderTasks renders the current page's try-it block (#680): one checkbox
// row per task with its resolver-first chord, plus the continue hint once
// everything is ticked. Empty for passive pages.
func (t *Tour) renderTasks() string {
	tasks := pages[t.page].tasks
	if len(tasks) == 0 {
		return ""
	}
	allDone := true
	var rows strings.Builder
	for _, task := range tasks {
		box := "[ ]"
		if t.done[task.CommandID] {
			box = "[x]"
		} else {
			allDone = false
		}
		rows.WriteString(key(box+" "+task.Prompt, t.chord(task.CommandID, task.Curated, task.Known...)))
	}
	// The header becomes the completion hint in place — the row count must
	// stay constant so a ticked page never overflows the shell budget.
	header := "Try it now (the key reaches the app while this page is up):"
	if allDone {
		header = "✓ all done — press → to continue"
	}
	return header + "\n" + rows.String()
}

// Tasks returns the current page's try-it tasks (empty for passive pages).
func (t *Tour) Tasks() []TryTask { return pages[t.page].tasks }

// TaskDone reports whether the try-it task for commandID is completed.
func (t *Tour) TaskDone(commandID string) bool { return t.done[commandID] }

// HasPendingTasks reports whether the current page still has an unfinished
// try-it task — while it does, the host lets non-paging keys through to the
// app instead of swallowing them (#680).
func (t *Tour) HasPendingTasks() bool {
	for _, task := range pages[t.page].tasks {
		if !t.done[task.CommandID] {
			return true
		}
	}
	return false
}

// NoteExecuted marks the try-it task for an executed command as done —
// on any page, so trying ahead counts — and reports whether a task was
// newly ticked.
func (t *Tour) NoteExecuted(commandID string) bool {
	if t.done[commandID] {
		return false
	}
	for _, p := range pages {
		for _, task := range p.tasks {
			if task.CommandID == commandID {
				t.done[commandID] = true
				return true
			}
		}
	}
	return false
}

// tourPage is one tour page: its renderer plus optional try-it tasks.
type tourPage struct {
	render func(*Tour) string
	tasks  []TryTask
}

// pages are the tour's pages, in order. Try-it tasks are chosen so the taught
// chord acts visibly around the floating shell (pane toggles) or in an
// overlay that naturally covers the tour and returns to it (the palette);
// their chords must not collide with the tour's paging keys.
var pages = []tourPage{
	{render: pageWelcome, tasks: []TryTask{{
		CommandID: "palette.searchEverywhere",
		Prompt:    "Open search everywhere (esc returns)",
		Curated:   "shift shift · cmd+shift+a",
	}}},
	{render: pageEditor},
	{render: pageLayout, tasks: []TryTask{{
		CommandID: "explorer.toggle",
		Prompt:    "Toggle the file tree",
		Curated:   "cmd+1",
	}}},
	{render: pageTools, tasks: []TryTask{{
		CommandID: "terminal.toggle",
		Prompt:    "Toggle the terminal",
		Curated:   "alt+f12",
	}}},
	{render: pageCustomize},
}

func pageWelcome(t *Tour) string {
	var b strings.Builder
	b.WriteString("IKE is a terminal IDE: JetBrains-style keybindings around a vim\n")
	b.WriteString("modal editor.\n\n")
	b.WriteString("The keys that open everything:\n\n")
	b.WriteString(key("Search everywhere", t.chord("palette.searchEverywhere", "shift shift · cmd+shift+a")))
	b.WriteString(key("Help cheat sheet", t.chord("palette.keymapHelp", "? · f1")))
	b.WriteString(key("Switch project", t.chord("project.switch", "cmd+shift+p", "ctrl+shift+p")))
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
	b.WriteString(key("Save", t.chord("editor.write", "cmd+s · :w", "ctrl+s")))
	b.WriteString(key("Undo", t.chord("editor.undo", "ctrl+z · u")))
	b.WriteString(key("Find in file", t.chord("editor.find", "cmd+f · /")))
	b.WriteString(key("Comment line", t.chord("editor.commentLine", "cmd+7")))
	return b.String()
}

func pageLayout(t *Tour) string {
	var b strings.Builder
	b.WriteString("Everything lives in panes: the file tree, editors with tabs, and\n")
	b.WriteString("tool windows. Any pane can be split, moved, and resized.\n\n")
	b.WriteString(key("Toggle file tree", t.chord("explorer.toggle", "cmd+1")))
	b.WriteString(key("Switch pane focus", t.chord("pane.switcher", "ctrl+tab · ctrl+arrows")))
	b.WriteString(key("Go to file", t.chord("project.goToFile", "cmd+shift+o")))
	b.WriteString(key("Recent files", t.chord("palette.recentFiles", "cmd+e")))
	b.WriteString(key("Split right", t.chord("pane.splitRight", "cmd+k right")))
	b.WriteString(key("Maximize pane", t.chord("pane.maximize", "cmd+k z")))
	return b.String()
}

func pageTools(t *Tour) string {
	var b strings.Builder
	b.WriteString("The tool windows, all also reachable from the palette:\n\n")
	b.WriteString(key("Terminal", t.chord("terminal.toggle", "alt+f12")))
	b.WriteString(key("Run file", t.chord("run.file", "shift+f10")))
	b.WriteString(key("Debug file", t.chord("debug.start", "shift+f9")))
	b.WriteString(key("Git tool window", t.chord("vcs.panel", "cmd+9")))
	b.WriteString(key("Find in path", t.chord("project.findInPath", "cmd+shift+f")))
	b.WriteString("\nInside a focused terminal every key goes to the shell. To get\n")
	b.WriteString("out, toggle it again (" + t.chord("terminal.toggle", "alt+f12") + ") or move focus with\n")
	b.WriteString("ctrl+arrows.\n")
	return b.String()
}

func pageCustomize(t *Tour) string {
	var b strings.Builder
	b.WriteString("Make it yours:\n\n")
	b.WriteString(key("Settings", t.chord("settings.open", "cmd+,")))
	b.WriteString(key("Menu bar", t.chord("menu.open", "f10")))
	b.WriteString("\nThemes, keybindings, and plugins live in Settings and in\n")
	b.WriteString("~/.ike/settings.toml; the palette finds every action by name.\n")
	b.WriteString("The help sheet (?) opens on the essentials — tab shows all.\n\n")
	b.WriteString("Next: pick language servers to install.\n")
	return b.String()
}
