package settings

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/pathcomplete"
	"ike/internal/theme"
)

// venv_wizard.go is the Python environment wizard as a SubPanel (0420, #884)
// — the visible rebuild of the old hidden "n" state machine. Four steps: tool
// (uv / python -m venv, with availability and the uv scaffold disclosure),
// Python (uv versions / discovered interpreters), location (path input with
// clickable completion), run (spinner + elapsed + cancel, real command output
// on failure). The create itself and the EnvMsg → interpreter write-back →
// LSP restart seams are unchanged.

// Wizard step indices.
const (
	wStepTool = iota
	wStepPython
	wStepPath
	wStepRun
)

// WizardTickMsg drives the run step's spinner/elapsed display; the app routes
// it back into the settings panel.
type WizardTickMsg struct{ At time.Time }

// WizardDataMsg delivers an async wizard data fetch (the Python version
// list); the app routes it back into the settings panel.
type WizardDataMsg struct {
	Pys []string
	Err error
}

// wizTool is one row of the tool step.
type wizToolRow struct {
	id        string // "uv" | "venv"
	label     string
	available bool
	reason    string // why it is disabled
}

// venvWizard implements SubPanel (and CmdReceiver, SubPanelClicker,
// SubPanelWheeler).
type venvWizard struct {
	page *ToolchainPage
	host SubPanelHost

	step int

	tools    []wizToolRow
	toolPick int

	loadingPys bool
	pys        []string // display values; uv: "default"+versions, venv: paths
	pyPick     int
	pyOff      int
	tool       string
	python     string // resolved choice ("" = default)

	path     string
	suggest  pathSuggest
	pathNote string

	running bool
	started time.Time
	elapsed time.Duration
	spin    int
	cancel  context.CancelFunc
	done    *EnvMsg
}

// newVenvWizard probes tool availability and opens on the tool step — both
// tools always listed, unavailable ones disabled with the reason (never
// hidden), the uv side effect disclosed up front.
func newVenvWizard(page *ToolchainPage, host SubPanelHost) *venvWizard {
	w := &venvWizard{page: page, host: host, path: ".venv"}
	uvOK := page.look("uv") != ""
	pyOK := page.look("python3") != "" || page.look("python") != ""
	w.tools = []wizToolRow{
		{id: "uv", label: "uv (recommended) — fast, manages pyproject.toml + uv.lock", available: uvOK, reason: "uv not found on PATH"},
		{id: "venv", label: "python -m venv — stdlib, no extra tooling", available: pyOK, reason: "no python on PATH"},
	}
	if !uvOK {
		w.toolPick = 1
	}
	return w
}

// Title implements SubPanel: the breadcrumb carries the step counter.
func (w *venvWizard) Title() string {
	return fmt.Sprintf("New Python Environment (%d/4)", w.step+1)
}

// Capturing implements SubPanel: the wizard owns esc (it means back, one
// step, not abort-everything) and the path step is a text input.
func (w *venvWizard) Capturing() bool { return true }

// Buttons implements SubPanel.
func (w *venvWizard) Buttons() []Button {
	back := Button{Label: "Back", Do: func() tea.Cmd { w.back(); return nil }}
	cancel := Button{Label: "Cancel", Do: func() tea.Cmd { w.abort(); return nil }}
	switch w.step {
	case wStepPath:
		return []Button{back, {Label: "Create", Do: w.create}, cancel}
	case wStepRun:
		if w.running {
			return []Button{{Label: "Cancel run", Do: func() tea.Cmd { w.cancelRun(); return nil }}}
		}
		return []Button{{Label: "Close", Do: func() tea.Cmd { w.host.Pop(); return nil }}}
	default:
		return []Button{back, {Label: "Next", Do: w.next}, cancel}
	}
}

// back steps one level back; from the first step it closes the wizard.
func (w *venvWizard) back() {
	switch w.step {
	case wStepTool:
		w.host.Pop()
	case wStepPython:
		w.step = wStepTool
	case wStepPath:
		// Skip the Python step backwards too when it had nothing to choose.
		if len(w.pys) > 1 {
			w.step = wStepPython
		} else {
			w.step = wStepTool
		}
		w.suggest.clear()
	}
}

// abort closes the wizard outright (Cancel button).
func (w *venvWizard) abort() {
	if w.cancel != nil {
		w.cancel()
	}
	w.host.Pop()
}

// cancelRun kills the running create.
func (w *venvWizard) cancelRun() {
	if w.cancel != nil {
		w.cancel()
	}
}

// next advances from the tool or Python step.
func (w *venvWizard) next() tea.Cmd {
	switch w.step {
	case wStepTool:
		row := w.tools[w.toolPick]
		if !row.available {
			return nil
		}
		w.tool = row.id
		w.step = wStepPython
		w.pys, w.pyPick, w.pyOff = nil, 0, 0
		w.loadingPys = true
		return w.fetchPys()
	case wStepPython:
		if w.loadingPys {
			return nil
		}
		w.python = ""
		if len(w.pys) > 0 {
			pick := w.pys[w.pyPick]
			if !(w.tool == "uv" && pick == "default") {
				w.python = pick
			}
		}
		w.step = wStepPath
		w.refreshPathNote()
	}
	return nil
}

// fetchPys loads the Python choices off the UI goroutine (#884 — the old
// wizard ran `uv python list` synchronously in the keypress).
func (w *venvWizard) fetchPys() tea.Cmd {
	page := w.page
	tool := w.tool
	return func() tea.Msg {
		if tool == "uv" {
			return WizardDataMsg{Pys: append([]string{"default"}, uvVersionsAll(page.run("uv", "python", "list"))...)}
		}
		return WizardDataMsg{Pys: pythonCandidates(page.root, page.run, page.look, page.resolve, page.glob)}
	}
}

// ReceiveCmd implements CmdReceiver: async data, run results and the
// spinner ticks land here.
func (w *venvWizard) ReceiveCmd(msg tea.Msg) tea.Cmd {
	switch v := msg.(type) {
	case WizardDataMsg:
		if w.step != wStepPython || !w.loadingPys {
			return nil
		}
		w.loadingPys = false
		w.pys = v.Pys
		// Nothing (or exactly one obvious choice) to pick: skip ahead, like
		// the old wizard did.
		if len(w.pys) <= 1 {
			w.python = ""
			if w.tool == "venv" && len(w.pys) == 1 {
				w.python = w.pys[0]
			}
			w.step = wStepPath
			w.refreshPathNote()
		}
	case WizardTickMsg:
		if !w.running {
			return nil
		}
		w.elapsed = v.At.Sub(w.started)
		w.spin++
		return w.tick()
	case EnvMsg:
		if w.step != wStepRun {
			return nil
		}
		w.running = false
		w.cancel = nil
		done := v
		w.done = &done
	}
	return nil
}

// tick schedules the next spinner frame.
func (w *venvWizard) tick() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg { return WizardTickMsg{At: t} })
}

// create starts the run step.
func (w *venvWizard) create() tea.Cmd {
	target := strings.TrimSpace(w.path)
	if target == "" {
		target = ".venv"
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.running = true
	w.started = time.Now()
	w.elapsed = 0
	w.done = nil
	w.step = wStepRun
	w.suggest.clear()
	return tea.Batch(
		createEnvRun(ctx, w.page.root, envSpec{Tool: w.tool, Python: w.python, Target: target}, execRunCtx, w.page.look),
		w.tick(),
	)
}

// Update implements SubPanel.
func (w *venvWizard) Update(key tea.KeyPressMsg) tea.Cmd {
	switch w.step {
	case wStepTool, wStepPython:
		return w.updatePick(key)
	case wStepPath:
		return w.updatePath(key)
	case wStepRun:
		switch key.String() {
		case "esc":
			if w.running {
				w.cancelRun()
				return nil
			}
			w.host.Pop()
		case "enter":
			if !w.running {
				w.host.Pop()
			}
		}
	}
	return nil
}

// updatePick handles the tool and Python list steps.
func (w *venvWizard) updatePick(key tea.KeyPressMsg) tea.Cmd {
	n := len(w.tools)
	pick := &w.toolPick
	if w.step == wStepPython {
		n, pick = len(w.pys), &w.pyPick
	}
	switch key.String() {
	case "esc":
		w.back()
	case "up", "k":
		if *pick > 0 {
			*pick--
		}
	case "down", "j":
		if *pick < n-1 {
			*pick++
		}
	case "enter":
		return w.next()
	}
	return nil
}

// updatePath handles the location input.
func (w *venvWizard) updatePath(key tea.KeyPressMsg) tea.Cmd {
	switch key.Code {
	case tea.KeyEscape:
		w.back()
	case tea.KeyEnter:
		return w.create()
	case tea.KeyTab:
		w.path = w.suggest.complete(w.path)
		w.refreshPathNote()
	case tea.KeyBackspace:
		if r := []rune(w.path); len(r) > 0 {
			w.path = string(r[:len(r)-1])
			w.suggest.refresh(w.path)
			w.refreshPathNote()
		}
	default:
		if key.Text != "" {
			w.path += key.Text
			w.suggest.refresh(w.path)
			w.refreshPathNote()
		}
	}
	return nil
}

// refreshPathNote updates the live target validation line.
func (w *venvWizard) refreshPathNote() {
	target := strings.TrimSpace(w.path)
	if target == "" {
		target = ".venv"
	}
	p := pathcomplete.Expand(target)
	if !filepath.IsAbs(p) {
		p = filepath.Join(w.page.root, p)
	}
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		w.pathNote = "already exists — the tool will reuse or refresh it"
		return
	}
	w.pathNote = ""
}

// Click implements SubPanelClicker: rows select in the pick steps (a press
// on the selection advances), suggestion rows complete the path input.
func (w *venvWizard) Click(_, y int) tea.Cmd {
	switch w.step {
	case wStepTool:
		// Rows render from line 1 (line 0 is the prompt).
		if idx := y - 1; idx >= 0 && idx < len(w.tools) {
			if idx == w.toolPick {
				return w.next()
			}
			w.toolPick = idx
		}
	case wStepPython:
		if idx := y - 1 + w.pyOff; idx >= 0 && idx < len(w.pys) {
			if idx == w.pyPick {
				return w.next()
			}
			w.pyPick = idx
		}
	case wStepPath:
		// Line 0 prompt, line 1 input, line 2 note, suggestions from line 3.
		if idx := y - 3; idx >= 0 {
			lines := w.suggest.lines()
			if idx < len(lines) {
				w.path = strings.TrimSpace(lines[idx])
				w.suggest.refresh(w.path)
				w.refreshPathNote()
			}
		}
	}
	return nil
}

// Wheel implements SubPanelWheeler: the Python list scrolls its highlight.
func (w *venvWizard) Wheel(delta int) {
	if w.step == wStepPython && len(w.pys) > 0 {
		w.pyPick = clamp(w.pyPick+delta, 0, len(w.pys)-1)
	}
}

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// View implements SubPanel.
func (w *venvWizard) View(width, height int) string {
	pal := w.theme()
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	sel := lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
	dim := lipgloss.NewStyle().Foreground(pal.Secondary).Faint(true)
	clip := lipgloss.NewStyle().MaxWidth(width)
	var lines []string
	add := func(s string) { lines = append(lines, clip.Render(s)) }

	switch w.step {
	case wStepTool:
		add(sec.Render(" Which tool creates the environment?"))
		for i, row := range w.tools {
			marker := "○ "
			if i == w.toolPick {
				marker = "● "
			}
			line := " " + marker + row.label
			switch {
			case !row.available:
				add(dim.Render(line + "  (" + row.reason + ")"))
			case i == w.toolPick:
				add(sel.Render(line))
			default:
				add(" " + marker + row.label)
			}
		}
		if w.tools[w.toolPick].id == "uv" && !fileExists(filepath.Join(w.page.root, "pyproject.toml")) {
			add("")
			add(sec.Render(" uv will also create pyproject.toml + uv.lock in the project root"))
		}
	case wStepPython:
		add(sec.Render(" Which Python?"))
		if w.loadingPys {
			add(sec.Render(" loading…"))
			break
		}
		// Windowed with the highlight followed (#884 — the old picker could
		// walk off-screen).
		w.pyOff = follow(w.pyOff, w.pyPick, w.pyPick, len(w.pys), height-1)
		end := w.pyOff + height - 1
		if end > len(w.pys) {
			end = len(w.pys)
		}
		for i := w.pyOff; i < end; i++ {
			line := " " + w.describePy(w.pys[i])
			if i == w.pyPick {
				add(sel.Render(line))
			} else {
				add(line)
			}
		}
	case wStepPath:
		add(sec.Render(" Where should the environment live? (relative to the project root)"))
		add(" " + w.path + "▌")
		if w.pathNote != "" {
			add(sec.Render(" ⚠ " + w.pathNote))
		} else {
			add("")
		}
		for _, s := range w.suggest.lines() {
			add(sec.Render(s))
		}
	case wStepRun:
		if w.running {
			frame := spinFrames[w.spin%len(spinFrames)]
			add(" " + frame + " creating " + w.runLabel() + "…  " + w.elapsed.Truncate(time.Second).String())
			add("")
			add(sec.Render(" uv may download a Python toolchain — this can take a while"))
			break
		}
		if w.done == nil {
			add(" …")
			break
		}
		if w.done.Err != nil {
			add(lipgloss.NewStyle().Foreground(pal.Error).Render(" ✗ " + w.done.Err.Error()))
			for _, l := range strings.Split(w.done.Output, "\n") {
				if l != "" {
					add(dim.Render(" " + l))
				}
			}
			break
		}
		add(lipgloss.NewStyle().Foreground(pal.Info).Render(" ✓ " + w.done.Label))
		add("")
		add(sec.Render(" registered as the project interpreter — LSP restarted"))
	}
	return strings.Join(lines, "\n")
}

// describePy renders one Python choice row.
func (w *venvWizard) describePy(p string) string {
	if w.tool == "uv" {
		if p == "default" {
			return "default (uv picks)"
		}
		return "python " + p
	}
	if prov := w.page.provenance(p); prov != "" {
		return p + "  [" + prov + "]"
	}
	return p
}

// runLabel phrases the running command for the spinner line.
func (w *venvWizard) runLabel() string {
	label := w.tool
	if w.python != "" {
		label += " (python " + w.python + ")"
	}
	return label
}

func (w *venvWizard) theme() *theme.Palette {
	if w.page.pal != nil {
		return w.page.pal
	}
	return theme.DefaultPalette()
}

// OpenPythonEnvWizard opens the panel on the Toolchain page with the venv
// wizard pushed (#884) — the python.newEnvironment palette entry point.
// Reports whether a toolchain page was found.
func (m *Model) OpenPythonEnvWizard() bool {
	m.Open()
	for i, page := range m.pages {
		if tp, ok := page.Custom.(*ToolchainPage); ok {
			m.cat, m.focus = i, formColumn
			tp.pushWizard()
			return true
		}
	}
	return false
}
