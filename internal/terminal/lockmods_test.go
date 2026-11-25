package terminal

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/vt"
)

// encodeKey returns the bytes the emulator emits for a press forwarded the
// way Model.Update does for keys without a dedicated chord encoding.
func encodeKey(t *testing.T, k tea.KeyPressMsg) string {
	t.Helper()
	if ev, ok := motionKey(k); ok {
		return emit(t, []vt.KeyPressEvent{ev})
	}
	if seq, ok := modifiedCursorSeq(k); ok {
		return seq
	}
	return emit(t, toVTKeys(k))
}

func emit(t *testing.T, evs []vt.KeyPressEvent) string {
	t.Helper()
	em := vt.NewEmulator(80, 24)
	defer em.Close()
	out := make(chan string, 1)
	go func() {
		buf := make([]byte, 256)
		n, _ := em.Read(buf)
		out <- string(buf[:n])
	}()
	for _, ev := range evs {
		em.SendKey(ev)
	}
	select {
	case s := <-out:
		return s
	case <-time.After(200 * time.Millisecond):
		return ""
	}
}

func TestLockModifiersAreIgnoredForForwardedKeys(t *testing.T) {
	keys := []tea.KeyPressMsg{
		{Code: tea.KeyLeft}, {Code: tea.KeyRight}, {Code: tea.KeyUp}, {Code: tea.KeyDown},
		{Code: tea.KeyHome}, {Code: tea.KeyEnd}, {Code: tea.KeyPgUp}, {Code: tea.KeyPgDown},
		{Code: tea.KeyDelete}, {Code: tea.KeyInsert}, {Code: tea.KeyF1}, {Code: tea.KeyF5},
		{Code: tea.KeyLeft, Mod: tea.ModCtrl},
		{Code: tea.KeyLeft, Mod: tea.ModAlt},
		{Code: tea.KeyUp, Mod: tea.ModShift},
		{Code: 'c', Mod: tea.ModCtrl},
	}
	locks := []tea.KeyMod{tea.ModNumLock, tea.ModCapsLock, tea.ModNumLock | tea.ModCapsLock}
	for _, k := range keys {
		want := encodeKey(t, k)
		if want == "" {
			t.Fatalf("%v: no bytes without lock modifier", k)
		}
		for _, l := range locks {
			locked := k
			locked.Mod |= l
			if got := encodeKey(t, locked); got != want {
				t.Fatalf("%v with lock %v: got %q, want %q", k, l, got, want)
			}
		}
	}
}

func TestLockModifiersKeepTypedText(t *testing.T) {
	k := tea.KeyPressMsg{Code: 'a', ShiftedCode: 'A', Mod: tea.ModCapsLock | tea.ModNumLock, Text: "A"}
	if got := encodeKey(t, k); got != "A" {
		t.Fatalf("got %q, want %q", got, "A")
	}
}

func TestCtrlCWithLockIsInterrupt(t *testing.T) {
	k := toVTKeys(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl | tea.ModNumLock})
	if len(k) != 1 || k[0].Mod != vt.ModCtrl {
		t.Fatalf("got %#v", k)
	}
}
