package terminal

import (
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// collector gathers async session msgs.
type collector struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (c *collector) send(msg tea.Msg) {
	c.mu.Lock()
	c.msgs = append(c.msgs, msg)
	c.mu.Unlock()
}

func (c *collector) has(pred func(tea.Msg) bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range c.msgs {
		if pred(m) {
			return true
		}
	}
	return false
}

// waitFor polls until cond holds or the deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// startSh spawns a plain /bin/sh session for grid assertions.
func startSh(t *testing.T, c *collector) *Session {
	t.Helper()
	s, err := StartSession("terminal", "/bin/sh", t.TempDir(), 80, 24, nil, c.send)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func plainView(s *Session) string { return ansi.Strip(s.View()) }

func TestSessionEchoRendersOnGrid(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)

	for _, r := range "echo hello-grid\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "echo output", func() bool {
		return strings.Count(plainView(s), "hello-grid") >= 2 // echoed input + output
	})
	if !c.has(func(m tea.Msg) bool { _, ok := m.(OutputMsg); return ok }) {
		t.Fatal("output should raise coalesced OutputMsg notifications")
	}
}

func keyFor(r rune) vt.KeyPressEvent {
	if r == '\r' {
		return vt.KeyPressEvent{Code: vt.KeyEnter}
	}
	return vt.KeyPressEvent{Code: r, Text: string(r)}
}

func TestSessionResizePropagates(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)

	s.Resize(100, 30)
	for _, r := range "stty size\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "stty size output", func() bool {
		return strings.Contains(plainView(s), "30 100")
	})
}

func TestSessionExitLifecycle(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)

	for _, r := range "exit\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "exit msg", func() bool {
		return c.has(func(m tea.Msg) bool { _, ok := m.(ExitedMsg); return ok })
	})
	if s.Running() {
		t.Fatal("session should report not running after exit")
	}
	s.Close() // double close is safe
}

func TestSessionAltScreenAndColors(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)

	// Drive raw escape sequences through printf: color + cursor addressing.
	cmd := `printf '\033[2J\033[3;5H\033[31mRED-MARK\033[0m'` + "\r"
	for _, r := range cmd {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "styled output", func() bool {
		return strings.Contains(plainView(s), "RED-MARK")
	})
	// The styled view keeps the SGR color around the mark.
	if !strings.Contains(s.View(), "RED-MARK") {
		t.Fatal("styled view should contain the mark")
	}
	lines := strings.Split(plainView(s), "\n")
	if len(lines) < 3 || !strings.Contains(lines[2], "RED-MARK") {
		t.Fatalf("cursor addressing should place the mark on row 3, got %q", lines[2])
	}
}

func TestShellResolution(t *testing.T) {
	if got := Shell("/bin/zsh"); got != "/bin/zsh" {
		t.Fatalf("override should win, got %q", got)
	}
	t.Setenv("SHELL", "/bin/fish")
	if got := Shell(""); got != "/bin/fish" {
		t.Fatalf("$SHELL should apply, got %q", got)
	}
	t.Setenv("SHELL", "")
	if got := Shell(""); got != "/bin/sh" {
		t.Fatalf("fallback should be /bin/sh, got %q", got)
	}
}

func TestScrollbackPaging(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	m := Model{sess: s, h: 24}

	// Push well over one screen of output into history.
	for _, r := range "seq 1 200\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "output scrolled", func() bool { return s.ScrollbackLen() > 20 })

	m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp, Mod: tea.ModShift})
	if m.Scroll() == 0 {
		t.Fatal("shift+pgup should enter the scrollback")
	}
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "[scrollback -") {
		t.Fatalf("scrolled view should carry the position marker:\n%s", v)
	}
	// Paging back down (clamped) returns to live.
	for i := 0; i < 10; i++ {
		m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown, Mod: tea.ModShift})
	}
	if m.Scroll() != 0 {
		t.Fatalf("paging down should clamp back to live, scroll = %d", m.Scroll())
	}

	// Any ordinary key snaps back to live and reaches the shell.
	m.ScrollBy(30)
	m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.Scroll() != 0 {
		t.Fatal("a typed key should snap the view back to live")
	}
}

// TestShiftedTextReachesShell: shift/caps-lock characters arrive as modified
// key presses that the vt encoder would drop; the model replays their text so
// uppercase input reaches the shell (#224).
func TestShiftedTextReachesShell(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	m := Model{sess: s, h: 24}

	for _, r := range "echo " {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m.Update(tea.KeyPressMsg{Code: 'u', ShiftedCode: 'U', Mod: tea.ModShift, Text: "U"})
	m.Update(tea.KeyPressMsg{Code: 'p', ShiftedCode: 'P', Mod: tea.ModCapsLock, Text: "P"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	waitFor(t, "uppercase echo output", func() bool {
		return strings.Count(plainView(s), "UP") >= 2 // echoed input + output
	})
}

// TestMouseSelection: drag selects text in virtual coordinates, cmd+c-style
// extraction returns it, typed keys clear it (#227).
func TestMouseSelection(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	m := Model{sess: s, h: 24, w: 80}

	for _, r := range "echo alpha-beta gamma\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "echo output", func() bool {
		return strings.Count(plainView(s), "alpha-beta gamma") >= 2
	})
	// Locate the output line (the non-echoed one, without "echo ").
	rows := strings.Split(plainView(s), "\n")
	rowIdx, colIdx := -1, -1
	for i, r := range rows {
		if idx := strings.Index(r, "alpha-beta gamma"); idx >= 0 && !strings.Contains(r, "echo ") {
			rowIdx, colIdx = i, idx
			break
		}
	}
	if rowIdx < 0 {
		t.Fatalf("output row not found in:\n%s", plainView(s))
	}

	// Drag across "alpha-beta" (10 cells, exclusive head).
	m.MousePress(colIdx, rowIdx)
	m.MouseDrag(colIdx+10, rowIdx)
	m.MouseRelease(colIdx+10, rowIdx)
	if !m.HasSelection() {
		t.Fatal("drag should create a selection")
	}
	if got := m.SelectionText(); got != "alpha-beta" {
		t.Fatalf("selection text = %q, want %q", got, "alpha-beta")
	}
	// The view highlights the span (reverse-video escape around the text).
	if !strings.Contains(m.View(), "alpha-beta") || m.View() == s.View() {
		t.Fatal("selection should restyle the view")
	}

	// The selection survives scrollback paging (virtual anchoring).
	m.ScrollBy(2)
	if got := m.SelectionText(); got != "alpha-beta" {
		t.Fatalf("selection after paging = %q, want %q", got, "alpha-beta")
	}
	m.ScrollBy(-2)

	// A key routed to the shell clears it.
	m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.HasSelection() {
		t.Fatal("typed key should clear the selection")
	}
}

// TestMouseSelectionMultiline: a drag across rows takes the start line's tail,
// full middle lines, and the end line's head.
func TestMouseSelectionMultiline(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	m := Model{sess: s, h: 24, w: 80}

	for _, r := range "printf 'one-mark\\ntwo-mark\\nthree-mark\\n'\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "printf output", func() bool {
		return strings.Contains(plainView(s), "three-mark")
	})
	rows := strings.Split(plainView(s), "\n")
	first := -1
	for i, r := range rows {
		if strings.HasPrefix(r, "one-mark") {
			first = i
			break
		}
	}
	if first < 0 {
		t.Fatalf("output rows not found in:\n%s", plainView(s))
	}
	m.MousePress(4, first) // from "mark" of line one
	m.MouseDrag(5, first+2)
	m.MouseRelease(5, first+2)
	if got, want := m.SelectionText(), "mark\ntwo-mark\nthree"; got != want {
		t.Fatalf("selection = %q, want %q", got, want)
	}
}

// TestMousePressForwardsToMouseChild: with a mouse-reporting child the press
// forwards as an encoded event instead of selecting.
func TestMousePressForwardsToMouseChild(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	m := Model{sess: s, h: 24, w: 80}

	for _, r := range "printf '\\033[?1000h\\033[?1006h' && cat\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "mouse mode", func() bool { return s.WantsMouse() })
	m.MousePress(10, 5)
	m.MouseRelease(10, 5)
	if m.HasSelection() {
		t.Fatal("press should forward to the child, not select")
	}
	waitFor(t, "SGR click echoed", func() bool {
		return strings.Contains(plainView(s), "[<0;") // MouseLeft press
	})
}

// TestMouseWheelRouting: the wheel goes to whoever asked for it (#226) —
// a mouse-reporting child gets the encoded event, an alt-screen child gets
// arrow keys, the plain shell pages the pane's scrollback.
func TestMouseWheelRouting(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	m := Model{sess: s, h: 24}

	// Plain prompt: the wheel pages the scrollback.
	for _, r := range "seq 1 200\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "history", func() bool { return s.ScrollbackLen() > 20 })
	m.MouseWheel(10, 5, 3)
	if m.Scroll() != 3 {
		t.Fatalf("plain shell: wheel should page the scrollback, scroll = %d", m.Scroll())
	}
	m.MouseWheel(10, 5, -3)
	if m.Scroll() != 0 {
		t.Fatal("plain shell: wheel down should return to live")
	}

	// Alt screen without mouse reporting: the wheel turns into arrow keys
	// (cat echoes them as ^[[A via the tty's ECHOCTL).
	for _, r := range "printf '\\033[?1049h' && cat\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "alt screen", func() bool { return s.AltScreen() })
	if s.WantsMouse() {
		t.Fatal("cat should not enable mouse reporting")
	}
	m.MouseWheel(10, 5, 3)
	waitFor(t, "arrow keys echoed", func() bool {
		return strings.Contains(plainView(s), "^[[A^[[A^[[A")
	})

	// Mouse reporting on: the wheel forwards as an encoded mouse event.
	// Enter first so ctrl+d is an EOF, not a partial-line flush.
	s.SendKey(vt.KeyPressEvent{Code: vt.KeyEnter})
	s.SendKey(vt.KeyPressEvent{Code: 'd', Mod: vt.ModCtrl}) // end cat
	for _, r := range "printf '\\033[?1000h\\033[?1006h' && cat\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "mouse mode tracked", func() bool { return s.WantsMouse() })
	m.MouseWheel(10, 5, 3)
	waitFor(t, "SGR wheel event echoed", func() bool {
		return strings.Contains(plainView(s), "[<64;")
	})

	// Disabling the mode hands the wheel back to the scrollback.
	s.SendKey(vt.KeyPressEvent{Code: vt.KeyEnter})
	s.SendKey(vt.KeyPressEvent{Code: 'd', Mod: vt.ModCtrl})
	for _, r := range "printf '\\033[?1000l\\033[?1049l'\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "modes reset", func() bool { return !s.WantsMouse() && !s.AltScreen() })
}

// TestMotionKeyTranslation: the macOS editing chords map to the readline
// emacs-mode defaults (#225); everything else passes through untranslated.
func TestMotionKeyTranslation(t *testing.T) {
	cases := []struct {
		name string
		in   tea.KeyPressMsg
		want vt.KeyPressEvent
	}{
		{"option+left", tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt}, vt.KeyPressEvent{Code: 'b', Mod: vt.ModAlt}},
		{"option+right", tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModAlt}, vt.KeyPressEvent{Code: 'f', Mod: vt.ModAlt}},
		{"shift+option+left", tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModShift | tea.ModAlt}, vt.KeyPressEvent{Code: 'b', Mod: vt.ModAlt}},
		{"cmd+left (super)", tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModSuper}, vt.KeyPressEvent{Code: 'a', Mod: vt.ModCtrl}},
		{"cmd+right (super)", tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModSuper}, vt.KeyPressEvent{Code: 'e', Mod: vt.ModCtrl}},
		{"cmd+right (meta)", tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModMeta}, vt.KeyPressEvent{Code: 'e', Mod: vt.ModCtrl}},
		{"option+backspace", tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt}, vt.KeyPressEvent{Code: vt.KeyBackspace, Mod: vt.ModAlt}},
		{"option+forward-delete", tea.KeyPressMsg{Code: tea.KeyDelete, Mod: tea.ModAlt}, vt.KeyPressEvent{Code: 'd', Mod: vt.ModAlt}},
		{"shift+option+forward-delete", tea.KeyPressMsg{Code: tea.KeyDelete, Mod: tea.ModShift | tea.ModAlt}, vt.KeyPressEvent{Code: 'd', Mod: vt.ModAlt}},
		{"cmd+backspace (super)", tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModSuper}, vt.KeyPressEvent{Code: 'u', Mod: vt.ModCtrl}},
		{"cmd+backspace (meta)", tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModMeta}, vt.KeyPressEvent{Code: 'u', Mod: vt.ModCtrl}},
	}
	for _, c := range cases {
		got, ok := motionKey(c.in)
		if !ok || got != c.want {
			t.Fatalf("%s: got %#v ok=%v, want %#v", c.name, got, ok, c.want)
		}
	}
	for _, in := range []tea.KeyPressMsg{
		{Code: tea.KeyLeft},                                // plain arrow
		{Code: tea.KeyLeft, Mod: tea.ModCtrl},              // pane focus chord territory
		{Code: tea.KeyUp, Mod: tea.ModAlt},                 // only left/right translate
		{Code: 'b', Mod: tea.ModAlt},                       // native ESC b stays as-is
		{Code: tea.KeyPgUp, Mod: tea.ModShift},             // scrollback paging
		{Code: tea.KeyLeft, Mod: tea.ModAlt | tea.ModCtrl}, // extra modifier
		{Code: tea.KeyBackspace},                           // plain backspace stays raw
		{Code: tea.KeyBackspace, Mod: tea.ModCtrl},         // not a natural-editing chord
	} {
		if _, ok := motionKey(in); ok {
			t.Fatalf("%#v should not translate", in)
		}
	}
}

// TestMotionKeysDriveTheShell: option/cmd arrows edit the readline buffer of
// a real shell — cmd+left prepends at the line start, option+left lands at
// the last word (#225).
func TestMotionKeysDriveTheShell(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	m := Model{sess: s, h: 24}

	type press = tea.KeyPressMsg
	for _, r := range "cho start-mark" {
		m.Update(press{Code: r, Text: string(r)})
	}
	m.Update(press{Code: tea.KeyLeft, Mod: tea.ModSuper}) // → ctrl+a
	m.Update(press{Code: 'e', Text: "e"})                 // completes "echo"
	m.Update(press{Code: tea.KeyEnter})
	waitFor(t, "line-start edit", func() bool {
		return strings.Count(plainView(s), "start-mark") >= 2 // echoed input + output
	})

	for _, r := range "echo one two" {
		m.Update(press{Code: r, Text: string(r)})
	}
	m.Update(press{Code: tea.KeyLeft, Mod: tea.ModAlt}) // → ESC b, before "two"
	m.Update(press{Code: 'X', ShiftedCode: 'X', Mod: tea.ModShift, Text: "X"})
	m.Update(press{Code: tea.KeyEnter})
	waitFor(t, "word-jump edit", func() bool {
		return strings.Count(plainView(s), "one Xtwo") >= 2
	})
}

// TestShiftedTextKeepsSpecialKeys: shift on non-text keys stays a modified
// event — shift+pgup must still page the scrollback, not type anything.
func TestShiftedTextKeepsSpecialKeys(t *testing.T) {
	got := toVTKeys(tea.KeyPressMsg{Code: tea.KeyPgUp, Mod: tea.ModShift})
	want := vt.KeyPressEvent{Code: vt.KeyPgUp, Mod: vt.ModShift}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("shift+pgup should pass through unchanged, got %#v", got)
	}
	// A ctrl chord with text stays a chord too.
	got = toVTKeys(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl, Text: "c"})
	if len(got) != 1 || got[0].Mod != vt.ModCtrl {
		t.Fatalf("ctrl+c should keep its modifier, got %#v", got)
	}
}

func TestScrollByClamps(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	m := Model{sess: s, h: 24}
	m.ScrollBy(-5)
	if m.Scroll() != 0 {
		t.Fatal("negative scroll clamps to live")
	}
	m.ScrollBy(1 << 20)
	if m.Scroll() > s.ScrollbackLen() {
		t.Fatal("scroll clamps to the available history")
	}
}

// TestSessionOSCTitle: OSC 2 title sequences land in Title() for the pane.
func TestSessionOSCTitle(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	cmd := `printf '\033]2;building things\007'` + "\r"
	for _, r := range cmd {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "osc title", func() bool { return s.Title() == "building things" })
}

// TestSessionClear empties history and repaints via ctrl+l.
func TestSessionClear(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	for _, r := range "seq 1 100\r" {
		s.SendKey(keyFor(r))
	}
	// Wait for seq to FINISH (its last line on screen), not merely to start —
	// clearing mid-stream would race the remaining output back onto the grid.
	waitFor(t, "seq done", func() bool { return strings.Contains(plainView(s), "100") })
	waitFor(t, "history", func() bool { return s.ScrollbackLen() > 0 })
	s.Clear()
	if s.ScrollbackLen() != 0 {
		t.Fatalf("scrollback should be empty, len = %d", s.ScrollbackLen())
	}
	// The visible screen is wiped emulator-side — no stale seq output.
	waitFor(t, "screen wipe", func() bool {
		return !strings.Contains(plainView(s), "97") && !strings.Contains(plainView(s), "42")
	})
}

// BenchmarkSessionView documents the #803 cost profile: an unchanged grid
// serves the cached render; bumping the version forces the full emulator
// render an OutputMsg-driven frame previously paid for every terminal pane.
func BenchmarkSessionView(b *testing.B) {
	c := &collector{}
	s, err := StartSession("bench", "/bin/sh", b.TempDir(), 200, 60, nil, c.send)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(s.Close)
	for _, r := range "seq 1000\r" {
		s.SendKey(keyFor(r))
	}
	time.Sleep(500 * time.Millisecond) // let the grid fill

	b.Run("cached", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = s.View()
		}
	})
	b.Run("uncached", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			s.version.Add(1)
			_ = s.View()
		}
	})
}

// TestSessionResizeDebounce guards #804: a rapid resize burst (divider drag)
// applies the first size immediately and folds the rest into one trailing
// apply of the final size — intermediate sizes never reach the PTY/emulator.
func TestSessionResizeDebounce(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)

	s.Resize(100, 30) // leading edge: immediate
	if w := s.em.Width(); w != 100 {
		t.Fatalf("leading resize must apply immediately, width = %d", w)
	}
	s.Resize(96, 30)
	s.Resize(92, 30)
	s.Resize(88, 30)
	if w := s.em.Width(); w != 100 {
		t.Fatalf("burst resizes must defer, width = %d, want still 100", w)
	}
	waitFor(t, "trailing resize", func() bool { return s.em.Width() == 88 })

	// A lone resize after the quiet window is immediate again.
	time.Sleep(resizeQuiet + 20*time.Millisecond)
	s.Resize(80, 24)
	if w := s.em.Width(); w != 80 {
		t.Fatalf("post-quiet resize must apply immediately, width = %d", w)
	}
}

// BenchmarkSessionResizeApply documents the #804 cost of one applied resize
// (PTY SIGWINCH + emulator reflow) — what every divider-drag step paid before
// the debounce.
func BenchmarkSessionResizeApply(b *testing.B) {
	c := &collector{}
	s, err := StartSession("bench", "/bin/sh", b.TempDir(), 200, 60, nil, c.send)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(s.Close)
	for _, r := range "seq 500\r" {
		s.SendKey(keyFor(r))
	}
	time.Sleep(300 * time.Millisecond)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := 199 + i%2 // alternate so every call is a real change
		s.mu.Lock()
		s.applyResizeLocked(w, 60)
		s.mu.Unlock()
	}
}
