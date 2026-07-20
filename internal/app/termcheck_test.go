package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
)

// TestTermCheckIssues (#720): each deficiency produces exactly one specific
// entry; a fully capable environment produces none.
func TestTermCheckIssues(t *testing.T) {
	full := termCaps{kitty: true, profile: colorprofile.TrueColor, profileSeen: true}
	if got := termCheckIssues(full, false); len(got) != 0 {
		t.Fatalf("capable environment should report nothing, got %+v", got)
	}

	noKitty := termCaps{kitty: false, profile: colorprofile.TrueColor, profileSeen: true}
	got := termCheckIssues(noKitty, false)
	if len(got) != 1 || !strings.Contains(got[0].detail, "Kitty keyboard protocol") {
		t.Fatalf("missing kitty issue, got %+v", got)
	}

	if got := termCheckIssues(full, true); len(got) != 1 || !strings.Contains(got[0].title, "tmux") {
		t.Fatalf("missing tmux issue, got %+v", got)
	}

	dim := termCaps{kitty: true, profile: colorprofile.ANSI256, profileSeen: true}
	if got := termCheckIssues(dim, false); len(got) != 1 || !strings.Contains(got[0].detail, "ANSI256") {
		t.Fatalf("missing color issue, got %+v", got)
	}

	// An unreported profile must not be judged (bubbletea sent no
	// ColorProfileMsg yet — Unknown is not a deficiency claim).
	unseen := termCaps{kitty: true}
	if got := termCheckIssues(unseen, false); len(got) != 0 {
		t.Fatalf("unseen profile should not report, got %+v", got)
	}

	// Worst case stacks one issue per problem, chords first.
	worst := termCheckIssues(termCaps{profile: colorprofile.ANSI, profileSeen: true}, true)
	if len(worst) != 3 || !strings.Contains(worst[0].detail, "Kitty") {
		t.Fatalf("worst case should stack 3 issues, kitty first, got %+v", worst)
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

// TestRunTermCheckOpensReport: a deficient environment opens the floating
// report once; the verdict never re-fires.
func TestRunTermCheckOpensReport(t *testing.T) {
	m := sized(t, 100, 30)
	m.caps.kitty = false // deficient: warns unless the guard holds
	if cmd := m.runTermCheck(); cmd != nil {
		t.Fatal("free shell: the verdict should open the report, not retry")
	}
	if !m.caps.done {
		t.Fatal("verdict should mark done")
	}
	if !m.shell.IsOpen() {
		t.Fatal("deficient terminal should open the floating report")
	}
	m.shell.Close()
	if cmd := m.runTermCheck(); cmd != nil || m.shell.IsOpen() {
		t.Fatal("second run must not re-open the report")
	}
}

// TestRunTermCheckWaitsForBusyShell: an occupied modal surface defers the
// report to a retry tick instead of stealing the shell.
func TestRunTermCheckWaitsForBusyShell(t *testing.T) {
	m := sized(t, 100, 30)
	m.caps.kitty = false
	m.shell.Open() // e.g. the welcome tour / another prompt owns the surface
	if cmd := m.runTermCheck(); cmd == nil {
		t.Fatal("busy shell: the verdict should schedule a retry")
	}
	if m.caps.done {
		t.Fatal("a deferred verdict must stay pending")
	}
}

// TestRunTermCheckSilentWhenCapable: no issues → no shell, no retry.
func TestRunTermCheckSilentWhenCapable(t *testing.T) {
	m := sized(t, 100, 30)
	m.caps = termCaps{kitty: true, profile: colorprofile.TrueColor, profileSeen: true}
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "xterm-ghostty")
	if cmd := m.runTermCheck(); cmd != nil {
		t.Fatal("capable environment should not retry")
	}
	if m.shell.IsOpen() {
		t.Fatal("capable environment should not open the report")
	}
	if !m.caps.done {
		t.Fatal("verdict should mark done")
	}
}
