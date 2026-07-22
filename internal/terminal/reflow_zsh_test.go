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

	cmd := `printf 'Name          Status  User  File\nmariadb       started geant /Library/LaunchAgents/homebrew.mxcl.mariadb.plist\nphp           started geant /Library/LaunchAgents/homebrew.mxcl.php.plist\n'`
	for _, r := range cmd + "\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "table output", func() bool {
		v := plainView(s)
		return strings.Contains(v, "mariadb       started geant /Library") &&
			strings.Count(v, "php") >= 2
	})
	time.Sleep(300 * time.Millisecond)

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

	// Real app-layer path: debounced Resize (leading apply + trailing apply
	// from a timer goroutine), bursts of intermediate widths like a drag.
	for _, w := range []int{104, 90, 83, 70, 77, 95, 60, 78, 102, 110, 68, 55, 91, 110} {
		s.Resize(w, 20)
		time.Sleep(60 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond)
	s.Resize(110, 20)
	time.Sleep(700 * time.Millisecond)

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
