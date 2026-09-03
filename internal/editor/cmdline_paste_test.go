package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/register"
)

// Tests for paste routing into editor-internal inputs (#1380): with the
// search/ex line or the find/replace panel open, a bracketed paste (and
// cmd+v) lands in that input — never in the buffer underneath.

func TestPasteTextIntoSearchLine(t *testing.T) {
	m, _ := loaded(t, "alpha beta\n")
	m = send(m, key('/'))
	m.PasteText("beta")
	if m.cmdline != "beta" || m.cmdCur != 4 {
		t.Fatalf("search paste: cmdline=%q cmdCur=%d want %q 4", m.cmdline, m.cmdCur, "beta")
	}
	if line(m, 0) != "alpha beta" {
		t.Fatalf("paste leaked into the buffer: %q", line(m, 0))
	}
	// The paste behaves like typing: the incremental preview tracks it.
	if m.preview.Pattern != "beta" {
		t.Fatalf("preview pattern=%q want %q", m.preview.Pattern, "beta")
	}
}

func TestPasteTextIntoSearchLineAtCursor(t *testing.T) {
	m, _ := loaded(t, "axb\n")
	m = send(m, key('/'))
	m = typeKeys(m, "ab")
	m.cmdCur = 1
	m.PasteText("x")
	if m.cmdline != "axb" || m.cmdCur != 2 {
		t.Fatalf("mid-line paste: cmdline=%q cmdCur=%d want %q 2", m.cmdline, m.cmdCur, "axb")
	}
}

func TestPasteTextMultiLineFlattensIntoSearchLine(t *testing.T) {
	m, _ := loaded(t, "alpha beta\n")
	m = send(m, key('/'))
	m.PasteText("one\ntwo\n")
	if m.cmdline != "one two" {
		t.Fatalf("multi-line paste not flattened: cmdline=%q", m.cmdline)
	}
	if line(m, 0) != "alpha beta" || m.buf.LineCount() != 1 {
		t.Fatalf("multi-line paste leaked into the buffer: %q", m.buf.Lines())
	}
}

func TestPasteTextIntoExLine(t *testing.T) {
	m, _ := loaded(t, "alpha\n")
	m = send(m, key(':'))
	m = typeKeys(m, "e ")
	m.PasteText("some/path.go")
	if m.cmdline != "e some/path.go" {
		t.Fatalf("ex paste: cmdline=%q", m.cmdline)
	}
	if line(m, 0) != "alpha" {
		t.Fatalf("paste leaked into the buffer: %q", line(m, 0))
	}
}

func TestPasteTextIntoReplacePanelFields(t *testing.T) {
	m, _ := loaded(t, "alpha beta\n")
	m, _ = m.runAction("replace")
	if m.replPanel == nil {
		t.Fatal("replace panel should be open")
	}
	m.PasteText("alpha")
	if m.replPanel.find.Text != "alpha" {
		t.Fatalf("find paste: %q", m.replPanel.find.Text)
	}
	m = send(m, tab())
	m.PasteText("gamma")
	if m.replPanel.repl.Text != "gamma" {
		t.Fatalf("replace paste: %q", m.replPanel.repl.Text)
	}
	if line(m, 0) != "alpha beta" {
		t.Fatalf("paste leaked into the buffer: %q", line(m, 0))
	}
}

func TestPasteTextReplacesPreselectedPanelPrefill(t *testing.T) {
	m, _ := loaded(t, "alpha beta\n")
	// Commit a literal search so the panel opens with a preselected prefill.
	m = send(m, key('/'))
	m = typeKeys(m, "alpha")
	m = send(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m, _ = m.runAction("replace")
	if m.replPanel == nil || !m.replPanel.preselect {
		t.Fatal("panel should open with a preselected prefill")
	}
	m.PasteText("beta")
	if m.replPanel.find.Text != "beta" {
		t.Fatalf("paste should replace the preselected prefill: %q", m.replPanel.find.Text)
	}
}

func TestPasteTextSwallowedBySubstituteConfirm(t *testing.T) {
	m, _ := loaded(t, "a b a\n")
	m = runEx(m, "s/a/X/gc")
	if m.subConfirm == nil {
		t.Fatal("confirm prompt should be open")
	}
	m.PasteText("LEAK")
	if line(m, 0) != "a b a" {
		t.Fatalf("paste leaked into the buffer under the confirm prompt: %q", line(m, 0))
	}
	if m.subConfirm == nil {
		t.Fatal("paste must not disturb the confirm prompt")
	}
}

func TestClipboardPasteIntoSearchLine(t *testing.T) {
	m, _ := loaded(t, "alpha beta\n")
	m.regs.Yank('+', register.Entry{Text: "beta"})
	m = send(m, key('/'))
	m, _ = m.runAction("paste") // cmd+v
	if m.cmdline != "beta" {
		t.Fatalf("cmd+v into search line: cmdline=%q", m.cmdline)
	}
	if line(m, 0) != "alpha beta" {
		t.Fatalf("cmd+v leaked into the buffer: %q", line(m, 0))
	}
}

func TestPasteTextInactiveSearchStillHitsBuffer(t *testing.T) {
	m, _ := loaded(t, "abc\n")
	m.PasteText("XYZ")
	if line(m, 0) != "aXYZbc" {
		t.Fatalf("normal-mode paste broken: %q", line(m, 0))
	}
}
