package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"ike/internal/host"
)

// termcheck.go probes what the hosting terminal actually delivers (#720) and
// raises one specific warning toast per deficiency at startup, replacing the
// blanket "⚠ terminal-dependent" chord labels: bubbletea reports the Kitty
// keyboard protocol handshake (tea.KeyboardEnhancementsMsg) and the color
// profile (tea.ColorProfileMsg); tmux is visible in the environment. A
// terminal without the Kitty protocol never answers the query, so a grace
// tick after startup treats silence as "unsupported".

// termCheckGrace is how long after Init the verdict is drawn. The handshake
// races the first frames; every answering terminal responds well within this.
const termCheckGrace = 3 * time.Second

// termCheckMsg draws the verdict when the grace period elapses.
type termCheckMsg struct{}

// termCaps accumulates the capability reports until the verdict.
type termCaps struct {
	// kitty is true once the terminal answered the Kitty keyboard protocol
	// query with any flag set (key disambiguation at minimum).
	kitty bool
	// profile is the color profile bubbletea detected; profileSeen guards
	// against judging before the report arrived.
	profile     colorprofile.Profile
	profileSeen bool
	// scheduled: the grace tick is on its way (set on the first size report).
	scheduled bool
	// done: the verdict fired; late reports must not re-toast.
	done bool
}

// termCheckTick schedules the verdict.
func termCheckTick() tea.Cmd {
	return tea.Tick(termCheckGrace, func(time.Time) tea.Msg { return termCheckMsg{} })
}

// insideTmux reports whether IKE runs under tmux (or screen), which consumes
// some chords (ctrl+tab) and forwards extended keys only when configured.
func insideTmux(getenv func(string) string) bool {
	if getenv("TMUX") != "" {
		return true
	}
	term := getenv("TERM")
	return strings.HasPrefix(term, "tmux") || strings.HasPrefix(term, "screen")
}

// termCheckWarnings composes the deficiency messages for the detected state.
// Order: most impactful first (chords beat colors).
func termCheckWarnings(caps termCaps, tmux bool) []string {
	var out []string
	if !caps.kitty {
		out = append(out, "Terminal doesn't report modifier chords (no Kitty keyboard protocol) — most Cmd/Alt shortcuts won't arrive. Use Ghostty, kitty, WezTerm, foot, iTerm2 3.5+ or Alacritty; the command palette and menu still reach everything.")
	}
	if tmux {
		out = append(out, "Running inside tmux: chords pass through only with extended keys enabled (tmux.conf: set -s extended-keys on); ctrl+tab never arrives.")
	}
	if caps.profileSeen && caps.profile != colorprofile.Unknown && caps.profile < colorprofile.TrueColor {
		// colorprofile orders Unknown < NoTTY < ASCII < ANSI < ANSI256 < TrueColor.
		out = append(out, fmt.Sprintf("Terminal reports %s color support — themes are designed for true color (COLORTERM=truecolor).", caps.profile))
	}
	return out
}

// runTermCheck draws the verdict once: every deficiency becomes one warning
// toast (and history entry) via the host notifier.
func (m *Model) runTermCheck() {
	if m.caps.done {
		return
	}
	m.caps.done = true
	for _, w := range termCheckWarnings(m.caps, insideTmux(os.Getenv)) {
		m.host.Notify(host.Warn, w)
	}
}
