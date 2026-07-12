package terminal

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/vt"
)

// TestOSCTitleWithMultiByteRuneLeavesGridClean is the regression test for
// #561: an OSC 0 title containing a rune whose UTF-8 encoding carries the
// byte 0x9C (✳ = U+2733 = E2 9C B3) must set the title in full and must not
// leak any of the title text into the screen grid as ghost cells.
func TestOSCTitleWithMultiByteRuneLeavesGridClean(t *testing.T) {
	em := vt.NewSafeEmulator(40, 4)
	defer em.Close()

	var title string
	em.SetCallbacks(vt.Callbacks{Title: func(s string) { title = s }})

	if _, err := em.WriteString("\x1b]0;✳ Claude Code\a"); err != nil {
		t.Fatalf("write OSC title: %v", err)
	}

	if title != "✳ Claude Code" {
		t.Errorf("title = %q, want %q", title, "✳ Claude Code")
	}
	if got := strings.TrimSpace(em.Render()); got != "" {
		t.Errorf("grid not empty after OSC title, rendered %q", got)
	}
}

// TestOSCTitleStillTerminatesOnEscBackslash guards the fix's scope: the
// two-byte ESC \ string terminator (and BEL, covered above) must keep working
// after 0x9C is downgraded to OSC payload.
func TestOSCTitleStillTerminatesOnEscBackslash(t *testing.T) {
	em := vt.NewSafeEmulator(40, 4)
	defer em.Close()

	var title string
	em.SetCallbacks(vt.Callbacks{Title: func(s string) { title = s }})

	if _, err := em.WriteString("\x1b]0;hello\x1b\\after"); err != nil {
		t.Fatalf("write OSC title: %v", err)
	}

	if title != "hello" {
		t.Errorf("title = %q, want %q", title, "hello")
	}
	if got := strings.TrimSpace(em.Render()); got != "after" {
		t.Errorf("text after ST not printed, rendered %q, want %q", got, "after")
	}
}
