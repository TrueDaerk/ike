package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"ike/internal/ui"
)

// termcheck.go probes what the hosting terminal actually delivers (#720) and
// presents the deficiencies in one centered floating report at startup,
// replacing the blanket "⚠ terminal-dependent" chord labels: bubbletea
// reports the Kitty keyboard protocol handshake (tea.KeyboardEnhancementsMsg)
// and the color profile (tea.ColorProfileMsg); tmux is visible in the
// environment. A terminal without the Kitty protocol never answers the query,
// so a grace tick after startup treats silence as "unsupported".

// termCheckGrace is how long after the color-profile report the verdict is
// drawn. The handshake races the first frames; every answering terminal
// responds well within this.
const termCheckGrace = 3 * time.Second

// A verdict due while the floating shell is occupied (welcome tour, crash
// recovery, onboarding) used to re-poll on a 2s tick — capped at 30 rounds
// (#2163), but still ~10 idle passes per 10s for the first minute of every
// session parked in a modal, the loudest source in the #2402 idle telemetry.
// The wait is event-driven now: the deferred verdict is marked pending and
// drained from Update's settled pass — closing the blocking modal takes a
// message anyway, so the report opens on that very pass and the wait itself
// costs zero wakes (and no longer needs a give-up cap).

// termReportWidth caps the report body: long lines wrap here.
const termReportWidth = 66

// termCheckMsg draws the verdict when the grace period elapses. gen names the
// model that armed it (#2194): a project switch rebuilds the model while the
// grace tick sleeps, and the fresh model's zero-valued caps would read a
// Kitty-capable terminal as lacking the protocol.
type termCheckMsg struct{ gen int64 }

// termCaps accumulates the capability reports until the verdict.
type termCaps struct {
	// kitty is true once the terminal answered the Kitty keyboard protocol
	// query with any flag set (key disambiguation at minimum).
	kitty bool
	// profile is the color profile bubbletea detected; profileSeen guards
	// against judging before the report arrived.
	profile     colorprofile.Profile
	profileSeen bool
	// scheduled: the grace tick is on its way (set on the profile report).
	scheduled bool
	// done: the verdict fired; late reports must not re-open the shell.
	done bool
	// pending: the verdict was due while a modal owned the shell; the settled
	// pass draws it once the surface frees up (no retry tick, #2402).
	pending bool
}

// termCheckTick schedules the verdict once the grace period elapses. It stamps
// the arming model's generation, so a tick outliving a project switch retires
// on arrival.
func termCheckTick(gen int64) tea.Cmd {
	return tea.Tick(termCheckGrace, func(time.Time) tea.Msg { return termCheckMsg{gen: gen} })
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

// termIssue is one detected deficiency: a short headline and the full
// explanation with the fix.
type termIssue struct {
	title  string
	detail string
}

// termCheckIssues composes the deficiency report for the detected state.
// Order: most impactful first (chords beat colors).
func termCheckIssues(caps termCaps, tmux bool) []termIssue {
	var out []termIssue
	if !caps.kitty {
		out = append(out, termIssue{
			title: "Keyboard shortcuts will not work",
			detail: "This terminal doesn't speak the Kitty keyboard protocol, so most Cmd/Alt chords never reach IKE. " +
				"Use a supporting terminal: Ghostty, kitty, WezTerm, foot, iTerm2 3.5+ or Alacritty. " +
				"Until then, the command palette (esc esc) and the menu bar still reach every action.",
		})
	}
	if tmux {
		out = append(out, termIssue{
			title: "Running inside tmux",
			detail: "tmux forwards modifier chords only with extended keys enabled — add to tmux.conf: set -s extended-keys on. " +
				"ctrl+tab is consumed by tmux and never arrives.",
		})
	}
	if caps.profileSeen && caps.profile != colorprofile.Unknown && caps.profile < colorprofile.TrueColor {
		// colorprofile orders Unknown < NoTTY < ASCII < ANSI < ANSI256 < TrueColor.
		out = append(out, termIssue{
			title: "Limited color support",
			detail: fmt.Sprintf("The terminal reports %s — IKE's themes are designed for true color. "+
				"Most terminals enable it via COLORTERM=truecolor (tmux: set -ga terminal-features ',*:RGB').", caps.profile),
		})
	}
	return out
}

// runTermCheck draws the verdict once. Deficiencies open a centered floating
// report the user must dismiss (esc) — a bottom-corner toast proved too easy
// to miss and truncated the fixes. When another modal owns the shell (tour,
// recovery, onboarding), the verdict parks as pending; drainTermCheck opens
// it on the settled pass that frees the surface.
func (m *Model) runTermCheck() {
	if m.caps.done {
		return
	}
	issues := termCheckIssues(m.caps, insideTmux(os.Getenv))
	if len(issues) == 0 {
		m.caps.done = true
		return
	}
	if m.shell.IsOpen() || m.tourOpen() || m.recoveryOpen() || m.onboardingOpen() ||
		m.themePickOpen() || m.toolchainInfoOpen() {
		m.caps.pending = true
		return
	}
	m.caps.done, m.caps.pending = true, false
	body := m.termReportBody(issues)
	m.shell.SetContent(ui.ModelContent{Heading: "TERMINAL CHECK", Body: func() string { return body }})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// drainTermCheck re-runs a deferred verdict from Update's settled pass. The
// cost while nothing is pending — the overwhelmingly common state — is one
// bool check per message.
func (m *Model) drainTermCheck() {
	if m.caps.pending && !m.caps.done {
		m.runTermCheck()
	}
}

// termReportBody lays the issues out for the floating shell: warning-colored
// headline, wrapped detail, one blank line between issues.
func (m *Model) termReportBody(issues []termIssue) string {
	pal := m.pal()
	title := lipgloss.NewStyle().Foreground(pal.Warning).Bold(true)
	detail := lipgloss.NewStyle().Foreground(pal.Foreground).Width(termReportWidth)
	parts := make([]string, 0, len(issues))
	for _, is := range issues {
		parts = append(parts, title.Render("▲ "+is.title)+"\n"+detail.Render(is.detail))
	}
	return strings.Join(parts, "\n\n")
}
