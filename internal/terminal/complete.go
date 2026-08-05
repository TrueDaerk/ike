package terminal

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/vt"

	"ike/internal/overlay"
)

// complete.go is the JetBrains-style command completion popup (#740): while
// the shell prompt is live (primary screen, no TUI app), typing auto-suggests
// completions for the current word and ctrl+space opens the popup on demand.
// The command line is read straight off the emulator's grid (soft-wrap chain
// joined, #1431) — the shell keeps owning line editing; accepting a candidate
// just pastes the remainder.
// Sources: executables on PATH (first word), files/dirs relative to the
// session's start directory, and make targets after `make`.

// maxCompItems bounds the popup list (and the per-source candidate scan).
const maxCompItems = 8

// completion is the popup state: full replacement words for the current
// prefix, the selected index, and whether the popup was opened by
// auto-suggest (auto closes on an empty word; ctrl+space shows everything).
// focused (#1432) gates enter-accept: an auto-suggest popup was never asked
// for, so enter keeps meaning "run the typed line" and passes through to the
// shell until the user engages the popup — via up/down, or by opening it
// explicitly with ctrl+space. Tab accepts regardless of focus.
type completion struct {
	open    bool
	items   []string
	sel     int
	word    string
	auto    bool
	focused bool
}

// SetAutoSuggest toggles the while-typing trigger (terminal.autosuggest);
// ctrl+space stays available either way.
func (m *Model) SetAutoSuggest(on bool) { m.autoSuggest = on }

// completionActive reports whether the popup may operate at all: a live
// shell session on the primary screen, at the live view (no scrollback), and
// with the shell itself at its prompt.
//
// The prompt check (#1340) is what keeps the popup out of a foreground
// program's way: while e.g. `python3 -c 'input("Give me something: ")'` reads
// stdin, shell-command and path suggestions are simply wrong, and the popup's
// own keys (tab/enter/arrows/esc) must reach the program instead of being
// swallowed. `Session.AtPrompt` answers it from the PTY's foreground process
// group, so it works with any shell — no prompt-integration marks required.
func (m *Model) completionActive() bool {
	return m.sess != nil && m.sess.Running() && !m.sess.IsCommand() &&
		!m.sess.AltScreen() && m.scroll == 0 && m.sess.AtPrompt()
}

// completionKey intercepts msg while the popup is open (or opens it on
// ctrl+space). It reports whether the key was consumed; unconsumed keys
// follow the normal raw route to the PTY.
func (m *Model) completionKey(msg string) bool {
	if !m.completionActive() {
		m.comp = completion{}
		return false
	}
	if msg == "ctrl+space" || msg == "ctrl+@" {
		// ctrl+space arrives as NUL (ctrl+@) from some terminals.
		m.refreshCompletion(false)
		return true
	}
	if !m.comp.open {
		return false
	}
	switch msg {
	case "esc":
		m.comp = completion{}
		return true
	case "up":
		m.comp.focused = true
		m.comp.sel = (m.comp.sel + len(m.comp.items) - 1) % len(m.comp.items)
		return true
	case "down":
		m.comp.focused = true
		m.comp.sel = (m.comp.sel + 1) % len(m.comp.items)
		return true
	case "enter":
		// Unfocused (#1432): the popup opened uninvited while typing, so
		// enter still means "run the typed line" — close and let the raw
		// route deliver the key to the shell.
		if !m.comp.focused {
			m.comp = completion{}
			return false
		}
		m.acceptCompletion()
		return true
	case "tab":
		m.acceptCompletion()
		return true
	}
	return false
}

// completionTyped is the post-forward hook of the raw route: a printable rune
// or backspace changed the command line, so the popup (or the auto-suggest
// trigger) wants a refresh once the shell's echo lands (OnOutput). str and
// text are the key's String() and Text forms.
func (m *Model) completionTyped(str, text string) {
	if !m.completionActive() {
		return
	}
	switch {
	case text != "" && !strings.ContainsAny(text, "\n\r"):
		if m.autoSuggest || m.comp.open {
			m.pendingSuggest = true
		}
	case str == "backspace":
		if m.comp.open {
			m.pendingSuggest = true
		}
	default:
		// Any other key (arrows, ctrl chords, enter) invalidates the popup:
		// the cursor or line may be anywhere now.
		m.comp = completion{}
	}
}

// OnOutput is the app's screen-changed hook (terminal.OutputMsg): the shell
// echoed the last keystrokes, so a pending auto-suggest (or an open popup)
// recomputes against the fresh cursor row.
func (m *Model) OnOutput() {
	if !m.pendingSuggest && !m.comp.open {
		return
	}
	auto := m.pendingSuggest || m.comp.auto
	m.pendingSuggest = false
	m.refreshCompletion(auto)
}

// refreshCompletion recomputes candidates for the word under the cursor.
// auto mode needs a non-empty word and quietly closes on no matches;
// ctrl+space (auto=false) completes the empty word too.
func (m *Model) refreshCompletion(auto bool) {
	if !m.completionActive() {
		m.comp = completion{}
		return
	}
	before := m.lineBeforeCursor()
	cmd, word := parseCmdline(before)
	if auto && (word == "" || !m.autoSuggestSafe(before)) {
		m.comp = completion{}
		return
	}
	// Candidates resolve against the live cwd (#770), so file and make-target
	// suggestions follow a `cd` instead of the session's start directory.
	// Matching runs on the unescaped word (#1552): the line holds `My\ Doc`,
	// the filesystem holds `My Documents`.
	items := candidates(unescapeShellWord(cmd), unescapeShellWord(word), m.sess.Cwd(), os.Getenv("PATH"))
	if len(items) == 0 || (len(items) == 1 && items[0] == unescapeShellWord(word)) {
		m.comp = completion{}
		return
	}
	sel := 0
	if m.comp.open && m.comp.sel < len(items) && sameItems(m.comp.items, items) {
		sel = m.comp.sel
	}
	// ctrl+space (auto=false) counts as engaging the popup, so enter accepts
	// right away; an auto refresh keeps whatever focus the user established
	// with up/down (#1432).
	focused := !auto || (m.comp.open && m.comp.focused)
	m.comp = completion{open: true, items: items, sel: sel, word: word, auto: auto, focused: focused}
}

// autoSuggestSafe reports whether an uninvited (auto-suggest) popup may open
// for the cursor line (#1464). Two cases stay silent; ctrl+space is unaffected
// and still completes with the soft-wrap chain joined:
//
//   - The command soft-wraps (the cursor sits on a continuation row of a
//     joined chain): #1431's acceptance criteria — no popup opens by itself
//     while a command wraps.
//   - The chain start could not be identified: the joined text carries no
//     prompt marker although the row above holds content. Then the cursor row
//     is likely a continuation row whose wrap the SoftWrapped heuristic missed
//     (e.g. a line editor that wraps early and leaves the last column blank),
//     and the "word" is a wrapped tail — PATH suggestions for it are garbage.
func (m *Model) autoSuggestSafe(before string) bool {
	_, y := m.sess.CursorPosition()
	cur := m.sess.ScrollbackLen() + y
	first, _ := m.logicalLineSpan(cur)
	if first < cur {
		return false
	}
	if !hasPromptMarker(before) && cur > 0 && m.sess.LineText(cur-1) != "" {
		return false
	}
	return true
}

func sameItems(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// acceptCompletion applies the selected candidate and closes the popup: an
// exact-prefix candidate types just the remainder (the word is already
// typed); a candidate matching only case-insensitively (#968) erases the
// typed word with backspaces first and types the candidate in its canonical
// case, so `mak` accepting `Makefile` lands as Makefile, not makMakefile.
// The text goes in as plain key presses, not a bracketed paste (#1442): zsh
// standout-highlights a pasted region by default, so the accepted text sat
// on the command line with a background.
//
// Accepting always ends the completion interaction, directories included
// (#1335): re-opening the popup on the accepted directory's contents left a
// preselected entry under the very next Enter, so `cd an` → tab → enter ran
// `cd ansible/ansible.cfg` instead of submitting `cd ansible/`. The pending
// refresh is cleared too — the typed remainder echoes back through OnOutput,
// which would otherwise reopen the popup for the accepted word. Typing on (or
// ctrl+space) completes inside the accepted directory as usual.
func (m *Model) acceptCompletion() {
	c := m.comp
	m.comp = completion{}
	m.pendingSuggest = false
	if c.sel >= len(c.items) {
		return
	}
	item := c.items[c.sel]
	// The popup's word is a snapshot from its last refresh. A keystroke whose
	// echo landed on the grid after that snapshot (fast type-then-tab, #1538)
	// leaves it stale, and a remainder computed from the snapshot double-types
	// the in-between keys (`mkdi` + tab inserted `ir` → `mkdiir`). Complete
	// against the word actually on the line instead — read the same way the
	// refresh reads it. A word that moved *away* from the candidate (or was
	// erased entirely) drops the accept rather than insert stale text.
	_, word := parseCmdline(m.lineBeforeCursor())
	if word == "" && c.word != "" {
		return
	}
	// Candidates are unescaped filesystem names while the parsed word keeps
	// the line's backslashes (#1552): match on the unescaped form, erase by
	// the escaped (on-screen) length.
	uword := unescapeShellWord(word)
	switch {
	case strings.HasPrefix(item, uword):
		rest := strings.TrimPrefix(item, uword)
		if rest == "" {
			return
		}
		// A dangling escape (`My\` mid-escape) would double up with the
		// escaped remainder — erase the lone backslash first.
		if hasDanglingEscape(word) {
			m.sess.SendKey(vt.KeyPressEvent{Code: vt.KeyBackspace})
		}
		m.sess.SendText(escapeShellWord(rest))
	case hasFoldPrefix(item, uword):
		// Case-correcting accept (#968): erase the word as it is on the line
		// now, not the snapshot's length.
		for range []rune(word) {
			m.sess.SendKey(vt.KeyPressEvent{Code: vt.KeyBackspace})
		}
		m.sess.SendText(escapeShellWord(item))
	}
}

// shellSpecials are the word-breaking and metacharacters a shell's own tab
// completion backslash-escapes when they occur in a filename (#1539). A
// leading "-" is not among them — shells leave it as-is too.
const shellSpecials = " \t'\"\\$`&|;()<>*?[]~#!{}"

// escapeShellWord backslash-escapes shell-special characters in s so the
// inserted completion lands on the command line as a single word (#1539):
// accepting `My Documents/` types `My\ Documents/`, one argument, the way
// the shell's own tab completion would.
func escapeShellWord(s string) string {
	if !strings.ContainsAny(s, shellSpecials) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(shellSpecials, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// lineBeforeCursor returns the logical command line's text left of the
// cursor. A command longer than the pane width soft-wraps onto continuation
// rows that carry neither the prompt nor the head of the command, so reading
// only the cursor row handed parseCmdline a garbage tail (#1431); instead the
// soft-wrap chain is walked back to the first row and the rows are joined —
// full rows padded to the pane width so a genuine trailing space survives the
// emulator's right-trim — before cutting at the cursor. The prompt only ever
// sits on the first row of the chain, so prompt stripping stays correct.
func (m *Model) lineBeforeCursor() string {
	x, y := m.sess.CursorPosition()
	cur := m.sess.ScrollbackLen() + y
	first, _ := m.logicalLineSpan(cur)
	w := m.sess.Width()
	var text []rune
	for l := first; l < cur; l++ {
		seg := []rune(m.sess.LineText(l))
		for len(seg) < w {
			seg = append(seg, ' ')
		}
		text = append(text, seg...)
	}
	r := []rune(m.sess.LineText(cur))
	if x > len(r) {
		x = len(r)
	}
	return string(append(text, r[:x]...))
}

// parseCmdline extracts the command head and the word under the cursor from
// the text left of the cursor. The prompt is stripped heuristically (text up
// to the last "$ ", "% ", "> ", "# " or "❯ "); unescaped command separators
// (|, ;, &) start a fresh command; an unescaped trailing space means a fresh
// empty word. Words are tokenized backslash-aware (#1552): `\<char>` belongs
// to the word — `My\ Doc` is one word, not two — and the escapes are kept
// verbatim, so the returned strings mirror the line's on-screen text (the
// accept path erases and anchors by that width; unescapeShellWord recovers
// the filesystem form for candidate matching).
func parseCmdline(before string) (cmd, word string) {
	for _, p := range promptMarkers {
		if i := strings.LastIndex(before, p); i >= 0 {
			before = before[i+len(p):]
		}
	}
	var fields []string
	var cur []rune
	esc := false
	endsSpace := true
	flush := func() {
		if len(cur) > 0 {
			fields = append(fields, string(cur))
			cur = cur[:0]
		}
	}
	for _, r := range before {
		if esc {
			cur = append(cur, r)
			esc = false
			continue
		}
		switch r {
		case '\\':
			esc = true
			cur = append(cur, r)
			endsSpace = false
		case ' ', '\t':
			flush()
			endsSpace = true
		case '|', ';', '&':
			cur = cur[:0]
			fields = fields[:0]
			endsSpace = true
		default:
			cur = append(cur, r)
			endsSpace = false
		}
	}
	flush()
	if len(fields) > 0 {
		cmd = fields[0]
	}
	if !endsSpace {
		word = fields[len(fields)-1]
	}
	if len(fields) == 1 && !endsSpace {
		cmd = word // still typing the command itself
	}
	return cmd, word
}

// unescapeShellWord strips the backslash escapes of a parseCmdline word,
// recovering the filesystem form (`My\ Doc` → `My Doc`) for candidate
// matching. A trailing lone backslash escapes a character yet to be typed
// and drops out.
func unescapeShellWord(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	esc := false
	for _, r := range s {
		if !esc && r == '\\' {
			esc = true
			continue
		}
		esc = false
		b.WriteRune(r)
	}
	return b.String()
}

// promptMarkers are the heuristic prompt terminators parseCmdline strips; a
// line containing none of them has no identifiable prompt (see
// autoSuggestSafe).
var promptMarkers = []string{"$ ", "% ", "> ", "# ", "❯ "}

// hasPromptMarker reports whether any prompt marker occurs in before.
func hasPromptMarker(before string) bool {
	for _, p := range promptMarkers {
		if strings.Contains(before, p) {
			return true
		}
	}
	return false
}

// hasDanglingEscape reports whether s ends in an unconsumed backslash — an
// escape whose character has not been typed yet.
func hasDanglingEscape(s string) bool {
	esc := false
	for _, r := range s {
		esc = !esc && r == '\\'
	}
	return esc
}

// candidates resolves the completion source for (cmd, word): PATH commands
// while the first word is being typed, make targets after `make`, files and
// directories relative to dir otherwise. Every candidate extends word (strict
// prefix match), so accepting can paste the remainder.
func candidates(cmd, word, dir, pathEnv string) []string {
	switch {
	case cmd == word && !strings.Contains(word, "/"):
		return commandCandidates(pathEnv, word)
	case cmd == "make" && !strings.Contains(word, "/"):
		return makeCandidates(dir, word)
	default:
		return pathCandidates(dir, word)
	}
}

// hasFoldPrefix reports whether s begins with prefix case-insensitively —
// the popup matches like the rest of the UI's typed searches (#968); the
// accept path case-corrects when the typed part differs.
func hasFoldPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return strings.EqualFold(s[:len(prefix)], prefix)
}

// commandCandidates lists executables on pathEnv matching the prefix.
func commandCandidates(pathEnv, prefix string) []string {
	seen := map[string]bool{}
	for _, d := range filepath.SplitList(pathEnv) {
		ents, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range ents {
			name := e.Name()
			if !hasFoldPrefix(name, prefix) || seen[name] || e.IsDir() {
				continue
			}
			if info, err := e.Info(); err != nil || info.Mode()&0o111 == 0 {
				continue
			}
			seen[name] = true
		}
	}
	return capSorted(seen)
}

// makeCandidates lists targets of the Makefile in dir matching the prefix.
func makeCandidates(dir, prefix string) []string {
	var data []byte
	for _, name := range []string{"Makefile", "makefile", "GNUmakefile"} {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			data = b
			break
		}
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" || line[0] == '\t' || line[0] == '#' {
			continue
		}
		head, _, ok := strings.Cut(line, ":")
		if !ok || strings.ContainsAny(head, "=$") {
			continue
		}
		for _, t := range strings.Fields(head) {
			if hasFoldPrefix(t, prefix) && !strings.HasPrefix(t, ".") && !seen[t] {
				seen[t] = true
			}
		}
	}
	return capSorted(seen)
}

// pathCandidates lists entries under dir matching the word, which may carry
// its own directory part (`src/ma` → entries of dir/src starting with "ma").
// Directories keep a trailing "/" so accepting descends. Dotfiles only show
// when the base prefix asks for them.
func pathCandidates(dir, word string) []string {
	sub, base := filepath.Split(word)
	root := filepath.Join(dir, filepath.FromSlash(sub))
	if strings.HasPrefix(word, "/") {
		root = filepath.FromSlash(sub)
	} else if strings.HasPrefix(word, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		root = filepath.Join(home, filepath.FromSlash(strings.TrimPrefix(sub, "~/")))
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	for _, e := range ents {
		name := e.Name()
		if !hasFoldPrefix(name, base) {
			continue
		}
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(base, ".") {
			continue
		}
		item := sub + name
		if e.IsDir() {
			item += "/"
		}
		seen[item] = true
	}
	return capSorted(seen)
}

// capSorted flattens the candidate set sorted and bounded.
func capSorted(seen map[string]bool) []string {
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	if len(out) > maxCompItems {
		out = out[:maxCompItems]
	}
	return out
}

// completionView composites the popup over the rendered grid, anchored at
// the start of the word under the cursor — below it when it fits, above
// otherwise.
func (m Model) completionView(view string) string {
	if !m.comp.open || len(m.comp.items) == 0 {
		return view
	}
	width := 0
	for _, it := range m.comp.items {
		if w := len([]rune(it)); w > width {
			width = w
		}
	}
	// The rendered box is width + 2 padding + 2 border columns wide.
	if width+4 > m.w {
		width = m.w - 4
	}
	if width < 1 {
		return view // pane too narrow for any readable popup
	}
	var b strings.Builder
	sel := lipgloss.NewStyle().Reverse(true)
	row := lipgloss.NewStyle().Faint(false)
	for i, it := range m.comp.items {
		r := []rune(it)
		if len(r) > width {
			r = r[:width]
		}
		line := " " + string(r) + strings.Repeat(" ", width-len(r)) + " "
		// Unfocused (#1442): no row highlights — enter would run the typed
		// line, so a reversed first row would falsely promise acceptance.
		if i == m.comp.sel && m.comp.focused {
			line = sel.Render(line)
		} else {
			line = row.Render(line)
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Render(b.String())
	cx, cy := m.sess.CursorPosition()
	// Anchor at the start of the word, but keep the whole box (width + border)
	// inside the pane (#1463): an anchor near the right edge shifts left, else
	// the overlaid rows exceed the pane width and the render wraps them apart.
	x := cx - len([]rune(m.comp.word))
	if x > m.w-width-4 {
		x = m.w - width - 4
	}
	if x < 0 {
		x = 0
	}
	boxH := len(m.comp.items) + 2
	y := cy + 1
	if y+boxH > m.h && cy-boxH >= 0 {
		y = cy - boxH
	}
	return overlay.Place(view, box, x, y, m.w, m.h)
}
