package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/project"
)

// guardprompt_test.go covers the enter-confirms-the-primary-option rule of the
// modal guard prompts (#1356): every guard keeps its letter shortcuts and esc,
// and enter answers for the first option listed.

// enterKey is the key press the guards must read as "confirm".
var enterKey = tea.KeyPressMsg{Code: tea.KeyEnter}

// guardBody renders the open guard prompt's body for hint assertions.
func guardBody(m Model) string { return m.shell.Content().Render(80) }

// TestGuardLineHintsEnterOnPrimary (#1356): only the primary option advertises
// the enter alias, and the descriptions stay aligned across both shapes.
func TestGuardLineHintsEnterOnPrimary(t *testing.T) {
	primary := guardLine("s", "save all", true)
	other := guardLine("d", "discard", false)
	cancel := guardCancel("cancel")
	if !strings.Contains(primary, "[s/enter]") {
		t.Errorf("primary line must hint enter, got %q", primary)
	}
	if strings.Contains(other, "enter") || strings.Contains(cancel, "enter") {
		t.Errorf("only the primary line hints enter, got %q / %q", other, cancel)
	}
	if got, want := strings.Index(primary, "save all"), strings.Index(other, "discard"); got != want {
		t.Errorf("descriptions misaligned: %d vs %d (%q / %q)", got, want, primary, other)
	}
	if strings.Index(cancel, "cancel") != strings.Index(other, "discard") {
		t.Errorf("cancel line misaligned: %q / %q", cancel, other)
	}
	if !strings.HasSuffix(primary, "\n") || strings.HasSuffix(cancel, "\n") {
		t.Errorf("option lines end in a newline, the cancel line does not: %q / %q", primary, cancel)
	}
}

// TestGuardAnswerMapsEnterToPrimary (#1356): enter resolves to the primary
// option, every other key answers for itself.
func TestGuardAnswerMapsEnterToPrimary(t *testing.T) {
	if got := guardAnswer(enterKey, "s"); got != "s" {
		t.Errorf("enter must answer the primary option, got %q", got)
	}
	if got := guardAnswer(tea.KeyPressMsg{Code: 'd', Text: "d"}, "s"); got != "d" {
		t.Errorf("letter keys answer for themselves, got %q", got)
	}
	if got := guardAnswer(tea.KeyPressMsg{Code: tea.KeyEscape}, "s"); got != "esc" {
		t.Errorf("esc must stay esc, got %q", got)
	}
}

// TestCloseGuardEnterSaves (#1356): enter takes the close guard's primary
// option — save, then close.
func TestCloseGuardEnterSaves(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	m := switchModel(t)
	m = openDirty(t, m, file)
	m.guardedCloseFocused()
	if !m.closePromptOpen() {
		t.Fatal("closing a dirty buffer must prompt")
	}
	body := guardBody(m)
	if !strings.Contains(body, "[s/enter]") {
		t.Errorf("close guard body must hint enter, got %q", body)
	}

	out, _ := m.Update(enterKey)
	m = out.(Model)
	if m.closePromptOpen() {
		t.Fatal("enter must answer the guard")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "one\n" {
		t.Fatalf("enter must save before closing, file = %q", data)
	}
}

// TestQuitGuardEnterSavesThenQuits (#1356): with dirty buffers the quit guard's
// primary option is "save all, then quit".
func TestQuitGuardEnterSavesThenQuits(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	m := switchModel(t)
	m = openDirty(t, m, file)
	out, _ := m.guardedQuit()
	m = out.(Model)
	if !m.closePromptOpen() {
		t.Fatal("quitting with dirty buffers must prompt")
	}

	out, cmd := m.Update(enterKey)
	m = out.(Model)
	if m.closePromptOpen() {
		t.Fatal("enter must answer the quit guard")
	}
	if data, _ := os.ReadFile(file); string(data) == "one\n" {
		t.Fatalf("enter must save before quitting, file = %q", data)
	}
	if cmd == nil {
		t.Fatal("enter must quit after saving")
	}
}

// TestQuitGuardEnterQuitsWhenNothingToSave (#1356): a running-only quit prompt
// offers no save option, so enter takes the plain quit.
func TestQuitGuardEnterQuitsWhenNothingToSave(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	m := switchModel(t)
	m.openQuitPrompt(nil, []string{"run build"})
	if !m.closePromptOpen() {
		t.Fatal("the quit guard must be open")
	}
	body := guardBody(m)
	if !strings.Contains(body, "[d/enter]") {
		t.Errorf("with nothing to save the quit line is primary, got %q", body)
	}

	_, cmd := m.Update(enterKey)
	if cmd == nil {
		t.Fatal("enter must quit when there is nothing to save")
	}
}

// TestSwitchGuardEnterSaves (#1356): the switch guard's primary option is
// "save all, then switch".
func TestSwitchGuardEnterSaves(t *testing.T) {
	base := t.TempDir()
	a, b := filepath.Join(base, "a"), filepath.Join(base, "b")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(a, "f.txt")
	if err := os.WriteFile(file, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(a)
	m := switchModel(t)
	m = openDirty(t, m, file)
	out, _ := m.Update(project.UnsavedChangesMsg{Root: b})
	m = out.(Model)
	if !m.switchPromptOpen() {
		t.Fatal("the switch guard must be open")
	}
	if body := guardBody(m); !strings.Contains(body, "[s/enter]") {
		t.Errorf("switch guard body must hint enter, got %q", body)
	}

	out, _ = m.Update(enterKey)
	m = out.(Model)
	if m.switchPromptOpen() {
		t.Fatal("enter must answer the switch guard")
	}
	if data, _ := os.ReadFile(file); string(data) == "one\n" {
		t.Fatalf("enter must save before switching, file = %q", data)
	}
	if !sameDir(t, cwd(t), b) {
		t.Fatalf("enter must switch, cwd = %s", cwd(t))
	}
}

// TestWsCloseGuardEnterSaves (#1356): the close-from-list guard saves the
// background workspace's buffers on enter, then closes it.
func TestWsCloseGuardEnterSaves(t *testing.T) {
	m, root, path := busyCloseFixture(t)
	out, _ := m.Update(project.CloseWorkspaceMsg{Path: root})
	m = out.(Model)
	if !m.wsClosePromptOpen() {
		t.Fatal("busy close-from-list must prompt")
	}
	if body := guardBody(m); !strings.Contains(body, "[s/enter]") {
		t.Errorf("ws-close guard body must hint enter, got %q", body)
	}

	out, _ = m.Update(enterKey)
	m = out.(Model)
	if m.ws.Peek(root) != nil {
		t.Fatal("enter must close the workspace after saving")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "Xone") {
		t.Fatalf("enter must write the background buffer, got %q", data)
	}
}

// TestProjectCloseGuardEnterSaves (#1356): the close-current guard saves on
// enter, then closes and resumes the parked project.
func TestProjectCloseGuardEnterSaves(t *testing.T) {
	m, _, bRoot, dirtyPath := closeCurrentFixture(t)
	out, _ := m.Update(project.CloseProjectMsg{})
	m = out.(Model)
	if !m.projectClosePromptOpen() {
		t.Fatal("busy close-current must prompt")
	}
	if body := guardBody(m); !strings.Contains(body, "[s/enter]") {
		t.Errorf("project-close guard body must hint enter, got %q", body)
	}

	out, _ = m.Update(enterKey)
	m = out.(Model)
	if !sameDir(t, m.activeWS().Root, bRoot) {
		t.Fatalf("enter must close after saving, active = %s", m.activeWS().Root)
	}
	if data, _ := os.ReadFile(dirtyPath); !strings.HasPrefix(string(data), "Xone") {
		t.Fatalf("enter must write the dirty buffer, got %q", data)
	}
}

// TestEvictGuardEnterEvicts (#1356): the eviction guard has a single confirm
// option, so enter evicts and esc still keeps the workspace.
func TestEvictGuardEnterEvicts(t *testing.T) {
	m, root, _ := busyCloseFixture(t)
	m.openEvictPrompt(root)
	if !m.evictPromptOpen() {
		t.Fatal("the eviction guard must be open")
	}
	if body := guardBody(m); !strings.Contains(body, "[e/enter]") {
		t.Errorf("evict guard body must hint enter, got %q", body)
	}

	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.evictPromptOpen() || m.ws.Peek(root) == nil {
		t.Fatal("esc must keep the workspace running")
	}

	m.openEvictPrompt(root)
	out, _ = m.Update(enterKey)
	m = out.(Model)
	if m.evictPromptOpen() || m.ws.Peek(root) != nil {
		t.Fatal("enter must evict the workspace")
	}
}
