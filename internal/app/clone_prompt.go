package app

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/host"
	"ike/internal/project"
	"ike/internal/ui"
	"ike/internal/vcs"
)

// clone_prompt.go drives "Clone Repository" (#1349), IKE's counterpart to
// JetBrains' Get-from-VCS: a two-field dialog (repository URL, directory name)
// that clones into the configured project directory and switches the IDE to
// the clone. The dialog owns the keyboard like the other shell prompts
// (saveas.go, #730); the git work is one async tea.Cmd, so the UI keeps
// rendering while the clone runs.

// cloneFieldURL / cloneFieldName index the two inputs; tab moves between them.
const (
	cloneFieldURL = iota
	cloneFieldName
)

// startClonePrompt opens the clone dialog with empty fields.
func (m *Model) startClonePrompt() {
	m.cloneOpen = true
	m.cloneRunning = false
	m.cloneURL.Clear()
	m.cloneName.Clear()
	m.cloneNameEdited = false
	m.cloneField = cloneFieldURL
	m.cloneErr = ""
	m.renderClonePrompt()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// startClonePromptURL opens the clone dialog pre-filled with url — the deep
// link's remote, verbatim (#2396): the user sees exactly what was linked and
// still confirms (or edits) before anything is cloned. The directory name
// follows the URL like a hand-typed one would.
func (m *Model) startClonePromptURL(url string) {
	m.startClonePrompt()
	m.cloneURL.Set(url)
	m.cloneName.Set(vcs.CloneName(url))
	m.renderClonePrompt()
}

// clonePromptOpen reports whether the shell currently shows the clone dialog.
func (m Model) clonePromptOpen() bool { return m.cloneOpen && m.shell.IsOpen() }

// closeClonePrompt clears the dialog state and the shell.
func (m *Model) closeClonePrompt() {
	m.cloneOpen = false
	m.cloneRunning = false
	m.cloneURL.Clear()
	m.cloneName.Clear()
	m.cloneNameEdited = false
	m.cloneField = cloneFieldURL
	m.cloneErr = ""
	// A deep link waiting on this dialog dies with it (#2396): cancelling the
	// clone cancels the link's whole payload, not just the checkout.
	m.dlAfterClone = nil
	m.shell.Close()
}

// cloneTargetHint renders the directory the clone would land in, or the reason
// the project directory cannot be resolved. Purely informational — the real
// check runs on accept (project.CloneTarget).
func (m Model) cloneTargetHint() string {
	dir, err := project.ProjectsDir()
	if err != nil {
		return err.Error()
	}
	name := strings.TrimSpace(m.cloneName.Text)
	if name == "" {
		name = "<name>"
	}
	// CompactPath bounds the line width: the shell drops a box wider than the
	// terminal, which a deep project directory would otherwise force.
	return project.CompactPath(filepath.Join(dir, name))
}

// cloneGhostStyle dims the directory name while it merely follows the URL
// (#1873): rendered like the focused URL field's live edit, the derived name
// used to read as typing into both fields at once. Faint marks it as a
// preview instead — the field itself gains real (undimmed) content only once
// cloneNameEdited is set, by hand or via startClone's fallback.
var cloneGhostStyle = lipgloss.NewStyle().Faint(true)

// renderClonePrompt (re)fills the shell for the current state; called on open
// and after every accepted key.
func (m *Model) renderClonePrompt() {
	// The shell drops a box wider than the terminal, so a long URL must never
	// grow the line: the value scrolls inside a window around the cursor.
	avail := m.width - 30
	if avail < 20 {
		avail = 20
	}
	url := m.cloneURL
	name := m.cloneName
	target := m.cloneTargetHint()
	running := m.cloneRunning
	errMsg := m.cloneErr
	edited := m.cloneNameEdited
	urlRow := func() string {
		if m.cloneField == cloneFieldURL && !running {
			return "> Repository URL : " + windowedInput(url.Text, url.Cur, avail)
		}
		return "  Repository URL : " + windowedInput(url.Text, url.Len(), avail)
	}
	nameRow := func() string {
		if m.cloneField == cloneFieldName && !running {
			return "> Directory name : " + windowedInput(name.Text, name.Cur, avail)
		}
		if !edited {
			return "  Directory name : " + cloneGhostStyle.Render(windowedPlain(name.Text, avail))
		}
		return "  Directory name : " + windowedInput(name.Text, name.Len(), avail)
	}
	m.shell.SetContent(ui.ModelContent{
		Heading: "Clone Repository",
		Body: func() string {
			b := &strings.Builder{}
			b.WriteString(urlRow())
			b.WriteString("\n")
			b.WriteString(nameRow())
			b.WriteString("\n\nClones into " + target)
			switch {
			case running:
				b.WriteString("\n\nCloning… esc cancels the dialog (the clone finishes)")
			case errMsg != "":
				b.WriteString("\nE: " + errMsg + "\n\ntab next field · enter clone · esc cancel")
			default:
				b.WriteString("\n\ntab next field · enter clone · esc cancel")
			}
			return b.String()
		},
	})
}

// windowedInput renders value (with the cursor at pos when it is the focused
// field) clipped to width runes, scrolled so the cursor stays visible; a
// clipped side is marked with "…".
func windowedInput(value string, pos, width int) string {
	w, wpos, clipL, clipR := windowRunes(value, pos, width)
	out := ui.CursorView(string(w), wpos)
	if clipL {
		out = "…" + out
	}
	if clipR {
		out += "…"
	}
	return out
}

// windowedPlain renders value clipped to width runes with no cursor mark —
// for a field that is not focused, e.g. the clone dialog's ghost name (#1873).
func windowedPlain(value string, width int) string {
	w, _, clipL, clipR := windowRunes(value, len([]rune(value)), width)
	out := string(w)
	if clipL {
		out = "…" + out
	}
	if clipR {
		out += "…"
	}
	return out
}

// windowRunes clips value to width runes around pos, scrolled so pos stays
// visible, and reports whether either side was clipped.
func windowRunes(value string, pos, width int) (window []rune, wpos int, clipL, clipR bool) {
	r := []rune(value)
	if pos > len(r) {
		pos = len(r)
	}
	if len(r) <= width {
		return r, pos, false, false
	}
	start := pos - width + 1
	if start < 0 {
		start = 0
	}
	end := start + width
	if end > len(r) {
		end, start = len(r), len(r)-width
	}
	return r[start:end], pos - start, start > 0, end < len(r)
}

// updateClonePrompt consumes every key while the dialog is open: tab switches
// fields, enter starts the clone, esc closes, everything else is line editing.
// While a clone runs the fields are frozen — only esc still answers.
func (m Model) updateClonePrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.Code == tea.KeyEscape {
		m.closeClonePrompt()
		return m, nil
	}
	if m.cloneRunning {
		return m, nil
	}

	switch msg.Code {
	case tea.KeyTab, tea.KeyDown:
		m.cloneField = (m.cloneField + 1) % 2
		m.renderClonePrompt()
		return m, nil
	case tea.KeyUp:
		m.cloneField = (m.cloneField + 1) % 2
		m.renderClonePrompt()
		return m, nil
	case tea.KeyEnter:
		return m.startClone()
	}

	switch m.cloneField {
	case cloneFieldURL:
		if handled, _ := m.cloneURL.Key(msg); handled {
			// The name follows the URL until it is edited by hand, so the
			// common case is type-URL-and-enter.
			if !m.cloneNameEdited {
				m.cloneName.Set(vcs.CloneName(m.cloneURL.Text))
			}
			m.renderClonePrompt()
		}
	case cloneFieldName:
		if handled, _ := m.cloneName.Key(msg); handled {
			m.cloneNameEdited = true
			m.renderClonePrompt()
		}
	}
	return m, nil
}

// pasteClonePrompt inserts a paste into the focused field at its cursor
// (#1873). A paste into the URL field re-derives the name exactly like
// typing there does; a paste into the name field marks it hand-edited, same
// as EditKey.
func (m *Model) pasteClonePrompt(text string) bool {
	if m.cloneRunning {
		return false
	}
	switch m.cloneField {
	case cloneFieldURL:
		if !m.cloneURL.Paste(text) {
			return false
		}
		if !m.cloneNameEdited {
			m.cloneName.Set(vcs.CloneName(m.cloneURL.Text))
		}
	case cloneFieldName:
		if !m.cloneName.Paste(text) {
			return false
		}
		m.cloneNameEdited = true
	}
	m.renderClonePrompt()
	return true
}

// startClone validates the two fields and launches the clone. Validation
// failures keep the dialog open and editable with the reason attached.
func (m Model) startClone() (tea.Model, tea.Cmd) {
	url := strings.TrimSpace(m.cloneURL.Text)
	if url == "" {
		m.cloneErr = "repository URL is empty — enter a URL to clone"
		m.cloneField = cloneFieldURL
		m.renderClonePrompt()
		return m, nil
	}
	name := strings.TrimSpace(m.cloneName.Text)
	if name == "" {
		name = vcs.CloneName(url)
	}
	dest, err := project.CloneTarget(name)
	if err != nil {
		m.cloneErr = err.Error()
		m.cloneField = cloneFieldName
		m.renderClonePrompt()
		return m, nil
	}
	m.cloneErr = ""
	m.cloneRunning = true
	m.renderClonePrompt()
	return m, vcs.CloneCmd(url, dest)
}

// finishClone answers a completed clone: a failure re-opens the fields with
// the git message, a success closes the dialog and switches the IDE to the
// fresh checkout through the regular switch transaction (unsaved-changes
// guard, history recording and all).
func (m Model) finishClone(msg vcs.CloneDoneMsg) (tea.Model, tea.Cmd) {
	if !m.cloneOpen {
		// The dialog was dismissed while the clone ran: report the outcome
		// instead of silently re-rooting the IDE under the user.
		if msg.Err != nil {
			m.host.Notify(host.Error, "clone failed: "+msg.Err.Error())
			return m, nil
		}
		m.host.Notify(host.Info, "cloned into "+project.CompactPath(msg.Dest))
		return m, nil
	}
	if msg.Err != nil {
		m.cloneRunning = false
		m.cloneErr = msg.Err.Error()
		m.cloneField = cloneFieldURL
		m.renderClonePrompt()
		return m, nil
	}
	// A clone driven by an ike:// link (#2396) continues the link's job: park
	// the payload before closeClonePrompt clears the marker, so the switch
	// below still opens the linked file and tool in the fresh checkout.
	if l := m.dlAfterClone; l != nil {
		m.dlPending = pendingFor(*l, msg.Dest)
	}
	m.closeClonePrompt()
	m.host.Notify(host.Info, "cloned into "+project.CompactPath(msg.Dest))
	return m, project.SwitchTo(msg.Dest)
}
