package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/nbview"
	"ike/internal/palette"
	"ike/internal/pane"
	"ike/internal/run"
	"ike/internal/terminal"
	"ike/internal/watch"
)

// run_notebook_test.go covers running a notebook from the viewer (#2682):
// run.file and the pane's r key launch nbconvert's execute-in-place under
// the resolved Python interpreter, rerun repeats it, the watcher's reload
// covers an atomic replace, and a run that dies for want of jupyter gets its
// install hint.

// nbTestToolchain stands in for the Python plugin (not linked into this test
// binary): the same argv shape the real provider synthesizes for a notebook
// spec, so the app-side assertions read the real command line.
type nbTestToolchain struct{}

func (nbTestToolchain) Detect(string) (map[string]any, bool) { return nil, false }
func (nbTestToolchain) RunCommand(_ string, spec lang.RunSpec, interpreter string) ([]string, bool) {
	if interpreter == "" {
		interpreter = "python3"
	}
	if spec.Notebook {
		return []string{interpreter, "-m", "jupyter", "nbconvert", "--to", "notebook", "--execute", "--inplace", spec.File}, true
	}
	return []string{interpreter, spec.File}, true
}

// toastTexts returns the texts of the notifications the model has shown so
// far, newest last: an Update turns host notifications into toasts.
func toastTexts(m Model) []string {
	var out []string
	for _, t := range m.toasts {
		out = append(out, t.text)
	}
	return out
}

// hasToast reports whether any shown toast contains want.
func hasToast(m Model, want string) bool {
	for _, t := range toastTexts(m) {
		if strings.Contains(t, want) {
			return true
		}
	}
	return false
}

// notebookRunApp builds an app with the stub python toolchain, an explicit
// interpreter pointing at script (a shell script standing in for python) and
// the test notebook open and focused in its viewer.
func notebookRunApp(t *testing.T, script string) (Model, string) {
	t.Helper()
	lang.Register(lang.Language{ID: "python", Extensions: []string{"py"}, Toolchain: nbTestToolchain{}})
	interp := filepath.Join(t.TempDir(), "python")
	if err := os.WriteFile(interp, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := hexApp(t, host.MapConfig{"lang.python.interpreter": interp, "run.placement": "bottom"})
	path := writeTestNotebook(t, "analysis.ipynb", testNotebook)
	m = settle(t, m, m.host.Dispatch(palette.OpenFileMsg{Path: path}))
	inst := notebookPane(m)
	if inst == nil {
		t.Fatal("the notebook pane did not open")
	}
	if focused := m.activeWS().Panes.FocusedInstance(); focused == nil || focused.Kind() != pane.KindNotebook {
		t.Fatal("the notebook pane must be focused after the open")
	}
	return m, canonicalPath(path)
}

// assertNotebookArgv checks the Run tool runs nbconvert in place for path
// from the notebook's directory.
func assertNotebookArgv(t *testing.T, m Model, path string) *terminal.Model {
	t.Helper()
	term := m.runToolTerminal()
	if term == nil {
		t.Fatal("the Run tool did not open")
	}
	argv := strings.Join(term.Argv(), " ")
	want := "-m jupyter nbconvert --to notebook --execute --inplace " + path
	if !strings.HasSuffix(argv, want) {
		t.Fatalf("argv = %q, want suffix %q", argv, want)
	}
	if !strings.HasSuffix(term.Argv()[0], "/python") {
		t.Fatalf("argv[0] = %q, want the explicit interpreter", term.Argv()[0])
	}
	if term.Dir() != filepath.Dir(path) {
		t.Fatalf("cwd = %q, want the notebook's directory %q", term.Dir(), filepath.Dir(path))
	}
	return term
}

// TestRunFileOnNotebookPane: shift+f10 on a focused notebook viewer runs the
// notebook through nbconvert and persists its default configuration.
func TestRunFileOnNotebookPane(t *testing.T) {
	m, path := notebookRunApp(t, `echo "$@"`)
	tm, _ := m.Update(RunFileMsg{})
	m = tm.(Model)
	term := assertNotebookArgv(t, m, path)
	t.Cleanup(term.Close)
	store := run.Load()
	cfg := store.ByName("analysis.ipynb")
	if cfg == nil || !cfg.Notebook || cfg.Lang != "python" {
		t.Fatalf("the default notebook config must persist: %+v", store)
	}
	if store.LastUsed != "analysis.ipynb" {
		t.Fatalf("last used = %q, want the notebook", store.LastUsed)
	}
	if !hasToast(m, `saved as "analysis.ipynb"`) {
		t.Fatalf("the first run must say the config was saved, got %v", toastTexts(m))
	}
}

// TestNotebookRunMsgAndKey: the pane's RunMsg and the notebook.run command
// (r via the keymap) launch the same run; rerun repeats it.
func TestNotebookRunMsgAndKey(t *testing.T) {
	m, path := notebookRunApp(t, `echo "$@"`)
	tm, _ := m.Update(nbview.RunMsg{Path: path})
	m = tm.(Model)
	term := assertNotebookArgv(t, m, path)
	t.Cleanup(term.Close)

	// The Run tool took focus; back on the notebook, r resolves through the
	// keymap's notebook context to notebook.run.
	_, _, inst, _ := m.findContent(func(c *pane.Instance) bool { return c.Kind() == pane.KindNotebook })
	m.setFocus(inst.Key())
	term.Close()
	m = pressKey(m, 'r')
	term = assertNotebookArgv(t, m, path)
	t.Cleanup(term.Close)

	term.Close()
	tm, _ = m.Update(RunRerunMsg{})
	m = tm.(Model)
	term = assertNotebookArgv(t, m, path)
	t.Cleanup(term.Close)
}

// TestNotebookRunRefusesOtherPanes: notebook.run with no notebook focused is
// a notice, not a launch.
func TestNotebookRunRefusesOtherPanes(t *testing.T) {
	m := hexApp(t, host.MapConfig{})
	tm, _ := m.Update(NotebookRunMsg{})
	m = tm.(Model)
	if m.runToolTerminal() != nil {
		t.Fatal("nothing must run without a notebook")
	}
	if !hasToast(m, "focus a notebook") {
		t.Fatalf("expected the focus notice, got %v", toastTexts(m))
	}
}

// TestNotebookMissingJupyterHint: a run that exits at once with Python's
// missing-module error gets the pip hint on top of the Run tool's output.
func TestNotebookMissingJupyterHint(t *testing.T) {
	m, _ := notebookRunApp(t, `echo "/usr/bin/python3: No module named jupyter" >&2; exit 1`)
	tm, _ := m.Update(RunFileMsg{})
	m = tm.(Model)
	term := m.runToolTerminal()
	if term == nil || m.nbRun == nil {
		t.Fatal("the notebook run must be watched")
	}
	t.Cleanup(term.Close)
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, exited := term.ExitCode()
		if exited && m.nbRun.missingJupyter() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the run did not exit with the missing-module error; head=%q", m.nbRun.head)
		}
		time.Sleep(10 * time.Millisecond)
	}
	m.toasts = nil
	tm, _ = m.Update(terminal.ExitedMsg{Key: term.SessionKey()})
	m = tm.(Model)
	if !hasToast(m, "pip install jupyter nbconvert") {
		t.Fatalf("expected the install hint, got %v", toastTexts(m))
	}
	if m.nbRun != nil {
		t.Fatal("the watch must be dropped after the exit")
	}
}

// TestNotebookCleanExitNoHint: a run that exits fine (or fails for another
// reason) says nothing beyond the Run tool.
func TestNotebookCleanExitNoHint(t *testing.T) {
	m, _ := notebookRunApp(t, `echo "[NbConvertApp] Writing"; exit 0`)
	tm, _ := m.Update(RunFileMsg{})
	m = tm.(Model)
	term := m.runToolTerminal()
	t.Cleanup(term.Close)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, exited := term.ExitCode(); exited {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the run did not exit")
		}
		time.Sleep(10 * time.Millisecond)
	}
	m.toasts = nil
	tm, _ = m.Update(terminal.ExitedMsg{Key: term.SessionKey()})
	m = tm.(Model)
	if hasToast(m, "pip install") {
		t.Fatalf("a clean exit must not hint at pip: %v", toastTexts(m))
	}
}

// TestNotebookReloadsOnAtomicReplace: a writer that renames a temp file over
// the notebook reports a removal to the watcher — the pane re-reads the
// replacement and the poll set is re-armed for the new inode.
func TestNotebookReloadsOnAtomicReplace(t *testing.T) {
	m := hexApp(t, host.MapConfig{})
	path := writeTestNotebook(t, "analysis.ipynb", testNotebook)
	m = settle(t, m, m.host.Dispatch(palette.OpenFileMsg{Path: path}))
	if notebookPane(m) == nil {
		t.Fatal("the notebook pane did not open")
	}
	updated := strings.Replace(testNotebook, `"# Analysis\n"`, `"# Analysis\n", "\n", "executed run\n"`, 1)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	out, cmd := m.Update(watch.EventMsg{Path: canonicalPath(path), Kind: watch.FileRemoved})
	m = settle(t, out.(Model), cmd)
	inst := notebookPane(m)
	if inst == nil {
		t.Fatal("the notebook pane must survive the replace")
	}
	if !strings.Contains(strings.Join(inst.Notebook().Rows(), "\n"), "executed run") {
		t.Fatalf("the pane did not re-render the replacement:\n%s", strings.Join(inst.Notebook().Rows(), "\n"))
	}
	// A notebook that is really gone keeps its rows and closes nothing.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	out, cmd = m.Update(watch.EventMsg{Path: canonicalPath(path), Kind: watch.FileRemoved})
	m = settle(t, out.(Model), cmd)
	if notebookPane(m) == nil {
		t.Fatal("a removed notebook must keep its pane")
	}
}

// TestDebugNotebookRefused: a notebook configuration never goes under the
// debugger — there is no program for debugpy to attach to.
func TestDebugNotebookRefused(t *testing.T) {
	m := hexApp(t, host.MapConfig{})
	m.startDebugConfig(t.TempDir(), run.Config{Name: "a.ipynb", Kind: run.KindRun, Lang: "python", File: "a.ipynb", Notebook: true})
	notes := m.host.DrainNotifications()
	if len(notes) == 0 || !strings.Contains(notes[0].Text, "nbconvert") {
		t.Fatalf("expected the notebook debug notice, got %v", notes)
	}
	if m.dbg != nil || m.dbgLaunching {
		t.Fatal("no debug session may start for a notebook")
	}
}
