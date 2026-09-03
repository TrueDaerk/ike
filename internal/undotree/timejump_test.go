package undotree

import (
	"strings"
	"testing"
	"time"

	"ike/internal/editor/history"
)

// agedNodes is a linear history one state per minute: seq 1 is 30m old, seq 2
// 10m, seq 3 (current) 1m. The root has no timestamp.
func agedNodes(now time.Time) []history.NodeInfo {
	return []history.NodeInfo{
		{Seq: 0, Parent: -1},
		{Seq: 1, Parent: 0, At: now.Add(-30 * time.Minute), Preview: `+"a"`},
		{Seq: 2, Parent: 1, At: now.Add(-10 * time.Minute), Preview: `+"b"`},
		{Seq: 3, Parent: 2, At: now.Add(-1 * time.Minute), Preview: `+"c"`, Current: true},
	}
}

func timeJumpModel(now time.Time) *Model {
	m := New()
	m.now = func() time.Time { return now }
	m.SetSize(100, 40)
	m.Open(agedNodes(now))
	return m
}

func TestTimeJumpPicksNewestStateOlderThanTheAge(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		minutes int
		want    int
	}{
		{0, 3},   // everything qualifies: the newest state wins
		{5, 2},   // seq 3 is only 1m old
		{20, 1},  // seq 2 is only 10m old
		{60, 0},  // nothing timestamped is that old: the root state
		{999, 0}, // ditto
	}
	for _, c := range cases {
		m := timeJumpModel(now)
		m.Update(key("t"))
		for _, d := range strings.Split(itoa(c.minutes), "") {
			m.Update(key(d))
		}
		cmd := m.Update(key("enter"))
		if cmd == nil {
			t.Fatalf("%dm: time jump emitted no command", c.minutes)
		}
		jump, ok := cmd().(JumpMsg)
		if !ok {
			t.Fatalf("%dm: emitted %T, want JumpMsg", c.minutes, cmd())
		}
		if jump.Seq != c.want {
			t.Errorf("%dm ago = state %d, want %d", c.minutes, jump.Seq, c.want)
		}
		if m.rows[m.cursor].node.Seq != c.want {
			t.Errorf("%dm ago left the cursor on %d, want %d",
				c.minutes, m.rows[m.cursor].node.Seq, c.want)
		}
	}
}

func TestTimeJumpPromptEditingAndCancel(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	m := timeJumpModel(now)
	m.Update(key("t"))
	if !m.asking {
		t.Fatal("t should open the age prompt")
	}
	if v := m.View(); !strings.Contains(v, "minutes ago") {
		t.Errorf("the prompt should be visible:\n%s", v)
	}
	// Digits type, non-digits (list keys) are ignored while asking.
	m.Update(key("1"))
	m.Update(key("j"))
	m.Update(key("2"))
	m.Update(key("backspace"))
	m.Update(key("5"))
	if m.ageInput.Text != "15" {
		t.Fatalf("age input = %q, want \"15\"", m.ageInput.Text)
	}
	if m.cursor != 0 {
		t.Errorf("the prompt must swallow list keys, cursor = %d", m.cursor)
	}
	if cmd := m.Update(key("esc")); cmd != nil {
		t.Error("esc must cancel the jump, not perform it")
	}
	if m.asking || m.ageInput.Text != "" {
		t.Error("esc should close the prompt and clear the input")
	}
	if !m.IsOpen() {
		t.Error("esc on the prompt closes the prompt, not the overlay")
	}
}

func TestTimeJumpEmptyInputIsANoOp(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	m := timeJumpModel(now)
	m.Update(key("t"))
	if cmd := m.Update(key("enter")); cmd != nil {
		t.Error("confirming an empty age must not jump")
	}
	if m.asking {
		t.Error("enter closes the prompt even on empty input")
	}
}

// itoa avoids importing strconv just for the test table.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
