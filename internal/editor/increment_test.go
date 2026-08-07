package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
)

// increment_test.go covers ctrl+a / ctrl+x and the value toggle (#1658).

// ctrlKey builds a ctrl-modified key press ("ctrl+a").
func ctrlKey(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }

func TestFindNumberUnderAndAfterCursor(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		col        int
		start, end int
		hex        bool
		ok         bool
	}{
		{name: "under cursor", line: "x = 42", col: 5, start: 4, end: 6},
		{name: "after cursor", line: "x = 42", col: 0, start: 4, end: 6},
		{name: "first of two", line: "1 and 2", col: 0, start: 0, end: 1},
		{name: "second when past first", line: "1 and 2", col: 3, start: 6, end: 7},
		{name: "negative sign joins", line: "v = -7", col: 5, start: 4, end: 6},
		{name: "hex whole literal", line: "m = 0x1f", col: 6, start: 4, end: 8, hex: true},
		{name: "inside identifier", line: "abc123", col: 0, start: 3, end: 6},
		{name: "none on line", line: "no digits", ok: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.ok || tc.end != 0
			span, ok := findNumber([]rune(tc.line), tc.col)
			if ok != want {
				t.Fatalf("ok=%v want %v", ok, want)
			}
			if !ok {
				return
			}
			if span.start != tc.start || span.end != tc.end || span.hex != tc.hex {
				t.Fatalf("span=%+v want start=%d end=%d hex=%v", span, tc.start, tc.end, tc.hex)
			}
		})
	}
}

func TestAddToNumberShapes(t *testing.T) {
	tests := []struct {
		text  string
		hex   bool
		delta int64
		want  string
	}{
		{text: "42", delta: 1, want: "43"},
		{text: "42", delta: -1, want: "41"},
		{text: "42", delta: 5, want: "47"},
		{text: "007", delta: 1, want: "008"},
		{text: "099", delta: 1, want: "100"},
		{text: "010", delta: -1, want: "009"},
		{text: "0", delta: -1, want: "-1"},
		{text: "-7", delta: 1, want: "-6"},
		{text: "-7", delta: -1, want: "-8"},
		{text: "-007", delta: -1, want: "-008"},
		{text: "0x1f", hex: true, delta: 1, want: "0x20"},
		{text: "0x0F", hex: true, delta: 1, want: "0x10"},
		{text: "0xff", hex: true, delta: 1, want: "0x100"},
		{text: "0X00ff", hex: true, delta: 1, want: "0X0100"},
		{text: "0xAB", hex: true, delta: 1, want: "0xAC"},
	}
	for _, tc := range tests {
		got, ok := addToNumber(tc.text, tc.hex, tc.delta)
		if !ok || got != tc.want {
			t.Errorf("addToNumber(%q, %v, %d) = %q, %v; want %q", tc.text, tc.hex, tc.delta, got, ok, tc.want)
		}
	}
}

func TestAddToNumberSaturatesAndRejectsWideLiterals(t *testing.T) {
	if got, _ := addToNumber("9223372036854775807", false, 1); got != "9223372036854775807" {
		t.Errorf("decimal overflow should clamp, got %q", got)
	}
	if _, ok := addToNumber("99999999999999999999999", false, 1); ok {
		t.Error("a decimal too wide for int64 must be left alone")
	}
	if _, ok := addToNumber("0xfffffffffffffffff", true, 1); ok {
		t.Error("a hex literal too wide for uint64 must be left alone")
	}
}

func TestIncrementKeysCountAndCursor(t *testing.T) {
	m, _ := loaded(t, "port = 8080\n")
	m = send(m, ctrlKey('a'))
	if got := line(m, 0); got != "port = 8081" {
		t.Fatalf("ctrl+a: %q", got)
	}
	// Vim leaves the cursor on the last digit of the new number.
	if m.cursor.Col != 10 {
		t.Fatalf("cursor col=%d, want 10", m.cursor.Col)
	}
	m = send(m, ctrlKey('x'))
	if got := line(m, 0); got != "port = 8080" {
		t.Fatalf("ctrl+x: %q", got)
	}
	// A count multiplies the step: 5<C-a>.
	m = typeKeys(m, "0")
	m = send(m, key('5'), ctrlKey('a'))
	if got := line(m, 0); got != "port = 8085" {
		t.Fatalf("5<C-a>: %q", got)
	}
}

func TestIncrementHexAndLeadingZerosInBuffer(t *testing.T) {
	m, _ := loaded(t, "mask 0x1f\nseq 007\n")
	m = send(m, ctrlKey('a'))
	if got := line(m, 0); got != "mask 0x20" {
		t.Fatalf("hex: %q", got)
	}
	m = typeKeys(m, "j0")
	m = send(m, ctrlKey('a'))
	if got := line(m, 1); got != "seq 008" {
		t.Fatalf("leading zeros: %q", got)
	}
}

func TestIncrementNoNumberLeavesBufferClean(t *testing.T) {
	m, _ := loaded(t, "nothing here\n")
	m = send(m, ctrlKey('a'))
	if got := line(m, 0); got != "nothing here" {
		t.Fatalf("line changed: %q", got)
	}
	if m.Dirty() {
		t.Fatal("a no-op increment must not dirty the buffer")
	}
}

func TestIncrementDotRepeatAndUndo(t *testing.T) {
	m, _ := loaded(t, "n = 1\n")
	m = send(m, ctrlKey('a'))
	m = typeKeys(m, "..")
	if got := line(m, 0); got != "n = 4" {
		t.Fatalf("dot repeat: %q", got)
	}
	// Each step is one undo unit.
	m = typeKeys(m, "u")
	if got := line(m, 0); got != "n = 3" {
		t.Fatalf("undo: %q", got)
	}
}

func TestIncrementActionsAndMultiCaret(t *testing.T) {
	m, _ := loaded(t, "a 1\nb 1\n")
	m, _ = m.runAction("increment")
	if got := line(m, 0); got != "a 2" {
		t.Fatalf("increment action: %q", got)
	}
	m, _ = m.runAction("decrement")
	if got := line(m, 0); got != "a 1" {
		t.Fatalf("decrement action: %q", got)
	}
	// A caret cloned onto the second line bumps both numbers as one change.
	m, _ = m.runAction("caret_add_below")
	m = send(m, ctrlKey('a'))
	if line(m, 0) != "a 2" || line(m, 1) != "b 2" {
		t.Fatalf("multi-caret: %q / %q", line(m, 0), line(m, 1))
	}
	m = typeKeys(m, "u")
	if line(m, 0) != "a 1" || line(m, 1) != "b 1" {
		t.Fatalf("multi-caret undo: %q / %q", line(m, 0), line(m, 1))
	}
}

func TestToggleValueCapitalizationAndOperators(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{line: "debug = true", want: "debug = false"},
		{line: "debug = false", want: "debug = true"},
		{line: "debug = True", want: "debug = False"},
		{line: "debug = TRUE", want: "debug = FALSE"},
		{line: "flag: on", want: "flag: off"},
		{line: "answer: yes", want: "answer: no"},
		{line: "state = enabled", want: "state = disabled"},
		{line: "if a == b", want: "if a != b"},
		{line: "if a && b", want: "if a || b"},
		{line: "if a < b", want: "if a > b"},
	}
	for _, tc := range tests {
		m, _ := loaded(t, tc.line+"\n")
		m = typeKeys(m, "g!")
		if got := line(m, 0); got != tc.want {
			t.Errorf("toggle %q = %q, want %q", tc.line, got, tc.want)
		}
	}
}

func TestToggleValueSkipsLongerOperatorRuns(t *testing.T) {
	// "<=" is one token, so it is not a "<" to flip; nothing on the line
	// matches and the buffer stays untouched.
	m, _ := loaded(t, "if a <= b\n")
	m = typeKeys(m, "g!")
	if got := line(m, 0); got != "if a <= b" {
		t.Fatalf("line changed: %q", got)
	}
	if m.Dirty() {
		t.Fatal("a no-op toggle must not dirty the buffer")
	}
	// A word containing a pair member is not the member itself.
	m2, _ := loaded(t, "isEnabled = 1\n")
	m2 = typeKeys(m2, "g!")
	if got := line(m2, 0); got != "isEnabled = 1" {
		t.Fatalf("substring toggled: %q", got)
	}
}

func TestToggleValueUnderCursorBeatsLaterMatch(t *testing.T) {
	m, _ := loaded(t, "true and false\n")
	// Two words on: the cursor sits on "false", which wins over the "true"
	// behind it.
	m = typeKeys(m, "0ww")
	m = typeKeys(m, "g!")
	if got := line(m, 0); got != "true and true" {
		t.Fatalf("toggle: %q", got)
	}
}

func TestToggleValueDotRepeatAndAction(t *testing.T) {
	m, _ := loaded(t, "a = true\n")
	m = typeKeys(m, "g!")
	m = typeKeys(m, ".")
	if got := line(m, 0); got != "a = true" {
		t.Fatalf("dot repeat should flip back: %q", got)
	}
	m, _ = m.runAction("toggle_value")
	if got := line(m, 0); got != "a = false" {
		t.Fatalf("toggle_value action: %q", got)
	}
}

func TestTogglePairsFromConfig(t *testing.T) {
	m, _ := loaded(t, "level = allow\n")
	m.Configure(host.MapConfig{"editor.toggle_pairs": "allow=deny, ,bad-entry"})
	m = typeKeys(m, "g!")
	if got := line(m, 0); got != "level = deny" {
		t.Fatalf("configured pair: %q", got)
	}
	// Built-ins survive alongside configured pairs.
	m2, _ := loaded(t, "x = on\n")
	m2.Configure(host.MapConfig{"editor.toggle_pairs": "allow=deny"})
	m2 = typeKeys(m2, "g!")
	if got := line(m2, 0); got != "x = off" {
		t.Fatalf("built-in pair: %q", got)
	}
}

func TestParseTogglePairsSkipsMalformed(t *testing.T) {
	got := parseTogglePairs("a=b, =c, d=, ,e = f")
	want := [][2]string{{"a", "b"}, {"e", "f"}}
	if len(got) != len(want) {
		t.Fatalf("pairs=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pairs=%v want %v", got, want)
		}
	}
}
