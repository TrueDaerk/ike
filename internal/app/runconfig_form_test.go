package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/run"
)

// openEnvForm runs the active file (which persists its default configuration)
// and opens that configuration in the run-configuration form.
func openEnvForm(t *testing.T, m Model) Model {
	t.Helper()
	tm, _ := m.Update(RunFileMsg{})
	m = tm.(Model)
	store := run.Load()
	if len(store.Configs) != 1 {
		t.Fatalf("setup: configs = %+v, want the default one", store.Configs)
	}
	tm, _ = m.Update(RunConfigPickedMsg{Cfg: store.Configs[0], Stored: true, Edit: true})
	m = tm.(Model)
	if !m.runConfigFormOpen() {
		t.Fatal("an edit-mode pick must open the run-configuration form")
	}
	return m
}

// addEnvRow drives the form's row editor: a add, key, tab, value, enter.
func addEnvRow(m Model, key, value string) Model {
	m = drainKey(m, tea.KeyPressMsg{Text: "a", Code: 'a'})
	m = typeText(m, key)
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = typeText(m, value)
	return drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
}

// TestRunConfigFormEnvRoundTrip is the #2173 acceptance path: variables added
// as rows in the form are persisted into the store and reach a spawned
// process as KEY=VALUE pairs.
func TestRunConfigFormEnvRoundTrip(t *testing.T) {
	m := openEnvForm(t, runModel(t, "bottom"))
	m = addEnvRow(m, "API_KEY", "secret")
	m = addEnvRow(m, "MODE", "dev")
	if got := len(m.runForm.rows); got != 2 {
		t.Fatalf("rows = %d, want 2: %+v", got, m.runForm.rows)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.runConfigFormOpen() {
		t.Fatal("a successful save must close the form")
	}
	store := run.Load()
	cfg := store.Configs[0]
	if cfg.Env["API_KEY"] != "secret" || cfg.Env["MODE"] != "dev" {
		t.Fatalf("stored env = %+v, want the edited rows", cfg.Env)
	}
	if slice := cfg.EnvSlice(); len(slice) != 2 || slice[0] != "API_KEY=secret" {
		t.Fatalf("EnvSlice = %+v, want the pairs handed to the process", slice)
	}

	// Re-opening the form shows what was saved — the editor reads the store,
	// not the picker's open-time copy.
	tm, _ := m.Update(RunConfigPickedMsg{Cfg: run.Config{Name: cfg.Name}, Stored: true, Edit: true})
	m = tm.(Model)
	if len(m.runForm.rows) != 2 || m.runForm.rows[0].Key != "API_KEY" {
		t.Fatalf("reopened rows = %+v, want the saved environment", m.runForm.rows)
	}
}

// TestRunConfigFormRejectsBadKeys: an empty key and a duplicate key are
// refused with a message and leave the row list unchanged.
func TestRunConfigFormRejectsBadKeys(t *testing.T) {
	m := openEnvForm(t, runModel(t, "bottom"))
	m = addEnvRow(m, "A", "1")

	// Empty key: the row editor stays open with the reason.
	m = drainKey(m, tea.KeyPressMsg{Text: "a", Code: 'a'})
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.runForm.editing || !strings.Contains(m.runForm.err, "must not be empty") {
		t.Fatalf("empty key must be rejected: editing=%v err=%q", m.runForm.editing, m.runForm.err)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape}) // abandon the row

	// Duplicate key: same treatment, and the existing row is untouched.
	m = addEnvRow(m, "A", "2")
	if !m.runForm.editing || !strings.Contains(m.runForm.err, "duplicate") {
		t.Fatalf("duplicate key must be rejected: editing=%v err=%q", m.runForm.editing, m.runForm.err)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if len(m.runForm.rows) != 1 || m.runForm.rows[0].Value != "1" {
		t.Fatalf("rows = %+v, want the original single row", m.runForm.rows)
	}

	// Editing a row in place keeps its own key legal (no self-duplicate).
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m = typeText(m, "9")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.runForm.err != "" || m.runForm.rows[0].Value != "19" {
		t.Fatalf("in-place edit = %+v (err %q)", m.runForm.rows, m.runForm.err)
	}
}

// TestRunConfigFormRemoveRow: d drops the selected row, and the save writes
// the shortened set (an emptied list clears the overlay).
func TestRunConfigFormRemoveRow(t *testing.T) {
	m := openEnvForm(t, runModel(t, "bottom"))
	m = addEnvRow(m, "A", "1")
	m = addEnvRow(m, "B", "2")
	m = drainKey(m, tea.KeyPressMsg{Text: "d", Code: 'd'}) // removes B (selected)
	if len(m.runForm.rows) != 1 || m.runForm.rows[0].Key != "A" {
		t.Fatalf("rows after remove = %+v", m.runForm.rows)
	}
	m = drainKey(m, tea.KeyPressMsg{Text: "d", Code: 'd'})
	m = drainKey(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if env := run.Load().Configs[0].Env; len(env) != 0 {
		t.Fatalf("env = %+v, want the overlay cleared", env)
	}
}

// TestRunConfigFormCancelDiscards: esc closes the form without touching the
// store — the edits only exist in the dialog until ctrl+s.
func TestRunConfigFormCancelDiscards(t *testing.T) {
	m := openEnvForm(t, runModel(t, "bottom"))
	m = addEnvRow(m, "A", "1")
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.runConfigFormOpen() {
		t.Fatal("esc must close the form")
	}
	if env := run.Load().Configs[0].Env; len(env) != 0 {
		t.Fatalf("env = %+v, want nothing persisted", env)
	}
}

// TestRerunLastRepeatsDebugKind: a configuration last started under the
// debugger reruns under the debugger — the chord does not silently downgrade
// it to a plain run.
func TestRerunLastRepeatsDebugKind(t *testing.T) {
	m := runModel(t, "bottom")
	store := run.Load()
	cfg, _, ok := store.EnsureFor(projectRoot(), m.activeEditor().Path())
	if !ok {
		t.Fatal("setup: no config for the fake language")
	}
	store.Touch(cfg.Name, run.KindDebug)
	if err := run.Save(store); err != nil {
		t.Fatal(err)
	}
	base := len(m.history)
	tm, _ := m.Update(RunRerunMsg{})
	m = tm.(Model)
	if len(m.toolLocations(runToolName)) != 0 {
		t.Fatal("a debug rerun must not open the Run tool")
	}
	if len(m.history) == base || !strings.HasPrefix(m.history[0].text, "debug:") {
		t.Fatalf("rerun must take the debug funnel: %+v", m.history)
	}

	// Touched as a plain run again, the same configuration reruns in the Run
	// tool.
	store = run.Load()
	store.Touch(cfg.Name, run.KindRun)
	if err := run.Save(store); err != nil {
		t.Fatal(err)
	}
	tm, _ = m.Update(RunRerunMsg{})
	m = tm.(Model)
	if len(m.toolLocations(runToolName)) == 0 {
		t.Fatal("a run rerun must open the Run tool")
	}
}

// TestRerunLastNothingYet: the chord toasts instead of doing nothing when no
// configuration has ever run.
func TestRerunLastNothingYet(t *testing.T) {
	m := runModel(t, "bottom")
	base := len(m.history)
	tm, _ := m.Update(RunRerunMsg{})
	m = tm.(Model)
	if len(m.toolLocations(runToolName)) != 0 {
		t.Fatal("nothing ran yet — nothing may be launched")
	}
	if len(m.history) == base || !strings.Contains(m.history[0].text, "nothing to rerun") {
		t.Fatalf("missing toast: %+v", m.history)
	}
}
