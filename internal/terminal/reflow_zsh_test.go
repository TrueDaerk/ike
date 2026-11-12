package terminal

import (
	"strings"
	"testing"
	"time"
)

// TestReflowRapidCyclesZsh guards #953 against a real interactive shell: zsh
// redraws its prompt on every SIGWINCH, and rapid debounced width cycles must
// not let those in-flight redraw bytes (or a mid-redraw cursor position)
// corrupt the reflowed history.
func TestReflowRapidCyclesZsh(t *testing.T) {
	c := &collector{}
	s, err := StartSession("terminal", "/bin/zsh", t.TempDir(), 110, 20, nil, c.send)
	if err != nil {
		t.Skip("zsh unavailable:", err)
	}
	t.Cleanup(s.Close)
	// Every width below gets its own trailing apply within milliseconds
	// instead of the production window (#3136).
	setResizeQuiet(s, 5*time.Millisecond)

	run := func(cmd string) {
		for _, r := range cmd + "\r" {
			s.SendKey(keyFor(r))
		}
	}
	// settle waits until zsh has run a marker command typed after everything
	// before it — the earlier output and the SIGWINCH redraws are in. The
	// marker is built from two printf args, so the echoed command line never
	// contains it.
	settle := func(tag string) {
		t.Helper()
		run(`printf 'zz-%s\n' ` + tag)
		waitFor(t, "marker "+tag, func() bool { return strings.Contains(plainView(s), "zz-"+tag) })
	}

	run(`printf 'Name          Status  User  File\nmariadb       started geant /Library/LaunchAgents/homebrew.mxcl.mariadb.plist\nphp           started geant /Library/LaunchAgents/homebrew.mxcl.php.plist\n'`)
	waitFor(t, "table output", func() bool {
		v := plainView(s)
		return strings.Contains(v, "mariadb       started geant /Library") &&
			strings.Count(v, "php") >= 2
	})
	settle("before")

	dump := func(tag string) string {
		total := s.ScrollbackLen() + 20
		var b strings.Builder
		for i := 0; i < total; i++ {
			b.WriteString(s.LineText(i))
			b.WriteString("\n")
		}
		t.Logf("=== %s ===\n%s", tag, b.String())
		return b.String()
	}
	dump("before")

	// Real app-layer path: debounced Resize applied from the timer
	// goroutine, a burst of intermediate widths like a drag — each one
	// applied while zsh's redraw for the previous one is still in flight.
	for _, w := range []int{104, 90, 83, 70, 77, 95, 60, 78, 102, 110, 68, 55, 91, 110} {
		s.Resize(w, 20)
		waitResized(t, s, w, 20)
	}
	settle("cycled")
	s.Resize(110, 20)
	waitResized(t, s, 110, 20)
	settle("final")

	final := dump("final")
	for _, bad := range []string{"plistphp", "plistName", "Filemariadb", "plistgeant"} {
		if strings.Contains(final, bad) {
			t.Fatalf("merged lines detected (%q)", bad)
		}
	}
	if !strings.Contains(final, "php           started geant /Library/LaunchAgents/homebrew.mxcl.php.plist") {
		t.Fatal("php output row lost")
	}
}
