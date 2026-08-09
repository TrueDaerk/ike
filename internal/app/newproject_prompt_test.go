package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/lang"
	"ike/internal/project"
)

// wizardScaffolder is the fake lang.ProjectScaffolder the wizard tests drive;
// it drops a marker file so the end-to-end path is observable.
type wizardScaffolder struct {
	opts []lang.ProjectOption
	err  error
}

func (*wizardScaffolder) Detect(string) (map[string]any, bool)   { return nil, false }
func (s *wizardScaffolder) ProjectOptions() []lang.ProjectOption { return s.opts }
func (s *wizardScaffolder) ScaffoldProject(root, option string) error {
	if s.err != nil {
		return s.err
	}
	return os.WriteFile(filepath.Join(root, "marker-"+option), []byte("ok"), 0o644)
}

// openNewProject registers a fake scaffolding language, opens the wizard and
// moves the type selection onto the fake — the registry is global, so other
// languages may precede it in the sorted list.
func openNewProject(t *testing.T, m Model, sc *wizardScaffolder) Model {
	t.Helper()
	lang.Register(lang.Language{ID: "wizlang", Toolchain: sc})
	tm, cmd := m.Update(project.OpenNewProjectMsg{})
	m = drainCmd(tm.(Model), cmd)
	if !m.newProjectPromptOpen() {
		t.Fatal("project.new must open the wizard")
	}
	for i, l := range m.newProj.langs {
		if l.ID == "wizlang" {
			m.newProj.langPick = i
			return m
		}
	}
	t.Fatal("the fake language must appear as a project type")
	return m
}

// TestNewProjectCommandOpensWizard drives the real command path (palette /
// menu → registry → host dispatch).
func TestNewProjectCommandOpensWizard(t *testing.T) {
	lang.Register(lang.Language{ID: "wizlang", Toolchain: &wizardScaffolder{
		opts: []lang.ProjectOption{{ID: "basic", Label: "Basic", Available: true}},
	}})
	m := newSized()
	cloneProjectsDir(t)
	m = drainCmd(m, m.RunCommand("project.new"))
	if !m.newProjectPromptOpen() {
		t.Fatal("running project.new must open the wizard")
	}
	if !strings.Contains(plainView(m), "New Project") {
		t.Fatalf("wizard is not rendered:\n%s", plainView(m))
	}
}

// TestNewProjectWizardEndToEnd walks type → toolchain → name, runs the
// scaffold command and checks the finished project re-roots the IDE.
func TestNewProjectWizardEndToEnd(t *testing.T) {
	base := newSized()
	dir := cloneProjectsDir(t)
	sc := &wizardScaffolder{opts: []lang.ProjectOption{{ID: "basic", Label: "Basic setup", Available: true}}}
	m := openNewProject(t, base, sc)

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.newProj.step != newProjStepTool {
		t.Fatalf("step = %d, want toolchain", m.newProj.step)
	}
	if !strings.Contains(plainView(m), "Basic setup") {
		t.Fatalf("toolchain options are not rendered:\n%s", plainView(m))
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.newProj.step != newProjStepName {
		t.Fatalf("step = %d, want name", m.newProj.step)
	}
	m = typeInto(m, "proj1")

	tm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tm.(Model)
	if !m.newProj.running || cmd == nil {
		t.Fatal("enter must start the scaffold")
	}
	msg, ok := cmd().(newProjectDoneMsg)
	if !ok {
		t.Fatal("the create command must resolve to a newProjectDoneMsg")
	}
	if msg.Err != nil {
		t.Fatalf("create failed: %v", msg.Err)
	}
	dest := filepath.Join(dir, "proj1")
	if msg.Dest != dest {
		t.Fatalf("Dest = %q, want %q", msg.Dest, dest)
	}
	if _, err := os.Stat(filepath.Join(dest, "marker-basic")); err != nil {
		t.Fatalf("scaffolder did not run in the target: %v", err)
	}

	tm, cmd = m.Update(msg)
	m = tm.(Model)
	if m.newProjectPromptOpen() {
		t.Fatal("a successful create must close the wizard")
	}
	if !hasSwitchTo(cmd, dest) {
		t.Fatalf("create must switch to %q", dest)
	}
}

// TestNewProjectRejectsBadNames keeps validation failures in the dialog.
func TestNewProjectRejectsBadNames(t *testing.T) {
	base := newSized()
	cloneProjectsDir(t)
	sc := &wizardScaffolder{opts: []lang.ProjectOption{{ID: "basic", Label: "Basic", Available: true}}}
	m := openNewProject(t, base, sc)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})

	tm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drainCmd(tm.(Model), cmd)
	if m.newProj.running || m.newProj.err == "" {
		t.Fatalf("an empty name must stay in the dialog with a reason, err=%q", m.newProj.err)
	}

	m = typeInto(m, "a/b")
	tm, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drainCmd(tm.(Model), cmd)
	if m.newProj.running || !strings.Contains(m.newProj.err, "separator") {
		t.Fatalf("a path separator must be refused, err=%q", m.newProj.err)
	}
}

// TestNewProjectUnavailableOptionBlocked keeps a disabled toolchain option
// selectable but not acceptable — the reason shows instead.
func TestNewProjectUnavailableOptionBlocked(t *testing.T) {
	base := newSized()
	cloneProjectsDir(t)
	sc := &wizardScaffolder{opts: []lang.ProjectOption{
		{ID: "fancy", Label: "Fancy", Available: false, Reason: "fancy not found on PATH"},
		{ID: "basic", Label: "Basic", Available: true},
	}}
	m := openNewProject(t, base, sc)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.newProj.optPick != 1 {
		t.Fatalf("selection must rest on the first available option, got %d", m.newProj.optPick)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyUp})
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.newProj.step != newProjStepTool || m.newProj.err != "fancy not found on PATH" {
		t.Fatalf("an unavailable option must refuse with the reason, step=%d err=%q", m.newProj.step, m.newProj.err)
	}
}

// TestNewProjectScaffoldFailureCleansUpAndStays keeps a failed scaffold in
// the dialog and removes the partial directory so the name can be reused.
func TestNewProjectScaffoldFailureCleansUpAndStays(t *testing.T) {
	base := newSized()
	dir := cloneProjectsDir(t)
	sc := &wizardScaffolder{
		opts: []lang.ProjectOption{{ID: "basic", Label: "Basic", Available: true}},
		err:  errors.New("tool exploded"),
	}
	m := openNewProject(t, base, sc)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m = typeInto(m, "broken")

	tm, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tm.(Model)
	msg, ok := cmd().(newProjectDoneMsg)
	if !ok || msg.Err == nil {
		t.Fatalf("the failed scaffold must report its error, got %#v", msg)
	}
	if _, err := os.Stat(filepath.Join(dir, "broken")); !os.IsNotExist(err) {
		t.Fatal("a failed scaffold must remove the partial directory")
	}

	tm, _ = m.Update(msg)
	m = tm.(Model)
	if !m.newProjectPromptOpen() || m.newProj.running {
		t.Fatal("a failed scaffold must leave the wizard open and editable")
	}
	if !strings.Contains(m.newProj.err, "tool exploded") {
		t.Fatalf("err = %q, want the scaffold reason", m.newProj.err)
	}
}

// TestNewProjectEscWalksBack guards the esc semantics: one step back, close
// from the first step.
func TestNewProjectEscWalksBack(t *testing.T) {
	base := newSized()
	cloneProjectsDir(t)
	sc := &wizardScaffolder{opts: []lang.ProjectOption{{ID: "basic", Label: "Basic", Available: true}}}
	m := openNewProject(t, base, sc)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.newProjectPromptOpen() || m.newProj.step != newProjStepType {
		t.Fatal("esc must step back to the type step")
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.newProjectPromptOpen() {
		t.Fatal("esc on the first step must close the wizard")
	}
}
