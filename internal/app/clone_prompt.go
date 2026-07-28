package app

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

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
	m.cloneURL, m.cloneURLPos = "", 0
	m.cloneName, m.cloneNamePos = "", 0
	m.cloneNameEdited = false
	m.cloneField = cloneFieldURL
	m.cloneErr = ""
	m.renderClonePrompt()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// clonePromptOpen reports whether the shell currently shows the clone dialog.
func (m Model) clonePromptOpen() bool { return m.cloneOpen && m.shell.IsOpen() }

// closeClonePrompt clears the dialog state and the shell.
func (m *Model) closeClonePrompt() {
	m.cloneOpen = false
	m.cloneRunning = false
	m.cloneURL, m.cloneURLPos = "", 0
	m.cloneName, m.cloneNamePos = "", 0
	m.cloneNameEdited = false
	m.cloneField = cloneFieldURL
	m.cloneErr = ""
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
	name := strings.TrimSpace(m.cloneName)
	if name == "" {
		name = "<name>"
	}
	// CompactPath bounds the line width: the shell drops a box wider than the
	// terminal, which a deep project directory would otherwise force.
	return project.CompactPath(filepath.Join(dir, name))
}

// renderClonePrompt (re)fills the shell for the current state; called on open
// and after every accepted key.
func (m *Model) renderClonePrompt() {
	// The shell drops a box wider than the terminal, so a long URL must never
	// grow the line: the value scrolls inside a window around the cursor.
	avail := m.width - 30
	if avail < 20 {
		avail = 20
	}
	field := func(idx int, label, value string, pos int) string {
		marker := "  "
		if m.cloneField == idx && !m.cloneRunning {
			marker = "> "
			return marker + label + ": " + windowedInput(value, pos, avail)
		}
		return marker + label + ": " + windowedInput(value, len([]rune(value)), avail)
	}
	url := m.cloneURL
	name := m.cloneName
	pos, npos := m.cloneURLPos, m.cloneNamePos
	target := m.cloneTargetHint()
	running := m.cloneRunning
	errMsg := m.cloneErr
	m.shell.SetContent(ui.ModelContent{
		Heading: "Clone Repository",
		Body: func() string {
			b := &strings.Builder{}
			b.WriteString(field(cloneFieldURL, "Repository URL ", url, pos))
			b.WriteString("\n")
			b.WriteString(field(cloneFieldName, "Directory name ", name, npos))
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
	r := []rune(value)
	if pos > len(r) {
		pos = len(r)
	}
	if len(r) <= width {
		return ui.CursorView(value, pos)
	}
	start := pos - width + 1
	if start < 0 {
		start = 0
	}
	end := start + width
	if end > len(r) {
		end, start = len(r), len(r)-width
	}
	out := ui.CursorView(string(r[start:end]), pos-start)
	if start > 0 {
		out = "…" + out
	}
	if end < len(r) {
		out += "…"
	}
	return out
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
		if out, pos, handled, _ := ui.EditKey(msg, m.cloneURL, m.cloneURLPos); handled {
			m.cloneURL, m.cloneURLPos = out, pos
			// The name follows the URL until it is edited by hand, so the
			// common case is type-URL-and-enter.
			if !m.cloneNameEdited {
				m.cloneName = vcs.CloneName(m.cloneURL)
				m.cloneNamePos = len([]rune(m.cloneName))
			}
			m.renderClonePrompt()
		}
	case cloneFieldName:
		if out, pos, handled, _ := ui.EditKey(msg, m.cloneName, m.cloneNamePos); handled {
			m.cloneName, m.cloneNamePos = out, pos
			m.cloneNameEdited = true
			m.renderClonePrompt()
		}
	}
	return m, nil
}

// startClone validates the two fields and launches the clone. Validation
// failures keep the dialog open and editable with the reason attached.
func (m Model) startClone() (tea.Model, tea.Cmd) {
	url := strings.TrimSpace(m.cloneURL)
	if url == "" {
		m.cloneErr = "repository URL is empty — enter a URL to clone"
		m.cloneField = cloneFieldURL
		m.renderClonePrompt()
		return m, nil
	}
	name := strings.TrimSpace(m.cloneName)
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
	m.closeClonePrompt()
	m.host.Notify(host.Info, "cloned into "+project.CompactPath(msg.Dest))
	return m, project.SwitchTo(msg.Dest)
}
