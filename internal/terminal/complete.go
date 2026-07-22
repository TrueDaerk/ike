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
// The command line is read straight off the emulator's cursor row — the shell
// keeps owning line editing; accepting a candidate just pastes the remainder.
// Sources: executables on PATH (first word), files/dirs relative to the
// session's start directory, and make targets after `make`.

// maxCompItems bounds the popup list (and the per-source candidate scan).
const maxCompItems = 8

// completion is the popup state: full replacement words for the current
// prefix, the selected index, and whether the popup was opened by
// auto-suggest (auto closes on an empty word; ctrl+space shows everything).
type completion struct {
	open  bool
	items []string
	sel   int
	word  string
	auto  bool
}

// SetAutoSuggest toggles the while-typing trigger (terminal.autosuggest);
// ctrl+space stays available either way.
func (m *Model) SetAutoSuggest(on bool) { m.autoSuggest = on }

// completionActive reports whether the popup may operate at all: a live
// shell session on the primary screen, at the live view (no scrollback).
func (m *Model) completionActive() bool {
	return m.sess != nil && m.sess.Running() && !m.sess.IsCommand() &&
		!m.sess.AltScreen() && m.scroll == 0
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
		m.comp.sel = (m.comp.sel + len(m.comp.items) - 1) % len(m.comp.items)
		return true
	case "down":
		m.comp.sel = (m.comp.sel + 1) % len(m.comp.items)
		return true
	case "enter", "tab":
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
	cmd, word := parseCmdline(m.lineBeforeCursor())
	if auto && word == "" {
		m.comp = completion{}
		return
	}
	// Candidates resolve against the live cwd (#770), so file and make-target
	// suggestions follow a `cd` instead of the session's start directory.
	items := candidates(cmd, word, m.sess.Cwd(), os.Getenv("PATH"))
	if len(items) == 0 || (len(items) == 1 && items[0] == word) {
		m.comp = completion{}
		return
	}
	sel := 0
	if m.comp.open && m.comp.sel < len(items) && sameItems(m.comp.items, items) {
		sel = m.comp.sel
	}
	m.comp = completion{open: true, items: items, sel: sel, word: word, auto: auto}
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
// exact-prefix candidate pastes just the remainder (the word is already
// typed); a candidate matching only case-insensitively (#968) erases the
// typed word with backspaces first and pastes the candidate in its canonical
// case, so `mak` accepting `Makefile` lands as Makefile, not makMakefile.
// A directory keeps suggesting its children.
func (m *Model) acceptCompletion() {
	c := m.comp
	m.comp = completion{}
	if c.sel >= len(c.items) {
		return
	}
	item := c.items[c.sel]
	switch {
	case strings.HasPrefix(item, c.word):
		rest := strings.TrimPrefix(item, c.word)
		if rest == "" {
			return
		}
		m.sess.Paste(rest)
	default:
		for range []rune(c.word) {
			m.sess.SendKey(vt.KeyPressEvent{Code: vt.KeyBackspace})
		}
		m.sess.Paste(item)
	}
	if strings.HasSuffix(item, "/") {
		m.pendingSuggest = true // keep completing into the directory
	}
}

// lineBeforeCursor returns the cursor row's text left of the cursor — the
// live command line as far as completion is concerned.
func (m *Model) lineBeforeCursor() string {
	x, y := m.sess.CursorPosition()
	line := m.sess.LineText(m.sess.ScrollbackLen() + y)
	r := []rune(line)
	if x > len(r) {
		x = len(r)
	}
	return string(r[:x])
}

// parseCmdline extracts the command head and the word under the cursor from
// the text left of the cursor. The prompt is stripped heuristically (text up
// to the last "$ ", "% ", "> ", "# " or "❯ "); command separators (|, ;, &&,
// ||) start a fresh command; a trailing space means a fresh empty word.
func parseCmdline(before string) (cmd, word string) {
	for _, p := range []string{"$ ", "% ", "> ", "# ", "❯ "} {
		if i := strings.LastIndex(before, p); i >= 0 {
			before = before[i+len(p):]
		}
	}
	for _, sep := range []string{"&&", "||", "|", ";"} {
		if i := strings.LastIndex(before, sep); i >= 0 {
			before = before[i+len(sep):]
		}
	}
	fields := strings.Fields(before)
	endsSpace := before == "" || strings.HasSuffix(before, " ")
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
	if width+2 > m.w {
		width = m.w - 2
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
		if i == m.comp.sel {
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
	x := cx - len([]rune(m.comp.word))
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
