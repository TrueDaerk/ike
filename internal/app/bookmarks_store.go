package app

import (
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/bookmarks"
	"ike/internal/host"
	"ike/internal/ui"
)

// bookmarks_store.go wires the project bookmark store (#55) into the app:
// JetBrains-style line bookmarks with an optional mnemonic digit and a note,
// toggled from the editor (bookmark.toggle), annotated through a prompt,
// stepped through with bookmark.next/previous and listed in the same picker
// the vim marks use (bookmarks.go). The store persists per project in
// .ike/bookmarks.json, saved on every change and on each buffer save — the
// breakpoint store's arrangement, down to the shared path key.

// BookmarkToggleMsg toggles the bookmark on the focused editor's cursor line
// (bookmark.toggle, or a JetBrains-style F11).
type BookmarkToggleMsg struct{}

// BookmarkMnemonicMsg opens the mnemonic prompt: Jump selects the
// bookmark.jumpMnemonic flavour (a digit jumps), otherwise a digit assigns
// the mnemonic to the cursor line (bookmark.toggleMnemonic).
type BookmarkMnemonicMsg struct{ Jump bool }

// BookmarkNoteMsg opens the annotation prompt for the cursor line
// (bookmark.annotate).
type BookmarkNoteMsg struct{}

// BookmarkStepMsg jumps to the next (Delta 1) or previous (Delta -1)
// bookmark in the project, wrapping around.
type BookmarkStepMsg struct{ Delta int }

// bookmarkPromptKind selects what the open bookmark prompt does with its
// input.
type bookmarkPromptKind int

const (
	// bmPromptMnemonic assigns a digit to the cursor line's bookmark.
	bmPromptMnemonic bookmarkPromptKind = iota
	// bmPromptJump jumps to the bookmark carrying the typed digit.
	bmPromptJump
	// bmPromptNote edits the cursor line's annotation.
	bmPromptNote
)

// bookmarkPrompt is the open bookmark prompt (the save-layout prompt's shape,
// #1175): the mnemonic flavours consume a single digit, the note flavour is
// a line editor prefilled with the existing annotation.
type bookmarkPrompt struct {
	kind  bookmarkPromptKind
	key   string // store key of the target line (project-relative path)
	disp  string // the same path as shown to the user
	line  int    // 0-based
	input string
	pos   int
	// back reopens the bookmarks overview (#2251) once the prompt closes:
	// the overview's edit key routes through this very prompt, so saving or
	// cancelling must land back in the list it was opened from.
	back bool
}

// bookmarkHooks returns the editor-facing gutter-sign and adjuster closures
// (#55), the breakpointHooks pattern: they capture the store pointer, so
// every view renders the live set.
func bookmarkHooks(store *bookmarks.Store) (signs func(path string) map[int]string, adjust func(path string, cursorAfter, delta int)) {
	signs = func(path string) map[int]string { return store.Signs(bpKey(path)) }
	adjust = func(path string, cursorAfter, delta int) {
		store.AdjustEdit(bpKey(path), cursorAfter, delta)
	}
	return signs, adjust
}

// bookmarkPath turns a store key back into an openable path: project
// relative keys resolve against the project root, absolute ones stand.
func bookmarkPath(key string) string {
	if filepath.IsAbs(key) {
		return key
	}
	return filepath.Join(projectRoot(), key)
}

// saveBookmarks persists the store, surfacing a failure as a warning like
// the breakpoint store does.
func (m *Model) saveBookmarks() {
	if err := m.bmarks.Save(); err != nil {
		m.host.Notify(host.Warn, "bookmarks not saved: "+err.Error())
	}
}

// bookmarkTarget resolves the focused editor's file and cursor line into the
// store's key, explaining on the notification line when there is none.
func (m *Model) bookmarkTarget() (key, disp string, line int, ok bool) {
	ed := m.focusedEditor()
	if ed == nil || !ed.HasFile() {
		m.host.Notify(host.Info, "bookmarks need a focused editor with an open file")
		return "", "", 0, false
	}
	line, _ = ed.CursorPos()
	return bpKey(ed.Path()), displayPath(ed.Path()), line, true
}

// toggleBookmarkAtCursor is the bookmark.toggle handler: flip the anonymous
// bookmark on the focused editor's cursor line.
func (m *Model) toggleBookmarkAtCursor() {
	key, disp, line, ok := m.bookmarkTarget()
	if !ok {
		return
	}
	on := m.bmarks.Toggle(key, line)
	m.saveBookmarks()
	state := "removed"
	if on {
		state = "set"
	}
	m.host.Notify(host.Info, "bookmark "+state+" — "+disp+":"+strconv.Itoa(line+1))
}

// startBookmarkPrompt opens one of the bookmark prompts on the focused
// editor's cursor line; the jump flavour needs no target line, but still
// wants a focused editor to jump from.
func (m *Model) startBookmarkPrompt(kind bookmarkPromptKind) {
	key, disp, line, ok := m.bookmarkTarget()
	if !ok {
		return
	}
	if kind == bmPromptJump && m.bmarks.Count() == 0 {
		m.host.Notify(host.Info, "no bookmarks yet — set one with Toggle Bookmark")
		return
	}
	p := &bookmarkPrompt{kind: kind, key: key, disp: disp, line: line}
	if kind == bmPromptNote {
		if b, has := m.bmarks.At(key, line); has {
			p.input = b.Note
			p.pos = len([]rune(b.Note))
		}
	}
	m.bmPrompt = p
	m.renderBookmarkPrompt()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// startBookmarkNotePrompt opens the note prompt on an arbitrary bookmark
// rather than on the cursor line — the overview's edit action (#2251). back
// asks the prompt to reopen the overview once it closes.
func (m *Model) startBookmarkNotePrompt(b bookmarks.Bookmark, back bool) {
	p := &bookmarkPrompt{
		kind:  bmPromptNote,
		key:   b.Path,
		disp:  displayPath(bookmarkPath(b.Path)),
		line:  b.Line,
		input: b.Note,
		pos:   len([]rune(b.Note)),
		back:  back,
	}
	m.bmPrompt = p
	m.renderBookmarkPrompt()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// bookmarkPromptOpen reports whether the shell shows a bookmark prompt.
func (m Model) bookmarkPromptOpen() bool { return m.bmPrompt != nil && m.shell.IsOpen() }

// bookmarkMnemonicLine renders the digit row of the mnemonic prompts: a used
// digit shows its file:line, a free one shows a dot.
func (m Model) bookmarkMnemonicLine() string {
	var rows []string
	for r := '0'; r <= '9'; r++ {
		b, ok := m.bmarks.ByMnemonic(r)
		if !ok {
			continue
		}
		row := string(r) + "  " + displayPath(bookmarkPath(b.Path)) + ":" + strconv.Itoa(b.Line+1)
		if b.Note != "" {
			row += "  — " + b.Note
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return "no mnemonics assigned yet"
	}
	return strings.Join(rows, "\n")
}

// renderBookmarkPrompt (re)fills the shell for the open prompt.
func (m *Model) renderBookmarkPrompt() {
	p := m.bmPrompt
	if p == nil {
		return
	}
	where := p.disp + ":" + strconv.Itoa(p.line+1)
	switch p.kind {
	case bmPromptJump:
		body := m.bookmarkMnemonicLine()
		m.shell.SetContent(ui.ModelContent{
			Heading: "Jump to bookmark mnemonic",
			Body:    func() string { return body + "\n\n0-9 jump · esc cancel" },
		})
	case bmPromptNote:
		line := "> " + ui.CursorView(p.input, p.pos)
		m.shell.SetContent(ui.ModelContent{
			Heading: "Bookmark note — " + where,
			Body:    func() string { return line + "\n\nenter save · empty clears the note · esc cancel" },
		})
	default:
		body := m.bookmarkMnemonicLine()
		m.shell.SetContent(ui.ModelContent{
			Heading: "Bookmark mnemonic — " + where,
			Body: func() string {
				return body + "\n\n0-9 assign (again on this line removes the bookmark) · esc cancel"
			},
		})
	}
}

// closeBookmarkPrompt drops the prompt state and the shell.
func (m *Model) closeBookmarkPrompt() {
	m.bmPrompt = nil
	m.shell.Close()
}

// updateBookmarkPrompt consumes every key while a bookmark prompt is open:
// the mnemonic flavours take a single digit, the note flavour is a line
// editor with enter/esc; esc always cancels.
func (m Model) updateBookmarkPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := m.bmPrompt
	if p == nil {
		return m, nil
	}
	if msg.Code == tea.KeyEscape {
		back := p.back
		m.closeBookmarkPrompt()
		if back {
			m.openBookmarkOverview()
		}
		return m, nil
	}
	if p.kind == bmPromptNote {
		if msg.Code == tea.KeyEnter {
			note := strings.TrimSpace(p.input)
			key, line, back := p.key, p.line, p.back
			m.closeBookmarkPrompt()
			m.bmarks.SetNote(key, line, note)
			m.saveBookmarks()
			if note == "" {
				m.host.Notify(host.Info, "bookmark note cleared")
			} else {
				m.host.Notify(host.Info, "bookmark note saved")
			}
			if back {
				m.openBookmarkOverview()
			}
			return m, nil
		}
		if out, pos, handled, _ := ui.EditKey(msg, p.input, p.pos); handled {
			p.input, p.pos = out, pos
			m.renderBookmarkPrompt()
		}
		return m, nil
	}
	r := []rune(msg.Text)
	if len(r) != 1 || !bookmarks.Mnemonic(r[0]) {
		return m, nil // an unusable key leaves the prompt open
	}
	digit := r[0]
	if p.kind == bmPromptJump {
		b, ok := m.bmarks.ByMnemonic(digit)
		m.closeBookmarkPrompt()
		if !ok {
			m.host.Notify(host.Info, "no bookmark on mnemonic "+string(digit))
			return m, nil
		}
		return m.openPathAt(bookmarkPath(b.Path), b.Line, 0)
	}
	key, line, disp := p.key, p.line, p.disp
	m.closeBookmarkPrompt()
	// Pressing the digit the line already carries removes the bookmark —
	// JetBrains' toggle-with-mnemonic semantics.
	if b, ok := m.bmarks.At(key, line); ok && b.Mnemonic == digit {
		m.bmarks.Remove(key, line)
		m.saveBookmarks()
		m.host.Notify(host.Info, "bookmark "+string(digit)+" removed — "+disp+":"+strconv.Itoa(line+1))
		return m, nil
	}
	m.bmarks.SetMnemonic(key, line, digit)
	m.saveBookmarks()
	m.host.Notify(host.Info, "bookmark "+string(digit)+" set — "+disp+":"+strconv.Itoa(line+1))
	return m, nil
}

// pasteBookmarkPrompt inserts a paste into the note input at its cursor
// (#1873); the digit prompts take no text.
func (m *Model) pasteBookmarkPrompt(text string) bool {
	p := m.bmPrompt
	if p == nil || p.kind != bmPromptNote {
		return false
	}
	out, pos, changed := ui.PasteText(p.input, p.pos, text)
	if !changed {
		return false
	}
	p.input, p.pos = out, pos
	m.renderBookmarkPrompt()
	return true
}

// stepBookmark is the bookmark.next/previous handler: walk the project's
// bookmarks in (path, line) order from the focused editor's position,
// wrapping at both ends, and open the target through the standard funnel.
func (m Model) stepBookmark(delta int) (tea.Model, tea.Cmd) {
	all := m.bmarks.All()
	if len(all) == 0 {
		m.host.Notify(host.Info, "no bookmarks yet — set one with Toggle Bookmark")
		return m, nil
	}
	key, line := "", -1
	if ed := m.focusedEditor(); ed != nil && ed.HasFile() {
		key = bpKey(ed.Path())
		line, _ = ed.CursorPos()
	}
	after := func(b bookmarks.Bookmark) bool {
		if b.Path != key {
			return b.Path > key
		}
		return b.Line > line
	}
	target := all[0]
	if delta > 0 {
		target = all[0]
		for _, b := range all {
			if after(b) {
				target = b
				break
			}
		}
	} else {
		target = all[len(all)-1]
		for i := len(all) - 1; i >= 0; i-- {
			if !after(all[i]) && !(all[i].Path == key && all[i].Line == line) {
				target = all[i]
				break
			}
		}
	}
	return m.openPathAt(bookmarkPath(target.Path), target.Line, 0)
}

// renameBookmarks re-keys the store after an explorer rename/move (#55), so
// bookmarks follow their file instead of dangling on the old path.
func (m *Model) renameBookmarks(old, new string) {
	if m.bmarks.Count() == 0 {
		return
	}
	m.bmarks.Rename(bpKey(old), bpKey(new))
	m.saveBookmarks()
}
