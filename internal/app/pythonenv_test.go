package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/settings"
)

// TestEnvMsgRegistersInterpreter guards #132: a successful environment action
// writes [lang.python] interpreter to the project config and toasts; a failed
// one only warns.
func TestEnvMsgRegistersInterpreter(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("IKE_CONFIG_DIR", "")
	m := switchModel(t)

	out, cmd := m.updateMsg(settings.EnvMsg{LangID: "python", Interpreter: "/opt/py/bin/python", Label: "created .venv"})
	m = out.(Model)
	m.drainNotifications()
	if cmd == nil {
		t.Fatal("success should return the write-back batch")
	}
	// Run the batch so the write lands (results not fed back; the write is a
	// side effect of the cmd itself).
	runCmds(cmd)
	data, err := os.ReadFile(filepath.Join(dir, ".ike", "settings.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "interpreter = \"/opt/py/bin/python\"") {
		t.Fatalf("interpreter should persist to the project config, got:\n%s", data)
	}
	if len(m.toasts) == 0 || !strings.Contains(m.toasts[0].text, "registered as project interpreter") {
		t.Fatalf("toasts = %+v", m.toasts)
	}

	before, _ := os.ReadFile(filepath.Join(dir, ".ike", "settings.toml"))
	out, cmd2 := m.updateMsg(settings.EnvMsg{LangID: "python", Err: os.ErrNotExist})
	m = out.(Model)
	m.drainNotifications()
	runCmds(cmd2) // nothing to write on failure
	after, _ := os.ReadFile(filepath.Join(dir, ".ike", "settings.toml"))
	if string(before) != string(after) {
		t.Fatal("failure must not write anything")
	}
	if !strings.Contains(m.toasts[0].text, "python environment") {
		t.Fatalf("toasts = %+v", m.toasts)
	}
}

// runCmds executes a cmd tree (batches flattened); resulting msgs are not
// fed back — the effects under test are the cmds' own side effects. Timer
// ticks are skipped so the drain does not sleep.
func runCmds(cmd tea.Cmd) {
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
		}
	}
}
