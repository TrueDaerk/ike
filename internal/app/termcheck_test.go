package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
)

// TestTermCheckWarnings (#720): each deficiency produces exactly one specific
// message; a fully capable environment produces none.
func TestTermCheckWarnings(t *testing.T) {
	full := termCaps{kitty: true, profile: colorprofile.TrueColor, profileSeen: true}
	if got := termCheckWarnings(full, false); len(got) != 0 {
		t.Fatalf("capable environment should warn nothing, got %q", got)
	}

	noKitty := termCaps{kitty: false, profile: colorprofile.TrueColor, profileSeen: true}
	got := termCheckWarnings(noKitty, false)
	if len(got) != 1 || !strings.Contains(got[0], "Kitty keyboard protocol") {
		t.Fatalf("missing kitty warning, got %q", got)
	}

	if got := termCheckWarnings(full, true); len(got) != 1 || !strings.Contains(got[0], "tmux") {
		t.Fatalf("missing tmux warning, got %q", got)
	}

	dim := termCaps{kitty: true, profile: colorprofile.ANSI256, profileSeen: true}
	if got := termCheckWarnings(dim, false); len(got) != 1 || !strings.Contains(got[0], "ANSI256") {
		t.Fatalf("missing color warning, got %q", got)
	}

	// An unreported profile must not be judged (bubbletea sent no
	// ColorProfileMsg yet — Unknown is not a deficiency claim).
	unseen := termCaps{kitty: true}
	if got := termCheckWarnings(unseen, false); len(got) != 0 {
		t.Fatalf("unseen profile should not warn, got %q", got)
	}

	// Worst case stacks one warning per problem, chords first.
	worst := termCheckWarnings(termCaps{profile: colorprofile.ANSI, profileSeen: true}, true)
	if len(worst) != 3 || !strings.Contains(worst[0], "Kitty") {
		t.Fatalf("worst case should stack 3 warnings, kitty first, got %q", worst)
	}
}

// TestInsideTmux covers the environment shapes: $TMUX, TERM prefixes, plain.
func TestInsideTmux(t *testing.T) {
	env := func(vals map[string]string) func(string) string {
		return func(k string) string { return vals[k] }
	}
	if !insideTmux(env(map[string]string{"TMUX": "/tmp/tmux-501/default,123,0"})) {
		t.Fatal("$TMUX set should detect tmux")
	}
	if !insideTmux(env(map[string]string{"TERM": "tmux-256color"})) {
		t.Fatal("TERM=tmux-* should detect tmux")
	}
	if !insideTmux(env(map[string]string{"TERM": "screen"})) {
		t.Fatal("TERM=screen should detect tmux/screen")
	}
	if insideTmux(env(map[string]string{"TERM": "xterm-ghostty"})) {
		t.Fatal("plain terminal misdetected as tmux")
	}
}

// TestRunTermCheckOnce: the verdict fires once; late grace ticks stay silent.
func TestRunTermCheckOnce(t *testing.T) {
	m := sized(t, 100, 30)
	m.caps.kitty = false // deficient: would warn on every run without the guard
	m.runTermCheck()
	if !m.caps.done {
		t.Fatal("verdict should mark done")
	}
	first := len(m.host.DrainNotifications())
	if first == 0 {
		t.Fatal("deficient terminal should notify")
	}
	m.runTermCheck()
	if n := len(m.host.DrainNotifications()); n != 0 {
		t.Fatalf("second run must not re-notify, got %d", n)
	}
}
