package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
)

// The sticky goal column (#1687, vim's curswant): vertical movement remembers
// the column it started from, so a short line in between clamps the caret but
// the next long enough line puts it back.

const goalText = "richtig langer text\n---\nanderer richtig langer text"

func TestGoalColumnNormalRestoresAfterShortLine(t *testing.T) {
	m, _ := loaded(t, goalText)
	// Between "langer" and "text" on line 0.
	m.cursor = buffer.Position{Line: 0, Col: 14}
	m.desiredCol = 14

	m = typeKeys(m, "j")
	if m.cursor != (buffer.Position{Line: 1, Col: 2}) {
		t.Fatalf("short line must clamp, cursor at %v", m.cursor)
	}
	m = typeKeys(m, "j")
	if m.cursor != (buffer.Position{Line: 2, Col: 14}) {
		t.Fatalf("goal column not restored, cursor at %v", m.cursor)
	}
	// And back up again.
	m = typeKeys(m, "kk")
	if m.cursor != (buffer.Position{Line: 0, Col: 14}) {
		t.Fatalf("goal column lost going up, cursor at %v", m.cursor)
	}
}

func TestGoalColumnNormalArrowsRestoreAfterShortLine(t *testing.T) {
	m, _ := loaded(t, goalText)
	m.cursor = buffer.Position{Line: 0, Col: 14}
	m.desiredCol = 14

	m = send(m, special(tea.KeyDown), special(tea.KeyDown))
	if m.cursor != (buffer.Position{Line: 2, Col: 14}) {
		t.Fatalf("goal column not restored, cursor at %v", m.cursor)
	}
}

func TestGoalColumnNormalResetByHorizontalMove(t *testing.T) {
	m, _ := loaded(t, goalText)
	m.cursor = buffer.Position{Line: 0, Col: 14}
	m.desiredCol = 14

	m = typeKeys(m, "j") // clamps to col 2 on "---"
	m = typeKeys(m, "h") // horizontal move resets the goal to col 1
	m = typeKeys(m, "j")
	if m.cursor != (buffer.Position{Line: 2, Col: 1}) {
		t.Fatalf("horizontal move must reset the goal column, cursor at %v", m.cursor)
	}
}

func TestGoalColumnInsertRestoresAfterShortLine(t *testing.T) {
	m, _ := loaded(t, goalText)
	m.cursor = buffer.Position{Line: 0, Col: 14}
	m.desiredCol = 14
	m = typeKeys(m, "i")

	m = send(m, special(tea.KeyDown))
	if m.cursor != (buffer.Position{Line: 1, Col: 3}) {
		t.Fatalf("short line must clamp past the end in insert, cursor at %v", m.cursor)
	}
	m = send(m, special(tea.KeyDown))
	if m.cursor != (buffer.Position{Line: 2, Col: 14}) {
		t.Fatalf("goal column not restored in insert, cursor at %v", m.cursor)
	}
	m = send(m, special(tea.KeyUp), special(tea.KeyUp))
	if m.cursor != (buffer.Position{Line: 0, Col: 14}) {
		t.Fatalf("goal column lost going up in insert, cursor at %v", m.cursor)
	}
}

func TestGoalColumnInsertResetByHorizontalMove(t *testing.T) {
	m, _ := loaded(t, goalText)
	m.cursor = buffer.Position{Line: 0, Col: 14}
	m.desiredCol = 14
	m = typeKeys(m, "i")

	m = send(m, special(tea.KeyDown), special(tea.KeyLeft), special(tea.KeyDown))
	if m.cursor != (buffer.Position{Line: 2, Col: 2}) {
		t.Fatalf("horizontal move must reset the goal column, cursor at %v", m.cursor)
	}
}

func TestGoalColumnInsertResetByTyping(t *testing.T) {
	m, _ := loaded(t, goalText)
	m.cursor = buffer.Position{Line: 0, Col: 14}
	m.desiredCol = 14
	m = typeKeys(m, "i")

	m = send(m, special(tea.KeyDown)) // clamped to col 3 on "---"
	m = typeKeys(m, "X")              // edit resets the goal to col 4
	m = send(m, special(tea.KeyDown))
	if m.cursor != (buffer.Position{Line: 2, Col: 4}) {
		t.Fatalf("edit must reset the goal column, cursor at %v", m.cursor)
	}
}

func TestGoalColumnResetByClick(t *testing.T) {
	m, _ := loaded(t, goalText)
	m.cursor = buffer.Position{Line: 0, Col: 14}
	m.desiredCol = 14

	m = typeKeys(m, "j") // clamped on "---"
	m.MouseClick(1, 1)   // click on line 1, col 1
	m = typeKeys(m, "j")
	if m.cursor != (buffer.Position{Line: 2, Col: 1}) {
		t.Fatalf("click must reset the goal column, cursor at %v", m.cursor)
	}
}
