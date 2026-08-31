package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/scratch"
	"ike/internal/ui"
)

// scratch_manager.go is the scratch **manager** (#2256): a floating picker
// over the whole scratch store that not only opens a scratch but manages it —
// rename, delete, change language — without a detour through the explorer.
//
// The store already had every mutation (`scratch.Rename`, `scratch.Delete`,
// plus `scratch.SetExt` added here); what was missing was a surface reachable
// from anywhere, since the explorer's Scratches section needs the explorer
// pane and `scratch.list` is a pure finder. The manager is a shell dialog like
// the test-data wizard (#2228): steps walked by enter/esc, type-ahead
// narrowing through ui.SpeedSearch, and clickable rows and buttons.
//
// Mutations never touch open buffers directly. A rename (and a language
// change, which *is* a rename of the extension) emits explorer.FileMovedMsg
// and a delete explorer.FileDeletedMsg — the very messages the explorer's file
// ops announce — so open tabs re-point or close through the one path that
// already exists (#175), tab titles follow the new name and the editor's
// language state is reset by editor.SetPath. Nothing here re-implements that.
//
// Promote (ctrl+p, #2339) is the one action that leaves the manager: it takes
// the scratch out of the store entirely, so it hands the selected row to
// scratch.promote's own target-path prompt (scratch_promote.go) instead of
// adding a step here.

// Manager step indices; esc walks them backwards.
const (
	smStepList = iota
	smStepRename
	smStepDelete
	smStepLang
	// smStepCustomExt is the language step's own free-extension prompt
	// (#2340); esc walks back to the language list, not out of the dialog.
	smStepCustomExt
)

// ShowScratchManagerMsg asks the root model to open the scratch manager
// (scratch.manage, and the "Manage scratch files…" row of the scratch.new
// language picker).
type ShowScratchManagerMsg struct{}

// smHitKind tags what a rendered manager line targets, so a click acts on
// what the user visibly hit instead of re-deriving the layout.
type smHitKind int

const (
	smHitRow    smHitKind = iota // arg: index into the filtered entry list
	smHitLang                    // arg: index into the filtered language list
	smHitButton                  // arg: the key rune the button replays
)

// smHit is one clickable region: the body line it lies on, its column span
// ([x0, x1)), what it targets and which one.
type smHit struct {
	y, x0, x1 int
	kind      smHitKind
	arg       int
}

// smEntry is one row of the manager: the scratch plus the metadata the list
// renders. It is a snapshot — every mutation reloads it from the store, so a
// row can never describe a file that moved underneath it.
type smEntry struct {
	path string
	name string
	lang string
	size int64
	mod  time.Time
}

// smLang is one row of the language picker: the title shown and the extension
// the scratch gets. "Plain Text"/txt is pinned first, like in scratch.new's
// picker.
type smLang struct {
	title string
	ext   string
	// custom marks the "Custom…" row (#2340): it carries no extension but
	// opens the free-extension prompt, and it is never filtered away.
	custom bool
}

// scratchMgrState is the open manager; nil when it is closed.
type scratchMgrState struct {
	step    int
	entries []smEntry
	pick    int
	top     int
	search  ui.SpeedSearch

	// The rename step edits the selected scratch's file name.
	renameInput string
	renamePos   int

	// The language step picks an extension for the selected scratch.
	langs      []smLang
	langPick   int
	langTop    int
	langSearch ui.SpeedSearch

	// The custom-extension step types an extension the language list does not
	// offer (#2340).
	customInput string
	customPos   int

	// hits are the clickable regions of the last render, rebuilt by every
	// renderScratchManager call.
	hits []smHit

	err  string
	note string
}

// scratchManageCommandID is the manager's palette command.
const scratchManageCommandID = "scratch.manage"

// startScratchManager loads the store and opens the manager on its list step.
// An empty store still opens — the dialog says so and points at the creator,
// which is friendlier than a toast that leaves the user where they were.
func (m *Model) startScratchManager() {
	s := &scratchMgrState{}
	s.reload("")
	m.scratchMgr = s
	m.renderScratchManager()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// scratchManagerOpen reports whether the shell currently shows the manager.
func (m Model) scratchManagerOpen() bool { return m.scratchMgr != nil && m.shell.IsOpen() }

// closeScratchManager clears the manager state and the shell.
func (m *Model) closeScratchManager() {
	m.scratchMgr = nil
	m.shell.Close()
}

// reload re-reads the store into the rows and keeps the selection on keep
// (a path) when it is still there — a rename or a language change moves the
// row inside the newest-first order, and the cursor must follow the file the
// user just acted on rather than whatever slid under it.
func (s *scratchMgrState) reload(keep string) {
	entries, err := scratch.Entries()
	if err != nil {
		s.err = err.Error()
	}
	s.entries = s.entries[:0]
	for _, e := range entries {
		s.entries = append(s.entries, smEntry{
			path: e.Path,
			name: filepath.Base(e.Path),
			lang: scratchLangTitle(e.Path),
			size: e.Size,
			mod:  e.ModTime,
		})
	}
	s.pick, s.top = 0, 0
	if keep == "" {
		return
	}
	for i, e := range s.visible() {
		if e.path == keep {
			s.pick = i
			return
		}
	}
}

// scratchLangTitle names the language of a scratch path for the metadata
// column, falling back to "Plain Text" for an unregistered extension — the
// same rendering scratch.list's Detail chip uses. It is the picker's own row
// title, so a .js scratch reads "JavaScript" and not the id of the language
// that happens to own the extension (#2333).
func scratchLangTitle(path string) string { return scratchRowTitle(path) }

// visible is the row set the list renders: the entries narrowed by the
// type-ahead, matched over "name language" so typing either finds a scratch.
func (s *scratchMgrState) visible() []smEntry {
	return ui.Narrow(&s.search, s.entries, func(e smEntry) string { return e.name + " " + e.lang })
}

// selected returns the scratch under the cursor, false when the filtered list
// is empty (an empty store, or a query that matches nothing).
func (s *scratchMgrState) selected() (smEntry, bool) {
	rows := s.visible()
	if len(rows) == 0 {
		return smEntry{}, false
	}
	if s.pick >= len(rows) {
		s.pick = len(rows) - 1
	}
	if s.pick < 0 {
		s.pick = 0
	}
	return rows[s.pick], true
}

// scratchLangs builds the language-picker rows: plain text pinned first, then
// the shared scratch offering (scratch_langs.go), which is already sorted by
// title and free of duplicate extensions, and "Custom…" pinned last (#2340)
// for everything the curated offering does not list.
func scratchLangs() []smLang {
	rows := scratchLangRows()
	out := make([]smLang, 0, len(rows)+2)
	out = append(out, smLang{title: "Plain Text", ext: "txt"})
	for _, r := range rows {
		out = append(out, smLang{title: r.Title, ext: r.Ext})
	}
	return append(out, smLang{title: scratchCustomTitle, custom: true})
}

// filteredLangs is the language picker's row set, narrowed by its own
// type-ahead over "title .ext". The "Custom…" row survives every filter and
// stays last: a query that matches no language is exactly when a free
// extension is wanted, so the row must be there to be chosen.
func (s *scratchMgrState) filteredLangs() []smLang {
	langs := make([]smLang, 0, len(s.langs))
	var custom []smLang
	for _, l := range s.langs {
		if l.custom {
			custom = append(custom, l)
			continue
		}
		langs = append(langs, l)
	}
	out := ui.Narrow(&s.langSearch, langs, func(l smLang) string { return l.title + " ." + l.ext })
	return append(out, custom...)
}

// smListRows is the height budget of the manager's list: the shell's laid-out
// viewport minus the dialog's fixed rows, so a long store scrolls inside the
// box rather than pushing the hint lines out of sight.
func (m *Model) smListRows(n int) int {
	rows := m.shell.ViewportRows() - 9
	if rows <= 0 {
		rows = m.height - 14 // before the shell's first layout
	}
	if rows < 4 {
		rows = 4
	}
	if rows > n {
		rows = n
	}
	return rows
}

// renderScratchManager (re)fills the shell for the current step and rebuilds
// the click map: every clickable line registers a hit while it is added, so
// the hit test can never drift from the rendering.
func (m *Model) renderScratchManager() {
	s := m.scratchMgr
	// The row budget is the wizard's value width plus the metadata columns,
	// but never wider than the terminal minus the dialog's chrome — an
	// overlong scratch name must be clipped, not pushed past the box.
	avail := min(m.tdValueWidth()+26, max(m.width-10, 20))
	var lines []string
	s.hits = s.hits[:0]
	add := func(l string) { lines = append(lines, l) }
	addHit := func(l string, kind smHitKind, arg int) {
		s.hits = append(s.hits, smHit{y: len(lines), x0: 0, x1: len([]rune(l)), kind: kind, arg: arg})
		add(l)
	}
	addButtons := func(btns ...[2]string) { // {label, key}
		var b []rune
		y := len(lines)
		for _, btn := range btns {
			if len(b) > 0 {
				b = append(b, ' ', ' ')
			}
			x0 := len(b)
			b = append(b, []rune("["+btn[0]+"]")...)
			s.hits = append(s.hits, smHit{y: y, x0: x0, x1: len(b), kind: smHitButton, arg: int([]rune(btn[1])[0])})
		}
		add(string(b))
	}
	const esc, enter = "\x1b", "\r"
	switch s.step {
	case smStepList:
		rows := s.visible()
		add(smPad("Name", 24) + smPad("Language", 14) + smPad("Size", 10) + "Modified")
		add("")
		switch {
		case len(s.entries) == 0:
			add("  (no scratch files — \"New Scratch File…\" creates one)")
		case len(rows) == 0:
			add("  (no scratch matches — backspace edits the filter, esc clears it)")
		}
		if s.pick >= len(rows) {
			s.pick = len(rows) - 1
		}
		if s.pick < 0 {
			s.pick = 0
		}
		vis := m.smListRows(len(rows))
		top := tdWindow(&s.top, s.pick, len(rows), vis)
		if top > 0 {
			add(fmt.Sprintf("  ↑ %d more", top))
		}
		now := time.Now()
		for i := top; i < top+vis && i < len(rows); i++ {
			e := rows[i]
			marker := "  "
			if i == s.pick {
				marker = "> "
			}
			row := marker + smPad(e.name, 24) + smPad(e.lang, 14) +
				smPad(humanBytes(e.size), 10) + ui.ShortAge(e.mod, now)
			addHit(tdTrunc(row, avail), smHitRow, i)
		}
		if rest := len(rows) - top - vis; rest > 0 {
			add(fmt.Sprintf("  ↓ %d more", rest))
		}
		if s.search.Active() {
			add("")
			add("  filter: " + s.search.Query())
		}
		add("")
		addButtons([2]string{"open", enter}, [2]string{"rename", "\x12"},
			[2]string{"language", "\x0c"}, [2]string{"promote", "\x10"},
			[2]string{"delete", "\x04"}, [2]string{"close", esc})
		add("")
		add("type filters · ↑↓ select · enter open · esc close")
		add("ctrl+r rename · ctrl+l language · ctrl+p promote · ctrl+d delete")
	case smStepRename:
		e, _ := s.selected()
		add("Rename " + e.name)
		add("")
		add("  " + windowedInput(s.renameInput, s.renamePos, m.tdValueWidth()))
		add("")
		add("The name is a plain file name; its extension decides the language.")
		add("")
		addButtons([2]string{"cancel", esc}, [2]string{"rename", enter})
		add("")
		add("enter rename · esc back")
	case smStepDelete:
		e, _ := s.selected()
		add("Delete " + e.name + "?")
		add("")
		add("  Scratches have no trash — this removes the file permanently.")
		add("")
		addButtons([2]string{"cancel", esc}, [2]string{"delete", "y"})
		add("")
		add("y/enter delete · n/esc cancel")
	case smStepLang:
		e, _ := s.selected()
		add("Language of " + e.name + " — type to filter")
		add("")
		filtered := s.filteredLangs()
		if s.langSearch.Active() {
			add("  filter: " + s.langSearch.Query())
			add("")
		}
		if len(filtered) == 0 {
			add("  (no language matches — backspace edits the filter, esc clears it)")
		}
		if s.langPick >= len(filtered) {
			s.langPick = len(filtered) - 1
		}
		if s.langPick < 0 {
			s.langPick = 0
		}
		vis := m.smListRows(len(filtered))
		top := tdWindow(&s.langTop, s.langPick, len(filtered), vis)
		if top > 0 {
			add(fmt.Sprintf("  ↑ %d more", top))
		}
		for i := top; i < top+vis && i < len(filtered); i++ {
			l := filtered[i]
			marker := "  "
			if i == s.langPick {
				marker = "> "
			}
			detail := "." + l.ext
			if l.custom {
				detail = scratchCustomDetail
			}
			addHit(tdTrunc(marker+smPad(l.title, 20)+detail, avail), smHitLang, i)
		}
		if rest := len(filtered) - top - vis; rest > 0 {
			add(fmt.Sprintf("  ↓ %d more", rest))
		}
		add("")
		add("The scratch keeps its name and only swaps its extension.")
		add("")
		addButtons([2]string{"back", esc}, [2]string{"choose", enter})
		add("")
		add("type filters · ↑↓ select · enter/click choose · esc back")
	case smStepCustomExt:
		e, _ := s.selected()
		add("Extension of " + e.name)
		add("")
		add("  " + windowedInput(s.customInput, s.customPos, m.tdValueWidth()))
		add("")
		add("Any extension the list does not offer: \"tf\", \".mjs\", \"sql\".")
		add("")
		addButtons([2]string{"back", esc}, [2]string{"apply", enter})
		add("")
		add("enter apply · esc back to the language list")
	}
	if s.err != "" {
		lines = append(lines, "", "E: "+s.err)
	} else if s.note != "" {
		lines = append(lines, "", s.note)
	}
	body := strings.Join(lines, "\n")
	m.shell.SetContent(ui.ModelContent{
		Heading: "Scratch Files",
		Body:    func() string { return body },
	})
}

// smPad left-aligns a column value in n columns, rune-counted and clipped, so
// a long scratch name cannot push the metadata columns out of alignment.
func smPad(s string, n int) string {
	r := []rune(s)
	if len(r) >= n {
		return string(r[:n-1]) + " "
	}
	return s + strings.Repeat(" ", n-len(r))
}

// updateScratchManager consumes every key while the manager is open.
func (m Model) updateScratchManager(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.scratchMgr
	if msg.Code == tea.KeyEscape {
		switch {
		case s.step == smStepCustomExt:
			// One step back, not out of the dialog: the row list is where a
			// wrong turn into the prompt is corrected (#2340).
			s.step = smStepLang
			s.err, s.note = "", ""
		case s.step == smStepLang && s.langSearch.EscClears():
			s.langPick, s.langTop = 0, 0
		case s.step != smStepList:
			s.step = smStepList
			s.err, s.note = "", ""
		case s.search.EscClears():
			s.pick, s.top = 0, 0
		default:
			m.closeScratchManager()
			return m, nil
		}
		m.renderScratchManager()
		return m, nil
	}
	switch s.step {
	case smStepList:
		return m.updateScratchManagerList(msg)
	case smStepRename:
		return m.updateScratchRename(msg)
	case smStepDelete:
		return m.updateScratchDelete(msg)
	case smStepLang:
		return m.updateScratchLang(msg)
	case smStepCustomExt:
		return m.updateScratchCustomLang(msg)
	}
	return m, nil
}

// updateScratchManagerList drives the list step: printable keys narrow the
// type-ahead (which is why the actions are chords — a letter belongs to the
// search), the navigation keys move the selection, enter opens.
func (m Model) updateScratchManagerList(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.scratchMgr
	switch msg.String() {
	case "enter":
		e, ok := s.selected()
		if !ok {
			return m, nil
		}
		m.closeScratchManager()
		return m.openPath(e.path, false)
	case "ctrl+r", "f2":
		if e, ok := s.selected(); ok {
			s.step = smStepRename
			s.renameInput = e.name
			s.renamePos = len([]rune(e.name))
			s.err, s.note = "", ""
		}
	case "ctrl+d", "delete":
		if _, ok := s.selected(); ok {
			s.step = smStepDelete
			s.err, s.note = "", ""
		}
	case "ctrl+l":
		if _, ok := s.selected(); ok {
			m.openScratchLangPicker()
		}
	case "ctrl+p":
		// Promote (#2339) leaves the manager rather than adding a step: the
		// scratch is on its way out of the store, and the target-path prompt
		// is the same one the palette command opens.
		if e, ok := s.selected(); ok {
			m.closeScratchManager()
			return m.Update(PromoteScratchMsg{Path: e.path})
		}
	default:
		if handled, changed := s.search.Key(msg); handled {
			if changed {
				s.pick, s.top = 0, 0
				s.err, s.note = "", ""
			}
			break
		}
		nav := s.pick
		if ui.ListNav(msg.String(), &nav, len(s.visible()), m.smListRows(len(s.visible())), ui.NavDefault) {
			s.pick = nav
		}
	}
	m.renderScratchManager()
	return m, nil
}

// openScratchLangPicker moves to the language step with the scratch's current
// language selected and the filter cleared.
func (m *Model) openScratchLangPicker() {
	s := m.scratchMgr
	s.langs = scratchLangs()
	s.langSearch.Reset()
	s.langPick, s.langTop = 0, 0
	if e, ok := s.selected(); ok {
		// Matched on the extension, not the title: several rows can share a
		// language (#2333), and only the extension says which one the scratch
		// currently is.
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(e.name), "."))
		for i, l := range s.langs {
			if l.ext == ext {
				s.langPick = i
				break
			}
		}
	}
	s.step = smStepLang
	s.err, s.note = "", ""
}

// updateScratchRename drives the rename step: enter renames through the store
// — a collision or an invalid name keeps the prompt open with the store's own
// message — and everything else is line editing.
func (m Model) updateScratchRename(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.scratchMgr
	if msg.Code == tea.KeyEnter {
		return m.applyScratchRename(strings.TrimSpace(s.renameInput))
	}
	if out, pos, handled, changed := ui.EditKey(msg, s.renameInput, s.renamePos); handled {
		s.renameInput, s.renamePos = out, pos
		if changed {
			s.err = ""
		}
	}
	m.renderScratchManager()
	return m, nil
}

// applyScratchRename runs the store rename and announces it. The rename is
// shared by the rename prompt and the language change, since re-languaging a
// scratch is a rename of its extension: both re-point open buffers through
// explorer.FileMovedMsg, so a scratch open in an editor follows its new name
// and its new highlighting without this file touching a buffer.
func (m Model) applyScratchRename(name string) (tea.Model, tea.Cmd) {
	s := m.scratchMgr
	e, ok := s.selected()
	if !ok {
		return m, nil
	}
	if name == "" {
		s.err = "the name is required"
		m.renderScratchManager()
		return m, nil
	}
	if name == e.name {
		s.step = smStepList
		s.err, s.note = "", ""
		m.renderScratchManager()
		return m, nil
	}
	target, err := scratch.Rename(e.path, name)
	if err != nil {
		s.err = err.Error()
		m.renderScratchManager()
		return m, nil
	}
	s.step = smStepList
	s.err = ""
	s.note = "renamed to " + filepath.Base(target)
	s.reload(target)
	m.explorer().RefreshScratches()
	m.renderScratchManager()
	return m, scratchMovedCmd(e.path, target)
}

// updateScratchDelete drives the delete confirmation: y/enter deletes, n
// cancels (esc is handled by the step walker above).
func (m Model) updateScratchDelete(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.scratchMgr
	switch msg.String() {
	case "y", "Y", "enter":
		e, ok := s.selected()
		if !ok {
			return m, nil
		}
		if err := scratch.Delete(e.path); err != nil {
			s.step = smStepList
			s.err = err.Error()
			m.renderScratchManager()
			return m, nil
		}
		s.step = smStepList
		s.err = ""
		s.note = "deleted " + e.name
		pick := s.pick
		s.reload("")
		s.pick = min(pick, max(len(s.visible())-1, 0))
		m.explorer().RefreshScratches()
		m.renderScratchManager()
		return m, scratchDeletedCmd(e.path)
	case "n", "N":
		s.step = smStepList
		m.renderScratchManager()
	}
	return m, nil
}

// updateScratchLang drives the language picker: the type-ahead narrows, the
// navigation keys move, enter re-languages the scratch through the store.
func (m Model) updateScratchLang(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.scratchMgr
	filtered := s.filteredLangs()
	if msg.Code == tea.KeyEnter {
		if len(filtered) == 0 {
			return m, nil
		}
		return m.chooseScratchLang(filtered[min(s.langPick, len(filtered)-1)])
	}
	if handled, changed := s.langSearch.Key(msg); handled {
		if changed {
			s.langPick, s.langTop = 0, 0
		}
		m.renderScratchManager()
		return m, nil
	}
	nav := s.langPick
	if ui.ListNav(msg.String(), &nav, len(filtered), m.smListRows(len(filtered)), ui.NavDefault) {
		s.langPick = nav
	}
	m.renderScratchManager()
	return m, nil
}

// chooseScratchLang activates one language row: an ordinary row re-languages
// the scratch right away, the "Custom…" row (#2340) opens the extension
// prompt instead — it is a step, not an extension.
func (m Model) chooseScratchLang(l smLang) (tea.Model, tea.Cmd) {
	if !l.custom {
		return m.applyScratchLang(l.ext)
	}
	s := m.scratchMgr
	s.step = smStepCustomExt
	// The current extension seeds the field, so the prompt starts from what
	// the scratch is rather than from nothing.
	e, _ := s.selected()
	s.customInput = strings.ToLower(strings.TrimPrefix(filepath.Ext(e.name), "."))
	s.customPos = len([]rune(s.customInput))
	s.err, s.note = "", ""
	m.renderScratchManager()
	return m, nil
}

// updateScratchCustomLang drives the manager's extension prompt: enter
// validates and re-languages through the very same store call the rows use, a
// refusal keeps the prompt open with its reason (esc is the step walker's).
func (m Model) updateScratchCustomLang(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.scratchMgr
	if msg.Code == tea.KeyEnter {
		ext, err := normalizeScratchExt(s.customInput)
		if err != nil {
			s.err = err.Error()
			m.renderScratchManager()
			return m, nil
		}
		return m.applyScratchLang(ext)
	}
	if out, pos, handled, changed := ui.EditKey(msg, s.customInput, s.customPos); handled {
		s.customInput, s.customPos = out, pos
		if changed {
			s.err = ""
		}
	}
	m.renderScratchManager()
	return m, nil
}

// applyScratchLang swaps the selected scratch's extension, which is the whole
// language change: highlighting, LSP and comment toggling all resolve from the
// path, and the open buffer follows it through the same move announcement a
// rename makes.
func (m Model) applyScratchLang(ext string) (tea.Model, tea.Cmd) {
	s := m.scratchMgr
	e, ok := s.selected()
	if !ok {
		return m, nil
	}
	target, err := scratch.SetExt(e.path, ext)
	if err != nil {
		s.err = err.Error()
		m.renderScratchManager()
		return m, nil
	}
	s.step = smStepList
	s.err = ""
	if target == e.path {
		s.note = e.name + " already is a ." + strings.TrimPrefix(ext, ".") + " scratch"
		m.renderScratchManager()
		return m, nil
	}
	s.note = filepath.Base(target) + " is now " + scratchLangTitle(target)
	s.reload(target)
	m.explorer().RefreshScratches()
	m.renderScratchManager()
	return m, scratchMovedCmd(e.path, target)
}

// scratchMovedCmd announces a renamed scratch the way the explorer's file ops
// do, so open editors re-point (and re-parse under the new language) through
// the app's one FileMovedMsg handler.
func scratchMovedCmd(old, new string) tea.Cmd {
	return func() tea.Msg { return explorer.FileMovedMsg{Old: old, New: new} }
}

// scratchDeletedCmd announces a removed scratch, so an editor still showing it
// closes exactly like after an explorer delete.
func scratchDeletedCmd(path string) tea.Cmd {
	return func() tea.Msg { return explorer.FileDeletedMsg{Path: path} }
}

// mouseScratchManager acts on the hit region the last render recorded for the
// clicked body line (x, y are content-local). A wheel event moves the current
// step's list selection instead. It reports whether it consumed the event.
func (m Model) mouseScratchManager(msg mouseEvent, x, y int) (tea.Model, tea.Cmd, bool) {
	s := m.scratchMgr
	if s == nil {
		return m, nil, false
	}
	if msg.action == mouseWheel {
		switch msg.Button {
		case tea.MouseWheelUp:
			m.wheelScratchManager(-wheelLines * msg.ticks())
		case tea.MouseWheelDown:
			m.wheelScratchManager(wheelLines * msg.ticks())
		default:
			return m, nil, false
		}
		return m, nil, true
	}
	var hit *smHit
	for i := range s.hits {
		h := s.hits[i]
		if h.y == y && x >= h.x0 && x < h.x1 {
			hit = &s.hits[i]
			break
		}
	}
	if hit == nil {
		return m, nil, true
	}
	switch hit.kind {
	case smHitButton:
		key := tea.Key{Code: rune(hit.arg), Text: string(rune(hit.arg))}
		switch rune(hit.arg) {
		case '\r':
			key = tea.Key{Code: tea.KeyEnter}
		case '\x1b':
			key = tea.Key{Code: tea.KeyEscape}
		case '\x12':
			key = tea.Key{Code: 'r', Mod: tea.ModCtrl}
		case '\x0c':
			key = tea.Key{Code: 'l', Mod: tea.ModCtrl}
		case '\x10':
			key = tea.Key{Code: 'p', Mod: tea.ModCtrl}
		case '\x04':
			key = tea.Key{Code: 'd', Mod: tea.ModCtrl}
		}
		out, cmd := m.updateScratchManager(tea.KeyPressMsg(key))
		return out.(Model), cmd, true
	case smHitRow:
		if s.pick == hit.arg {
			// A second click on the selected row opens it, the list-picker
			// convention of the test-data wizard.
			out, cmd := m.updateScratchManager(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
			return out.(Model), cmd, true
		}
		s.pick = hit.arg
		s.note = ""
	case smHitLang:
		filtered := s.filteredLangs()
		if hit.arg >= 0 && hit.arg < len(filtered) {
			out, cmd := m.chooseScratchLang(filtered[hit.arg])
			return out.(Model), cmd, true
		}
	}
	m.renderScratchManager()
	return m, nil, true
}

// wheelScratchManager moves the current step's list selection by delta rows,
// so the wheel scrolls what the arrows walk and the selection can never be
// scrolled out of reach.
func (m *Model) wheelScratchManager(delta int) {
	s := m.scratchMgr
	move := func(pick *int, n int) {
		if n == 0 {
			return
		}
		p := *pick + delta
		if p < 0 {
			p = 0
		}
		if p >= n {
			p = n - 1
		}
		*pick = p
	}
	switch s.step {
	case smStepList:
		move(&s.pick, len(s.visible()))
	case smStepLang:
		move(&s.langPick, len(s.filteredLangs()))
	default:
		return
	}
	m.renderScratchManager()
}

// pasteScratchManager inserts a paste into the rename field (#1873) or the
// extension field (#2340); the list steps have no input to paste into.
func (m *Model) pasteScratchManager(text string) bool {
	s := m.scratchMgr
	if s == nil || (s.step != smStepRename && s.step != smStepCustomExt) {
		return false
	}
	in, pos := s.renameInput, s.renamePos
	if s.step == smStepCustomExt {
		in, pos = s.customInput, s.customPos
	}
	out, pos, changed := ui.PasteText(in, pos, text)
	if !changed {
		return false
	}
	if s.step == smStepCustomExt {
		s.customInput, s.customPos = out, pos
	} else {
		s.renameInput, s.renamePos = out, pos
	}
	s.err = ""
	m.renderScratchManager()
	return true
}
