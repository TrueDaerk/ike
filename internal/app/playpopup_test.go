package app

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// playpopup_test.go covers the modal order over the focused playground
// (#3011): a popup opened from inside the mode — the Help overlay, the
// language cheatsheet, the saved-filter picker — takes the key before the
// playground, so esc closes the popup and only the next esc the playground.

// f1Key is the Help overlay's key.
func f1Key() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyF1} }

// runeKey is a plain printable key press.
func runeKey(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

// press feeds one key through the root Update and drains its commands.
func playPopupKey(t *testing.T, m Model, k tea.KeyPressMsg) Model {
	t.Helper()
	tm, cmd := m.Update(k)
	return drainCmd(asModel(tm), cmd)
}

// playSnapshot is the playground state a popup must leave untouched.
type playSnapshot struct {
	program string
	cur     int
	buf     bool
	result  string
}

func snapPlay(m Model) playSnapshot {
	s := m.play
	return playSnapshot{program: s.program.Text, cur: s.program.Cur, buf: s.bufFocus, result: s.resultEd.View()}
}

// openPlayWithProgram opens the jq playground with a program typed and the
// caret mid-line, so a leaked key would visibly change it.
func openPlayWithProgram(t *testing.T) Model {
	t.Helper()
	m := openJQ(t, dismissOnboarding(playApp(t, `{"foo":[1,2,3]}`)))
	m = setProgram(m, ".foo[]")
	m.play.program.Cur = 3
	return m
}

// helpThenEscEsc drives F1, esc, esc and asserts the overlay closes first and
// the playground only on the second esc.
func helpThenEscEsc(t *testing.T, m Model) {
	t.Helper()
	before := snapPlay(m)
	m = playPopupKey(t, m, f1Key())
	if !m.shell.IsOpen() || m.shell.Content() != m.help {
		t.Fatal("F1 must open the Help overlay over the focused playground")
	}
	m = playPopupKey(t, m, escKey())
	if m.shell.IsOpen() {
		t.Fatal("esc must close the Help overlay")
	}
	if !m.playOpen() {
		t.Fatal("esc over the Help overlay must not close the playground (#3011)")
	}
	if got := snapPlay(m); got != before {
		t.Fatalf("the playground changed under the overlay: %+v, want %+v", got, before)
	}
	if !m.playFocused() {
		t.Fatal("the playground must keep the focus after the overlay closed")
	}
	m = playPopupKey(t, m, escKey())
	if m.playOpen() {
		t.Fatal("the next esc must close the playground")
	}
}

func TestPlaygroundEscClosesHelpOverlayFirst(t *testing.T) {
	helpThenEscEsc(t, openPlayWithProgram(t))
}

func TestPlaygroundEscClosesHelpOverlayFirstFromResultBuffer(t *testing.T) {
	m := openPlayWithProgram(t)
	m.play.setBufFocus(true)
	helpThenEscEsc(t, m)
}

func TestPlaygroundEscClosesHelpOverlayFirstFromDetachedResult(t *testing.T) {
	m := splitJQ(t, `{"foo":[1,2,3]}`, ".foo[]")
	m.activeWS().Panes.SetFocused(m.play.resultKey)
	m.play.setBufFocus(true)
	if !m.playFocused() {
		t.Fatal("the detached result pane must hold the focus for this test to mean anything")
	}
	helpThenEscEsc(t, m)
}

// TestPlaygroundHelpOverlayKeysDoNotLeak: everything the overlay owns —
// filter typing, q and ? as filter letters, backspace, scrolling — stays in
// the overlay; the query line is byte-identical afterwards.
func TestPlaygroundHelpOverlayKeysDoNotLeak(t *testing.T) {
	m := openPlayWithProgram(t)
	before := snapPlay(m)
	m = playPopupKey(t, m, f1Key())
	for _, k := range []tea.KeyPressMsg{
		runeKey('f'), runeKey('q'), runeKey('?'),
		{Code: tea.KeyBackspace},
		{Code: tea.KeyDown}, {Code: tea.KeyPgDown}, {Code: tea.KeyUp},
		{Code: 'j'}, {Code: 'k'},
	} {
		m = playPopupKey(t, m, k)
		if !m.shell.IsOpen() {
			t.Fatalf("%q must not close the filtered Help overlay", k.String())
		}
	}
	if got := snapPlay(m); got != before {
		t.Fatalf("overlay keys leaked into the playground: %+v, want %+v", got, before)
	}
	// With an active filter the first esc clears it, the second closes the
	// overlay, and only the third reaches the playground.
	m = playPopupKey(t, m, escKey())
	if !m.shell.IsOpen() {
		t.Fatal("the first esc over an active filter must only clear it")
	}
	m = playPopupKey(t, m, escKey())
	if m.shell.IsOpen() || !m.playOpen() {
		t.Fatal("the second esc must close the overlay and leave the playground open")
	}
	if got := snapPlay(m); got != before {
		t.Fatalf("the playground changed under the overlay: %+v, want %+v", got, before)
	}
	m = playPopupKey(t, m, escKey())
	if m.playOpen() {
		t.Fatal("the third esc must close the playground")
	}
}

// TestPlaygroundUnfilteredHelpQClosesOverlayOnly: q and ? dismiss an
// unfiltered overlay — and only it.
func TestPlaygroundUnfilteredHelpQClosesOverlayOnly(t *testing.T) {
	for _, k := range []tea.KeyPressMsg{runeKey('q'), runeKey('?'), f1Key()} {
		m := openPlayWithProgram(t)
		before := snapPlay(m)
		m = playPopupKey(t, m, f1Key())
		m = playPopupKey(t, m, k)
		if m.shell.IsOpen() {
			t.Fatalf("%q must close the unfiltered overlay", k.String())
		}
		if got := snapPlay(m); !m.playOpen() || got != before {
			t.Fatalf("%q leaked into the playground: %+v, want %+v", k.String(), got, before)
		}
	}
}

// TestPlaygroundEscClosesPalettePickersFirst: the language cheatsheet and the
// saved-filter picker are locked palette modes, which already precede the
// playground — pinned here so the order stays.
func TestPlaygroundEscClosesPalettePickersFirst(t *testing.T) {
	open := map[string]func(m *Model){
		"cheatsheet": func(m *Model) { m.openPlayCheatsheet(m.play.dialect, "") },
		"filters": func(m *Model) {
			*m = saveFilter(t, *m, "nums", false)
			_ = m.openPlayFilterPicker(m.play.dialect, false)
		},
	}
	for name, fn := range open {
		t.Run(name, func(t *testing.T) {
			m := openPlayWithProgram(t)
			before := snapPlay(m)
			fn(&m)
			if !m.palette.IsOpen() {
				t.Fatal("the picker must open")
			}
			m = atClock(m, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			m = playPopupKey(t, m, escKey())
			if m.palette.IsOpen() {
				t.Fatal("esc must close the picker")
			}
			if got := snapPlay(m); !m.playOpen() || got != before {
				t.Fatalf("esc over the picker reached the playground: %+v, want %+v", got, before)
			}
			m = playPopupKey(t, m, escKey())
			if m.playOpen() {
				t.Fatal("the next esc must close the playground")
			}
		})
	}
}
