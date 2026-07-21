package settings

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// confirmVia drives the pushed confirmation's destructive button (#891).
func confirmVia(t *testing.T, h *stubHost) tea.Cmd {
	t.Helper()
	c, ok := h.top().(*confirmPanel)
	if !ok {
		t.Fatalf("expected a confirmation sub-panel, got %T", h.top())
	}
	return c.Buttons()[0].Do()
}

// TestConfirmCancelKeepsData: cancel pops without running the action.
func TestConfirmCancelKeepsData(t *testing.T) {
	h := &stubHost{}
	ran := false
	h.Push(newConfirm(h, "delete x", "Delete", nil, func() tea.Cmd { ran = true; return nil }))
	c := h.top().(*confirmPanel)
	c.Buttons()[1].Do()
	if ran || h.top() != nil {
		t.Fatal("cancel must pop without running")
	}
}

// TestConfirmYRuns: the y synonym confirms.
func TestConfirmYRuns(t *testing.T) {
	h := &stubHost{}
	ran := false
	h.Push(newConfirm(h, "delete x", "Delete", nil, func() tea.Cmd { ran = true; return nil }))
	h.top().(*confirmPanel).Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if !ran || h.top() != nil {
		t.Fatal("y must confirm and pop")
	}
}
