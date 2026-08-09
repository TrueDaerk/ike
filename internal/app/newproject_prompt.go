package app

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/project"
	"ike/internal/ui"
)

// newproject_prompt.go drives "New Project" (#1718): a three-step shell
// dialog — project type (the languages offering scaffolding, lang.
// ProjectLanguages), toolchain (the language's lang.ProjectOptions; for
// Python the guided venv choice between uv and pip/venv), directory name.
// The scaffold itself is one async tea.Cmd like the clone (#1349), and a
// finished project opens through the regular switch transaction.

// Wizard step indices; esc walks them backwards.
const (
	newProjStepType = iota
	newProjStepTool
	newProjStepName
)

// newProjState is the open wizard; nil when it is closed.
type newProjState struct {
	step     int
	langs    []lang.Language
	langPick int
	opts     []lang.ProjectOption
	optPick  int
	name     string
	namePos  int
	// running freezes the wizard while the scaffold subprocesses run; err is
	// the validation or scaffold message shown under the current step.
	running bool
	err     string
}

// newProjectDoneMsg reports the finished async create.
type newProjectDoneMsg struct {
	Dest string
	Err  error
}

// startNewProjectPrompt opens the wizard on the type step, or explains why it
// cannot (no registered language offers scaffolding — a plugin-less build).
func (m *Model) startNewProjectPrompt() {
	langs := lang.ProjectLanguages()
	if len(langs) == 0 {
		m.host.Notify(host.Error, "no installed language offers project scaffolding")
		return
	}
	m.newProj = &newProjState{langs: langs}
	m.renderNewProjectPrompt()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// newProjectPromptOpen reports whether the shell currently shows the wizard.
func (m Model) newProjectPromptOpen() bool { return m.newProj != nil && m.shell.IsOpen() }

// closeNewProjectPrompt clears the wizard state and the shell.
func (m *Model) closeNewProjectPrompt() {
	m.newProj = nil
	m.shell.Close()
}

// projTypeLabel renders a language id as the type row ("python" → "Python").
func projTypeLabel(id string) string {
	switch id {
	case "php":
		return "PHP"
	default:
		if id == "" {
			return id
		}
		return strings.ToUpper(id[:1]) + id[1:]
	}
}

// newProjTargetHint renders the directory the project would land in, or the
// reason the project directory cannot be resolved. Purely informational — the
// real check runs on accept (project.Target).
func (m Model) newProjTargetHint() string {
	dir, err := project.ProjectsDir()
	if err != nil {
		return err.Error()
	}
	name := strings.TrimSpace(m.newProj.name)
	if name == "" {
		name = "<name>"
	}
	return project.CompactPath(filepath.Join(dir, name))
}

// renderNewProjectPrompt (re)fills the shell for the current state; called on
// open and after every accepted key.
func (m *Model) renderNewProjectPrompt() {
	s := m.newProj
	avail := m.width - 30
	if avail < 20 {
		avail = 20
	}
	var lines []string
	add := func(l string) { lines = append(lines, l) }
	switch s.step {
	case newProjStepType:
		add("Project type")
		add("")
		for i, l := range s.langs {
			marker := "  ○ "
			if i == s.langPick {
				marker = "> ● "
			}
			add(marker + projTypeLabel(l.ID))
		}
		add("")
		add("↑↓ select · enter next · esc cancel")
	case newProjStepTool:
		add("Toolchain for " + projTypeLabel(s.langs[s.langPick].ID))
		add("")
		for i, o := range s.opts {
			marker := "  ○ "
			if i == s.optPick {
				marker = "> ● "
			}
			line := marker + o.Label
			if !o.Available {
				line += "  (" + o.Reason + ")"
			} else if o.Detail != "" {
				line += "  [" + project.CompactPath(o.Detail) + "]"
			}
			add(line)
		}
		add("")
		add("↑↓ select · enter next · esc back")
	case newProjStepName:
		add("Directory name: " + windowedInput(s.name, s.namePos, avail))
		add("")
		add("Creates " + m.newProjTargetHint())
		add("")
		if s.running {
			add("Creating… esc dismisses the dialog (the scaffold finishes)")
		} else {
			add("enter create · esc back")
		}
	}
	if s.err != "" && !s.running {
		lines = append(lines, "", "E: "+s.err)
	}
	body := strings.Join(lines, "\n")
	m.shell.SetContent(ui.ModelContent{
		Heading: "New Project",
		Body:    func() string { return body },
	})
}

// updateNewProjectPrompt consumes every key while the wizard is open: up/down
// move the lists, enter advances, esc steps back (from the first step: closes).
// While the scaffold runs the wizard is frozen — only esc still answers, and
// it only dismisses the dialog; the scaffold finishes and toasts.
func (m Model) updateNewProjectPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.newProj
	if msg.Code == tea.KeyEscape {
		if s.running || s.step == newProjStepType {
			m.closeNewProjectPrompt()
			return m, nil
		}
		s.step--
		s.err = ""
		m.renderNewProjectPrompt()
		return m, nil
	}
	if s.running {
		return m, nil
	}

	switch s.step {
	case newProjStepType, newProjStepTool:
		n, pick := len(s.langs), &s.langPick
		if s.step == newProjStepTool {
			n, pick = len(s.opts), &s.optPick
		}
		switch msg.Code {
		case tea.KeyUp:
			*pick = (*pick + n - 1) % n
			m.renderNewProjectPrompt()
		case tea.KeyDown, tea.KeyTab:
			*pick = (*pick + 1) % n
			m.renderNewProjectPrompt()
		case tea.KeyEnter:
			return m.advanceNewProject()
		}
	case newProjStepName:
		if msg.Code == tea.KeyEnter {
			return m.startNewProjectCreate()
		}
		if out, pos, handled, _ := ui.EditKey(msg, s.name, s.namePos); handled {
			s.name, s.namePos = out, pos
			m.renderNewProjectPrompt()
		}
	}
	return m, nil
}

// advanceNewProject moves past a list step.
func (m Model) advanceNewProject() (tea.Model, tea.Cmd) {
	s := m.newProj
	switch s.step {
	case newProjStepType:
		s.opts = lang.ProjectOptions(s.langs[s.langPick].ID)
		if len(s.opts) == 0 {
			s.err = projTypeLabel(s.langs[s.langPick].ID) + " offers no toolchain options"
			m.renderNewProjectPrompt()
			return m, nil
		}
		s.optPick = 0
		// Rest the selection on the first available option.
		for i, o := range s.opts {
			if o.Available {
				s.optPick = i
				break
			}
		}
		s.step = newProjStepTool
	case newProjStepTool:
		o := s.opts[s.optPick]
		if !o.Available {
			s.err = o.Reason
			m.renderNewProjectPrompt()
			return m, nil
		}
		s.step = newProjStepName
	}
	s.err = ""
	m.renderNewProjectPrompt()
	return m, nil
}

// startNewProjectCreate validates the name and launches the scaffold.
// Validation failures keep the wizard open and editable with the reason.
func (m Model) startNewProjectCreate() (tea.Model, tea.Cmd) {
	s := m.newProj
	dest, err := project.Target(s.name)
	if err != nil {
		s.err = err.Error()
		m.renderNewProjectPrompt()
		return m, nil
	}
	s.err = ""
	s.running = true
	m.renderNewProjectPrompt()
	return m, createProjectCmd(s.langs[s.langPick].ID, s.opts[s.optPick].ID, dest)
}

// createProjectCmd creates dest and runs the language's scaffolder in one
// async tea.Cmd, so the UI keeps rendering while subprocesses (uv init,
// python -m venv, go mod init) run.
func createProjectCmd(langID, option, dest string) tea.Cmd {
	return func() tea.Msg {
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return newProjectDoneMsg{Dest: dest, Err: err}
		}
		if err := lang.ScaffoldProject(langID, dest, option); err != nil {
			// dest did not exist before this command (project.Target refused
			// an existing path), so removing the partial scaffold is safe —
			// and a retry can reuse the name.
			os.RemoveAll(dest)
			return newProjectDoneMsg{Dest: dest, Err: err}
		}
		return newProjectDoneMsg{Dest: dest}
	}
}

// finishNewProject answers a completed scaffold: a failure re-opens the name
// step with the reason, a success closes the wizard and opens the fresh
// project through the regular switch transaction (unsaved-changes guard,
// history recording and all).
func (m Model) finishNewProject(msg newProjectDoneMsg) (tea.Model, tea.Cmd) {
	if !m.newProjectPromptOpen() {
		// The dialog was dismissed while the scaffold ran: report the outcome
		// instead of silently re-rooting the IDE under the user.
		if msg.Err != nil {
			m.host.Notify(host.Error, "new project failed: "+msg.Err.Error())
			return m, nil
		}
		m.host.Notify(host.Info, "created "+project.CompactPath(msg.Dest))
		return m, nil
	}
	s := m.newProj
	if msg.Err != nil {
		s.running = false
		s.err = msg.Err.Error()
		m.renderNewProjectPrompt()
		return m, nil
	}
	m.closeNewProjectPrompt()
	m.host.Notify(host.Info, "created "+project.CompactPath(msg.Dest))
	return m, project.SwitchTo(msg.Dest)
}
