package debugpanel

// copy_test.go covers debug.copy (cmd+c in the debug panel, #2400).

import (
	"testing"

	"ike/internal/dap"
)

// panelWithVars builds a paused panel with one scope of variables.
func panelWithVars(t *testing.T) *Model {
	t.Helper()
	m := New(nil)
	m.SetSize(80, 12)
	m.SetFrames(frames())
	m.SetScopes([]dap.Scope{{Name: "Locals", VariablesReference: 100}})
	m.SetChildren(100, []dap.Variable{
		{Name: "x", Value: "42"},
		{Name: "name", Value: `"ada"`},
	})
	return &m
}

// copied runs the copy command and returns the requested clipboard text.
func copied(t *testing.T, m *Model) (string, string) {
	t.Helper()
	cmd := m.CopyKeyCmd()
	if cmd == nil {
		return "", ""
	}
	msg, ok := cmd().(CopyMsg)
	if !ok {
		t.Fatalf("copy produced %T, want CopyMsg", cmd())
	}
	return msg.Text, msg.What
}

// TestCopySelectedVariable: in the variables column the chord copies the
// selected row as "name = value".
func TestCopySelectedVariable(t *testing.T) {
	m := panelWithVars(t)
	m.Update(key("tab")) // to the variables column
	m.Update(key("j"))   // Locals(0) → x(1)

	text, what := copied(t, m)
	if text != "x = 42" {
		t.Fatalf("copied %q, want %q", text, "x = 42")
	}
	if what != "variable value" {
		t.Fatalf("what = %q", what)
	}
}

// TestCopySelectedFrame: the frames column copies the stack frame line the
// panel renders.
func TestCopySelectedFrame(t *testing.T) {
	m := panelWithVars(t)

	text, what := copied(t, m)
	if text != "inner — a.py:7" {
		t.Fatalf("copied %q, want the selected frame", text)
	}
	if what != "stack frame" {
		t.Fatalf("what = %q", what)
	}
}

// TestCopyWithNothingSelected stays inert: an empty panel has nothing to put
// on the clipboard, and a chord that would copy nothing must not pretend it
// did.
func TestCopyWithNothingSelected(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 12)
	if cmd := m.CopyKeyCmd(); cmd != nil {
		t.Fatalf("empty panel produced %+v", cmd())
	}
}
