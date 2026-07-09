package editor

import (
	"testing"

	"ike/internal/editor/buffer"
)

// runEx executes body as a ":" command line and returns the updated model.
func runEx(m Model, body string) Model {
	m.mode = Command
	m.cmdline = body
	m, _ = m.runExLine()
	return m
}

func TestExGotoLineAndRange(t *testing.T) {
	m, _ := loaded(t, "a\nb\nc\nd\ne\n")
	// Bare line number jumps (1-based) → 0-based index.
	if m = runEx(m, "3"); m.cursor.Line != 2 {
		t.Fatalf(":3 → line %d, want 2", m.cursor.Line)
	}
	// "$" jumps to the last line.
	if m = runEx(m, "$"); m.cursor.Line != 4 {
		t.Fatalf(":$ → line %d, want 4", m.cursor.Line)
	}
	// A two-address range jumps to the range's last line.
	if m = runEx(m, "1,4"); m.cursor.Line != 3 {
		t.Fatalf(":1,4 → line %d, want 3", m.cursor.Line)
	}
	// Out-of-range clamps to the last line.
	if m = runEx(m, "999"); m.cursor.Line != 4 {
		t.Fatalf(":999 → line %d, want 4", m.cursor.Line)
	}
}

func TestExPatternAddress(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\nthree\nfour\n")
	m.moveTo(pos(0, 0))
	if m = runEx(m, "/three/"); m.cursor.Line != 2 {
		t.Fatalf(":/three/ → line %d, want 2", m.cursor.Line)
	}
	// Backward search from the last line wraps/finds the earlier match.
	m.moveTo(pos(3, 0))
	if m = runEx(m, "?one?"); m.cursor.Line != 0 {
		t.Fatalf(":?one? → line %d, want 0", m.cursor.Line)
	}
}

func TestExNotImplementedMessages(t *testing.T) {
	m, _ := loaded(t, "x\n")
	if m = runEx(m, "g/x/d"); m.cmdMsg == "" {
		t.Fatal(":g should report not-implemented via cmdMsg")
	}
	if m = runEx(m, "nonsense"); m.cmdMsg == "" {
		t.Fatal("unknown command should report an error via cmdMsg")
	}
	// A subsequent normal-mode key clears the message.
	m = send(m, key('l'))
	if m.cmdMsg != "" {
		t.Fatalf("cmdMsg should clear on the next key, got %q", m.cmdMsg)
	}
}

func TestExVisualRangePrefill(t *testing.T) {
	m, _ := loaded(t, "a\nb\nc\nd\n")
	m.moveTo(pos(1, 0))
	// V to linewise-select, j to extend to line 2, then ":".
	m = send(m, key('V'), key('j'), key(':'))
	if m.mode != Command {
		t.Fatalf("':' from visual should enter command mode, mode=%v", m.mode)
	}
	if m.cmdline != "'<,'>" {
		t.Fatalf("visual ':' prefill = %q, want '<,'>", m.cmdline)
	}
	if m.visualStart != 1 || m.visualEnd != 2 {
		t.Fatalf("visual bounds = (%d,%d), want (1,2)", m.visualStart, m.visualEnd)
	}
}

// pos is a small helper for buffer positions in tests.
func pos(line, col int) buffer.Position { return buffer.Position{Line: line, Col: col} }
