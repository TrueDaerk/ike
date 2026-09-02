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

// freeShell closes whatever first-start modal sized() left on the floating
// shell (the LSP onboarding opens on machines where its Init command finds
// install candidates), so termcheck tests start from a free surface.
func freeShell(m *Model) {
	m.onboarding = nil
	m.shell.Close()
}

// TestRunTermCheckOpensReport: a deficient environment opens the floating
// report once; the verdict never re-fires.
func TestRunTermCheckOpensReport(t *testing.T) {
	m := sized(t, 100, 30)
	freeShell(&m)
	m.caps.kitty = false // deficient: warns unless the guard holds
	m.runTermCheck()
	if !m.caps.done {
		t.Fatal("verdict should mark done")
	}
	if !m.shell.IsOpen() {
		t.Fatal("deficient terminal should open the floating report")
	}
	m.shell.Close()
	m.runTermCheck()
	if m.shell.IsOpen() {
		t.Fatal("second run must not re-open the report")
	}
}

// TestRunTermCheckWaitsForBusyShell (#2402): an occupied modal surface parks
// the report as pending — no retry tick, no wake — and the settled-pass drain
// opens it once the surface frees up.
func TestRunTermCheckWaitsForBusyShell(t *testing.T) {
	m := sized(t, 100, 30)
	freeShell(&m)
	m.caps.kitty = false
	m.shell.Open() // e.g. the welcome tour / another prompt owns the surface
	m.runTermCheck()
	if m.caps.done {
		t.Fatal("a deferred verdict must stay pending")
	}
	if !m.caps.pending {
		t.Fatal("busy shell: the verdict should mark itself pending")
	}
	// Still busy: the drain must keep waiting without giving up.
	m.drainTermCheck()
	if m.caps.done {
		t.Fatal("drain must not steal an occupied shell")
	}
	m.shell.Close()
	m.drainTermCheck()
	if !m.caps.done || !m.shell.IsOpen() {
		t.Fatal("freed shell: the drain should open the deferred report")
	}
}

// TestRunTermCheckSilentWhenCapable: no issues → no shell, nothing pending.
func TestRunTermCheckSilentWhenCapable(t *testing.T) {
	m := sized(t, 100, 30)
	freeShell(&m)
	m.caps = termCaps{kitty: true, profile: colorprofile.TrueColor, profileSeen: true}
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "xterm-ghostty")
	m.runTermCheck()
	if m.shell.IsOpen() {
		t.Fatal("capable environment should not open the report")
	}
	if !m.caps.done {
		t.Fatal("verdict should mark done")
	}
	if m.caps.pending {
		t.Fatal("capable environment must not park a pending verdict")
	}
}
