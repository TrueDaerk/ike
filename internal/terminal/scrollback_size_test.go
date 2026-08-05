package terminal

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestScrollbackBoundConfigured: new sessions are bounded by the process-wide
// scrollback default (terminal.scrollback_lines, #1545), and lowering the
// bound on a live session trims history forward.
func TestScrollbackBoundConfigured(t *testing.T) {
	old := defaultScrollback.Load()
	t.Cleanup(func() { defaultScrollback.Store(old) })
	SetDefaultScrollbackLines(120)

	s := NewPipeSession("pipe-sb", 80, 24, func(tea.Msg) {})
	t.Cleanup(s.Close)

	var buf strings.Builder
	for i := 0; i < 500; i++ {
		fmt.Fprintf(&buf, "line %d\r\n", i)
	}
	s.FeedBytes([]byte(buf.String()))
	waitFor(t, "scrollback filled to the bound", func() bool { return s.ScrollbackLen() >= 120 })
	if n := s.ScrollbackLen(); n > 120 {
		t.Fatalf("scrollback should be bounded to 120 lines, got %d", n)
	}

	// Live lowering (config reload) trims forward.
	s.SetScrollbackSize(50)
	s.FeedBytes([]byte("more\r\n"))
	waitFor(t, "scrollback trimmed to the new bound", func() bool { return s.ScrollbackLen() <= 50 })
}

// TestSetDefaultScrollbackLinesIgnoresNonPositive: 0 or negative values keep
// the current bound instead of resetting it.
func TestSetDefaultScrollbackLinesIgnoresNonPositive(t *testing.T) {
	old := defaultScrollback.Load()
	t.Cleanup(func() { defaultScrollback.Store(old) })

	SetDefaultScrollbackLines(300)
	SetDefaultScrollbackLines(0)
	SetDefaultScrollbackLines(-5)
	if got := defaultScrollback.Load(); got != 300 {
		t.Fatalf("non-positive values should be ignored, got %d", got)
	}
}
