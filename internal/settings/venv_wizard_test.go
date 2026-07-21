package settings

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
)

// wizardAt builds a page+wizard pair with both tools available.
func wizardAt(t *testing.T) (*ToolchainPage, *venvWizard, *stubHost) {
	t.Helper()
	py := mkInterp(t, t.TempDir())
	f := &fakeEnv{binaries: map[string]string{"uv": "/bin/uv", "python3": py}}
	f.onRun = func(name string, args ...string) string { return "" }
	p := pythonPage(t, f)
	h := &stubHost{}
	p.SetSubPanelHost(h)
	p.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	w, ok := h.top().(*venvWizard)
	if !ok {
		t.Fatal("n must push the wizard")
	}
	return p, w, h
}

// TestWizardRunStepResultAndOutput: an EnvMsg failure shows the error plus
// the captured command output; the spinner stops.
func TestWizardRunStepResultAndOutput(t *testing.T) {
	_, w, h := wizardAt(t)
	w.tool, w.step = "venv", wStepPath
	if cmd := w.create(); cmd == nil {
		t.Fatal("create must return commands")
	}
	if !w.running || w.step != wStepRun {
		t.Fatalf("run state = running %v step %d", w.running, w.step)
	}
	// A tick while running re-arms itself.
	if c := w.ReceiveCmd(WizardTickMsg{At: time.Now()}); c == nil {
		t.Fatal("tick while running must chain the next tick")
	}
	w.ReceiveCmd(EnvMsg{LangID: "python", Err: fmt.Errorf("boom"), Output: "$ uv venv\nsome stderr detail"})
	if w.running || w.done == nil {
		t.Fatal("EnvMsg must finish the run")
	}
	if c := w.ReceiveCmd(WizardTickMsg{At: time.Now()}); c != nil {
		t.Fatal("ticks must stop after the result")
	}
	v := w.View(70, 12)
	if !strings.Contains(v, "boom") || !strings.Contains(v, "some stderr detail") {
		t.Fatalf("failure view must show the real output:\n%s", v)
	}
	// Enter closes the finished wizard.
	w.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if h.top() != nil {
		t.Fatal("enter on the result must close the wizard")
	}
}

// TestWizardDisclosesUvScaffold: with no pyproject.toml the tool step names
// the side effect.
func TestWizardDisclosesUvScaffold(t *testing.T) {
	_, w, _ := wizardAt(t)
	v := w.View(80, 12)
	if !strings.Contains(v, "pyproject.toml + uv.lock") {
		t.Fatalf("tool step must disclose the uv scaffold:\n%s", v)
	}
}

// TestWizardUnavailableToolDisabled: a missing tool renders disabled with its
// reason and cannot be advanced onto.
func TestWizardUnavailableToolDisabled(t *testing.T) {
	f := &fakeEnv{binaries: map[string]string{"python3": mkInterp(t, t.TempDir())}}
	p := pythonPage(t, f)
	h := &stubHost{}
	p.SetSubPanelHost(h)
	p.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	w := h.top().(*venvWizard)
	if w.tools[0].available {
		t.Fatal("uv must be unavailable")
	}
	if w.toolPick != 1 {
		t.Fatalf("pick must start on the available tool, got %d", w.toolPick)
	}
	w.toolPick = 0
	if cmd := w.next(); cmd != nil || w.step != wStepTool {
		t.Fatal("an unavailable tool must not advance")
	}
	if v := w.View(110, 12); !strings.Contains(v, "uv not found on PATH") {
		t.Fatalf("disabled row must carry its reason:\n%s", v)
	}
}

// TestToolchainActionRowPushesWizard: the visible "+ New environment…" row
// opens the wizard on enter.
func TestToolchainActionRowPushesWizard(t *testing.T) {
	f := &fakeEnv{binaries: map[string]string{"uv": "/bin/uv"}}
	p := pythonPage(t, f)
	h := &stubHost{}
	p.SetSubPanelHost(h)
	rows := p.rows()
	found := false
	for i, r := range rows {
		if r.action == "newenv" {
			p.sel, found = i, true
		}
	}
	if !found {
		t.Fatal("the toolchain list must carry the new-environment action row")
	}
	if v := p.View(120, 30); !strings.Contains(v, "+ New environment…") {
		t.Fatalf("action row must render:\n%s", v)
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, ok := h.top().(*venvWizard); !ok {
		t.Fatal("enter on the action row must push the wizard")
	}
}

// TestOpenPythonEnvWizard: the palette entry point opens settings on the
// toolchain page with the wizard pushed.
func TestOpenPythonEnvWizard(t *testing.T) {
	restoreConfig(t)
	tp := NewToolchainPage(config.Options{}, t.TempDir(), nil)
	tp.look = func(name string) string {
		if name == "uv" {
			return "/bin/uv"
		}
		return ""
	}
	tp.run = func(string, ...string) string { return "" }
	m := New(append(BasePages([]string{"default"}), Page{Title: "Toolchain", Custom: tp}), testOpts(t))
	m.SetSize(90, 30)
	if !m.OpenPythonEnvWizard() {
		t.Fatal("the toolchain page must be found")
	}
	if !m.SubOpen() {
		t.Fatal("the wizard must be pushed")
	}
	if _, ok := m.topSub().(*venvWizard); !ok {
		t.Fatalf("top = %T, want the venv wizard", m.topSub())
	}
	if !strings.Contains(m.View(), "New Python Environment (1/4)") {
		t.Fatal("breadcrumb must show the wizard step")
	}
}
