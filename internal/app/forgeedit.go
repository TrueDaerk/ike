package app

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
	"ike/internal/ghissues"
	"ike/internal/host"
	"ike/internal/scratch"
	"ike/internal/ui"
)

// forgeedit.go binds editor buffers to forge texts (#2087): editing your own
// issue body or comment, and composing a new one, happens in a **real
// markdown buffer** — not an inline mini-editor in the issues pane.
//
// **Decision:** the buffer is a scratch file, the same machinery "Treat Buffer
// as …" materializes into (#2056, materialize.go). A forge text is exactly
// what the scratch store is for — a throwaway outside the project tree — and
// going through an ordinary file buffer means the edit gets the whole editor
// for free: vim motions, markdown highlighting, the preview pane, undo,
// autosave, crash recovery, session restore. The only thing added on top is a
// binding, keyed by path: the app remembers which forge text the buffer holds
// and what its text was when it opened.
//
// The save chain then hangs off the ordinary save signal. Writing the buffer
// (`:w`, Save All, autosave) fires editor.EventSave, the emitter reports the
// path, and a bound path dispatches forge.SaveTextCmd:
//
//   - success → the scratch file is removed, its buffer closed, and the issues
//     window refetches the listing and the open issue's timeline;
//   - stale base → nothing was written; a dialog offers overwrite or reload;
//   - failure → the buffer stays exactly as it is, with the error in a dialog
//     that retries on 'r'. No text is ever lost to a failed push.

// forgeEdit is one markdown buffer bound to a forge text.
type forgeEdit struct {
	target forge.TextTarget
	// base is the text the buffer opened with: the reference the stale-base
	// check compares the server against, updated when the user reloads.
	base string
	// title is the issue's title, for the dialogs and toasts.
	title string
	// saving guards against a second push while one is in flight — an
	// autosave landing on top of a manual write would otherwise race.
	saving bool
	// lastErr is the push error the buffer currently carries, "" when none;
	// the dialog shows it and 'r' retries with it.
	lastErr string
}

// openForgeEdit answers the issues pane's edit request: allocate a markdown
// scratch, seed it with the current text, open it through the standard funnel
// and bind it to the target.
func (m *Model) openForgeEdit(req ghissues.EditTextRequestMsg) (tea.Model, tea.Cmd) {
	// An already-open buffer for the same text is focused rather than
	// duplicated — two buffers over one comment would race each other's push.
	if path := m.forgeEditPath(req.Target); path != "" {
		m.host.Notify(host.Info, "already editing "+req.Target.Label())
		return m.openPath(path, false)
	}
	path, err := scratch.Create("md")
	if err != nil {
		m.host.Notify(host.Warn, "edit "+req.Target.Label()+": "+err.Error())
		return m, nil
	}
	// The tab should name what it edits, not "scratch-7.md". A name already
	// taken (a leftover from an abandoned edit) keeps the allocated one —
	// the binding is by path, so the name is cosmetic.
	if named, err := scratch.Rename(path, req.Target.Slug()+".md"); err == nil {
		path = named
	}
	base := forge.NormalizeText(req.Base)
	seed := base
	if seed != "" {
		seed += "\n"
	}
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		os.Remove(path)
		m.host.Notify(host.Warn, "edit "+req.Target.Label()+": "+err.Error())
		return m, nil
	}
	if m.forgeEdits == nil {
		m.forgeEdits = map[string]*forgeEdit{}
	}
	m.forgeEdits[path] = &forgeEdit{target: req.Target, base: base, title: req.Title}
	m.explorer().RefreshScratches()
	m.host.Notify(host.Info, "editing "+req.Target.Label()+" — save to push it to the forge")
	return m.openPath(path, false)
}

// describe names the bound text with the issue's title when one is known —
// "comment on #12 (add markdown preview)" — so a dialog raised long after the
// buffer was opened still says what it is about.
func (e *forgeEdit) describe() string {
	if e.title == "" {
		return e.target.Label()
	}
	return e.target.Label() + " (" + e.title + ")"
}

// forgeEditPath finds the open buffer already bound to target, "" when none.
func (m Model) forgeEditPath(target forge.TextTarget) string {
	for path, e := range m.forgeEdits {
		if e.target == target {
			return path
		}
	}
	return ""
}

// forgeEditSavedMsg reports that a bound buffer was written to disk. The
// editor emitter sends it from a goroutine, like every other save hook.
type forgeEditSavedMsg struct{ path string }

// pushForgeEdit dispatches the push for a saved bound buffer. The text comes
// from the file the save just wrote, so whatever the editor produced (its
// trailing newline, its line endings) is what travels.
func (m *Model) pushForgeEdit(path string, force bool) tea.Cmd {
	e := m.forgeEdits[path]
	if e == nil || e.saving {
		return nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		m.host.Notify(host.Error, "push "+e.target.Label()+": "+err.Error())
		return nil
	}
	if e.target.Kind == forge.TextNewComment && strings.TrimSpace(string(body)) == "" {
		// An empty new comment is the "opened it, changed my mind" case, not
		// something to post; the buffer stays for the text that may follow.
		return nil
	}
	e.saving, e.lastErr = true, ""
	return forge.SaveTextCmd(".", path, e.target, e.base, string(body), force)
}

// finishForgeEdit applies one finished push attempt.
func (m *Model) finishForgeEdit(msg forge.SaveTextMsg) tea.Cmd {
	e := m.forgeEdits[msg.Path]
	if e == nil {
		// The buffer was closed under the in-flight push (the user gave up on
		// it); a landed result has nothing left to apply.
		return nil
	}
	e.saving = false
	switch {
	case msg.Err != nil:
		e.lastErr = msg.Err.Error()
		m.openForgeEditDialog(msg.Path)
		return nil
	case msg.Stale:
		return m.openForgeStaleDialog(msg.Path, msg.Current)
	}
	m.closeForgeEdit(msg.Path)
	m.host.Notify(host.Info, "pushed "+e.target.Label())
	if p := m.issuesPanel(); p != nil {
		return p.RefreshAfterSave(e.target.Issue)
	}
	return nil
}

// closeForgeEdit drops the binding, closes the buffer and removes the scratch
// file — a pushed text has no reason to linger in the scratch store, where it
// would show up in the picker as a nameless duplicate of what the forge now
// holds.
func (m *Model) closeForgeEdit(path string) {
	delete(m.forgeEdits, path)
	m.closeEditorsForPath(path, false)
	if err := scratch.Delete(path); err == nil {
		m.explorer().RefreshScratches()
	}
}

// openForgeEditDialog raises the failed-push dialog: the error in full, and
// the two ways on. The buffer keeps every character — the dialog sits over
// it, it does not replace it.
func (m *Model) openForgeEditDialog(path string) {
	e := m.forgeEdits[path]
	if e == nil {
		return
	}
	if !m.forgeDialogFree() {
		m.host.Notify(host.Error, "push "+e.target.Label()+" failed: "+e.lastErr+" — save again to retry")
		return
	}
	m.forgeEditKey, m.forgeEditConflict, m.forgeEditStale = path, false, ""
	m.shell.SetContent(ui.ModelContent{
		Heading: "Pushing to the forge failed",
		Body: func() string {
			return e.describe() + " could not be saved:\n\n" +
				"  " + e.lastErr + "\n\n" +
				"Your text is untouched in the buffer.\n\n" +
				"  [r]   retry the push\n" +
				"  [esc] keep editing — save again whenever you want"
		},
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// openForgeStaleDialog raises the concurrent-edit warning: the text moved on
// the forge since the buffer opened, so a push would overwrite someone else's
// change. Nothing was written yet — the user decides.
func (m *Model) openForgeStaleDialog(path, current string) tea.Cmd {
	e := m.forgeEdits[path]
	if e == nil {
		return nil
	}
	if !m.forgeDialogFree() {
		m.host.Notify(host.Warn, e.target.Label()+" changed on the forge — nothing was pushed; save again to decide")
		return nil
	}
	m.forgeEditKey, m.forgeEditConflict, m.forgeEditStale = path, true, current
	m.shell.SetContent(ui.ModelContent{
		Heading: "Changed on the forge",
		Body: func() string {
			return e.describe() + " changed on the forge while you were editing it.\n\n" +
				"  [o]   overwrite — push my version anyway\n" +
				"  [l]   load — replace the buffer with the forge's version\n" +
				"  [esc] cancel — decide later (nothing was pushed)"
		},
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
	return nil
}

// forgeDialogFree reports whether a push dialog may take the screen. A push
// result lands while the user is somewhere else entirely — stealing an open
// prompt, the palette or the settings panel to announce it would drop what
// they were doing. Blocked, the outcome is announced instead; the buffer
// stays bound, so the next save re-runs the whole check and can raise the
// dialog then.
func (m Model) forgeDialogFree() bool {
	return !m.shell.IsOpen() && !m.overlayCapturesKeyboard()
}

// forgeEditDialogOpen reports whether one of the two edit dialogs owns the
// keyboard.
func (m Model) forgeEditDialogOpen() bool { return m.forgeEditKey != "" && m.shell.IsOpen() }

// updateForgeEditDialog consumes every key while an edit dialog is open, so a
// modal decision never leaks into the buffer underneath.
func (m Model) updateForgeEditDialog(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	path := m.forgeEditKey
	e := m.forgeEdits[path]
	if e == nil {
		return m.closeForgeEditDialog(), nil
	}
	stale := m.forgeEditConflict
	switch msg.String() {
	case "r":
		if stale {
			return m, nil
		}
		m.closeForgeEditDialog()
		return m, m.pushForgeEdit(path, false)
	case "o":
		if !stale {
			return m, nil
		}
		m.closeForgeEditDialog()
		return m, m.pushForgeEdit(path, true)
	case "l":
		if !stale {
			return m, nil
		}
		current := m.forgeEditStale
		m.closeForgeEditDialog()
		return m, m.reloadForgeEdit(path, current)
	case "esc":
		return m.closeForgeEditDialog(), nil
	}
	return m, nil
}

// closeForgeEditDialog dismisses the dialog, leaving the buffer and its
// binding exactly as they are.
func (m *Model) closeForgeEditDialog() tea.Model {
	m.forgeEditKey, m.forgeEditConflict, m.forgeEditStale = "", false, ""
	m.shell.Close()
	return *m
}

// reloadForgeEdit replaces a bound buffer's content with the forge's current
// version and re-bases it, the "their version wins" answer to a concurrent
// edit. The user's text is discarded knowingly — the dialog spells that out.
func (m *Model) reloadForgeEdit(path, current string) tea.Cmd {
	e := m.forgeEdits[path]
	if e == nil {
		return nil
	}
	text := forge.NormalizeText(current)
	seed := text
	if seed != "" {
		seed += "\n"
	}
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		m.host.Notify(host.Error, "reload "+e.target.Label()+": "+err.Error())
		return nil
	}
	e.base, e.lastErr = text, ""
	var cmds []tea.Cmd
	for _, ed := range m.editorViewsForPath(path) {
		// The clean-reload path the save-conflict prompt uses (#82): the
		// buffer's edits are discarded for the file just written.
		if c := ed.ResolveConflictReload(); c != nil {
			cmds = append(cmds, c)
		}
	}
	m.host.Notify(host.Info, "loaded the forge's version of "+e.target.Label())
	return tea.Batch(cmds...)
}

// ForgeEditTarget reports the forge text a path is bound to (tests).
func (m Model) ForgeEditTarget(path string) (forge.TextTarget, bool) {
	if e := m.forgeEdits[path]; e != nil {
		return e.target, true
	}
	return forge.TextTarget{}, false
}
