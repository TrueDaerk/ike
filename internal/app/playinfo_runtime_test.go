package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestPlayDurationFormat (#2776): `<1 ms`, whole milliseconds, one decimal of
// seconds.
func TestPlayDurationFormat(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                       "<1 ms",
		999 * time.Microsecond:  "<1 ms",
		38 * time.Millisecond:   "38 ms",
		999 * time.Millisecond:  "999 ms",
		1200 * time.Millisecond: "1.2 s",
	} {
		if got := playDuration(d); got != want {
			t.Errorf("playDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

// TestPlayInfoRowShowsRuntimeAndSize (#2776): after a run the summary carries
// the result size and the wall clock; a slow run paints the time in Warning.
func TestPlayInfoRowShowsRuntimeAndSize(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	m = setProgram(m, ".")
	row := ansi.Strip(m.playInfoRow(240))
	if !strings.Contains(row, "Result — 1 value(s) · "+humanBytes(int64(m.play.result.Size()))+" · ") ||
		!strings.Contains(row, " ms") {
		t.Fatalf("summary should carry size and runtime, got %q", row)
	}
	m.play.elapsed = 1200 * time.Millisecond
	warn := lipgloss.NewStyle().Foreground(m.pal().Warning)
	if row := m.playInfoRow(240); !strings.Contains(row, warn.Render("1.2 s")) {
		t.Errorf("a slow run should render its time in Warning, got %q", row)
	}
	m.play.elapsed = 38 * time.Millisecond
	if row := m.playInfoRow(240); strings.Contains(row, warn.Render("38 ms")) {
		t.Errorf("a fast run must not warn, got %q", row)
	}
	// A narrow pane still keeps input and summary, runtime included.
	narrow := ansi.Strip(m.playInfoRow(70))
	if !strings.Contains(narrow, "Input:") || !strings.Contains(narrow, "38 ms") {
		t.Errorf("narrow row lost the meta data: %q", narrow)
	}
}

// TestPlayHintsHideWhileTyping (#2776): a keystroke in the query line hides the
// key hints; the idle tick of the latest keystroke brings them back, a stale
// one does not.
func TestPlayHintsHideWhileTyping(t *testing.T) {
	m := openJQ(t, playApp(t, `{"a":1}`))
	m = setProgram(m, ".")
	if !strings.Contains(ansi.Strip(m.playInfoRow(300)), playHelpHint) {
		t.Fatal("hints should show before typing")
	}
	tm, _ := m.updatePlaygroundKey(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = tm.(Model)
	stale := m.play.hgen
	tm, _ = m.updatePlaygroundKey(tea.KeyPressMsg{Code: 'b', Text: "b"})
	m = tm.(Model)
	if !m.play.typing || strings.Contains(ansi.Strip(m.playInfoRow(300)), playHelpHint) {
		t.Fatalf("hints should hide while typing, got %q", ansi.Strip(m.playInfoRow(300)))
	}
	tm, _ = m.Update(playHintIdleMsg{st: m.play, gen: stale})
	m = tm.(Model)
	if !m.play.typing {
		t.Fatal("a superseded idle tick must not bring the hints back")
	}
	tm, _ = m.Update(playHintIdleMsg{st: m.play, gen: m.play.hgen})
	m = tm.(Model)
	if !strings.Contains(ansi.Strip(m.playInfoRow(300)), playHelpHint) {
		t.Errorf("hints should return after idle, got %q", ansi.Strip(m.playInfoRow(300)))
	}
}
