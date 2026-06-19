package editor

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// key builds a tea.KeyMsg for a single rune.
func key(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// special builds a tea.KeyMsg for a non-rune key.
func special(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

// send applies a sequence of keys and returns the resulting model.
func send(m Model, keys ...tea.KeyMsg) Model {
	for _, k := range keys {
		m, _ = m.Update(k)
	}
	return m
}

func loaded(t *testing.T, content string) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(80, 20)
	m.SetFocused(true)
	return m, path
}

func TestLoadSplitsLines(t *testing.T) {
	m, _ := loaded(t, "alpha\nbeta\ngamma\n")
	if len(m.lines) != 3 {
		t.Fatalf("want 3 lines, got %d: %q", len(m.lines), m.lines)
	}
	if m.lines[1] != "beta" {
		t.Fatalf("line 1 = %q", m.lines[1])
	}
	if m.Dirty() {
		t.Fatal("fresh load should not be dirty")
	}
}

func TestMotionsHJKL(t *testing.T) {
	m, _ := loaded(t, "abc\ndef\n")
	m = send(m, key('l'), key('l')) // col -> 2
	if m.col != 2 {
		t.Fatalf("after ll col=%d want 2", m.col)
	}
	m = send(m, key('l')) // clamp at last rune
	if m.col != 2 {
		t.Fatalf("col should clamp at 2, got %d", m.col)
	}
	m = send(m, key('j')) // down
	if m.row != 1 {
		t.Fatalf("row=%d want 1", m.row)
	}
	m = send(m, key('h'), key('h'), key('h'))
	if m.col != 0 {
		t.Fatalf("col=%d want 0", m.col)
	}
}

func TestMotionsLineStartEnd(t *testing.T) {
	m, _ := loaded(t, "hello world\n")
	m = send(m, key('$'))
	if m.col != 10 {
		t.Fatalf("$ col=%d want 10", m.col)
	}
	m = send(m, key('0'))
	if m.col != 0 {
		t.Fatalf("0 col=%d want 0", m.col)
	}
}

func TestMotionGgG(t *testing.T) {
	m, _ := loaded(t, "a\nb\nc\nd\n")
	m = send(m, key('G'))
	if m.row != 3 {
		t.Fatalf("G row=%d want 3", m.row)
	}
	m = send(m, key('g'), key('g'))
	if m.row != 0 {
		t.Fatalf("gg row=%d want 0", m.row)
	}
}

func TestWordMotion(t *testing.T) {
	m, _ := loaded(t, "foo bar baz\n")
	m = send(m, key('w'))
	if m.col != 4 {
		t.Fatalf("w col=%d want 4", m.col)
	}
	m = send(m, key('w'))
	if m.col != 8 {
		t.Fatalf("w col=%d want 8", m.col)
	}
	m = send(m, key('b'))
	if m.col != 4 {
		t.Fatalf("b col=%d want 4", m.col)
	}
}

func TestDeleteRuneX(t *testing.T) {
	m, _ := loaded(t, "abc\n")
	m = send(m, key('x'))
	if m.lines[0] != "bc" {
		t.Fatalf("after x line=%q want bc", m.lines[0])
	}
	if !m.Dirty() {
		t.Fatal("x should set dirty")
	}
}

func TestDeleteLineDd(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\nthree\n")
	m = send(m, key('j'), key('d'), key('d'))
	if len(m.lines) != 2 || m.lines[1] != "three" {
		t.Fatalf("after dd lines=%q", m.lines)
	}
}

func TestInsertMode(t *testing.T) {
	m, _ := loaded(t, "bc\n")
	m = send(m, key('i'), key('a'))
	if m.lines[0] != "abc" {
		t.Fatalf("insert i: line=%q want abc", m.lines[0])
	}
	if m.ModeName() != Insert {
		t.Fatal("should be in insert mode")
	}
	m = send(m, special(tea.KeyEsc))
	if m.ModeName() != Normal {
		t.Fatal("esc should return to normal")
	}
}

func TestAppendMode(t *testing.T) {
	m, _ := loaded(t, "ab\n")
	m = send(m, key('a'), key('X')) // append after first char
	if m.lines[0] != "aXb" {
		t.Fatalf("append a: line=%q want aXb", m.lines[0])
	}
}

func TestOpenLineBelow(t *testing.T) {
	m, _ := loaded(t, "top\nbottom\n")
	m = send(m, key('o'), key('n'), key('e'), key('w'))
	if len(m.lines) != 3 || m.lines[1] != "new" {
		t.Fatalf("o: lines=%q", m.lines)
	}
	if m.ModeName() != Insert {
		t.Fatal("o should enter insert mode")
	}
}

func TestOpenLineAbove(t *testing.T) {
	m, _ := loaded(t, "top\nbottom\n")
	m = send(m, key('j'), key('O'), key('m'), key('i'), key('d'))
	if m.lines[1] != "mid" {
		t.Fatalf("O: lines=%q", m.lines)
	}
}

func TestEnterSplitsLine(t *testing.T) {
	m, _ := loaded(t, "abcd\n")
	m = send(m, key('i')) // insert at col 0
	m = send(m, special(tea.KeyEsc))
	m = send(m, key('l'), key('l'), key('a')) // cursor after index2 -> col3
	m = send(m, special(tea.KeyEnter))
	if len(m.lines) != 2 || m.lines[0] != "abc" || m.lines[1] != "d" {
		t.Fatalf("enter split: lines=%q", m.lines)
	}
}

func TestBackspaceJoinsLines(t *testing.T) {
	m, _ := loaded(t, "ab\ncd\n")
	m = send(m, key('j'), key('i'), special(tea.KeyBackspace))
	if len(m.lines) != 1 || m.lines[0] != "abcd" {
		t.Fatalf("backspace join: lines=%q", m.lines)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	m, path := loaded(t, "hello\n")
	m = send(m, key('x')) // delete 'h' -> "ello"
	if !m.Dirty() {
		t.Fatal("should be dirty before save")
	}
	// :w
	m = send(m, key(':'), key('w'), special(tea.KeyEnter))
	if m.Dirty() {
		t.Fatal("save should clear dirty")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ello\n" {
		t.Fatalf("file = %q want %q", got, "ello\n")
	}
}

func TestQuitEmitsCloseMsg(t *testing.T) {
	m, _ := loaded(t, "x\n")
	m, _ = m.Update(key(':'))
	m, _ = m.Update(key('q'))
	m, cmd := m.Update(special(tea.KeyEnter))
	if cmd == nil {
		t.Fatal(":q should return a command")
	}
	if _, ok := cmd().(CloseMsg); !ok {
		t.Fatalf(":q msg = %T want CloseMsg", cmd())
	}
}

func TestCommandLineRender(t *testing.T) {
	m, _ := loaded(t, "x\n")
	m = send(m, key(':'), key('w'), key('q'))
	if got := m.CommandLine(); got != ":wq" {
		t.Fatalf("command line = %q want :wq", got)
	}
}
