// Package keydoctor is the in-app keymap doctor (#2080): a full-screen
// overlay running the terminal reality probe (0081/10) inside the live IKE
// session. While it is open the app hands it every raw key and mouse nav
// button before keymap resolution — the same matching `cmd/keyprobe` does,
// via the shared keymap.ProbeSession — and a finished run is offered for
// persistence as this terminal's reachability override set.
//
// Its second job needs no probing (#2161): the dead-binding report audits the
// active keymap against this platform and terminal, lists every chord that
// cannot arrive here with the reason, and offers a conflict-free rebind per
// finding (analysis.go, report.go).
package keydoctor

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/keymap"
	"ike/internal/theme"
)

// ResultMsg is emitted when the overlay closes after a run: Save asks the
// app to persist the results as the terminal's override set; a discarded run
// carries Save=false and must leave the store untouched. Review additionally
// asks for the dead-binding report (#2161) — the saved verdicts install
// before it opens, so the audit judges against measured truth.
type ResultMsg struct {
	Terminal string
	Results  []keymap.ProbeResult
	Save     bool
	Review   bool
}

// phase is the overlay's state: probing matches raw keys; summary interprets
// keys normally again (probing has stopped) to decide the run's fate; report
// is the dead-binding audit of the active keymap (#2161), which presses no
// keys at all and can be opened on its own.
type phase int

const (
	probing phase = iota
	summary
	report
)

// Model is the doctor overlay, owned by the app model like the other
// self-owned overlays (finder, todo, …).
type Model struct {
	open     bool
	ph       phase
	terminal string
	sess     *keymap.ProbeSession
	skipKey  string
	width    int
	height   int
	pal      *theme.Palette

	// Dead-binding report state (#2161): the audited findings, the selected
	// row with its scroll offset, which suggestion each finding currently
	// offers, and the chords already rebound in this run.
	env      keymap.TerminalEnv
	findings []Finding
	sel      int
	off      int
	pick     []int
	applied  map[string]bool
	note     string
}

// New constructs a closed doctor.
func New() *Model { return &Model{} }

// SetSize records the terminal size.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// SetPalette installs the theme palette.
func (m *Model) SetPalette(p *theme.Palette) { m.pal = p }

// theme returns the active palette, defaulting when none was threaded in.
func (m *Model) theme() *theme.Palette {
	if m.pal != nil {
		return m.pal
	}
	return theme.DefaultPalette()
}

// IsOpen reports whether the overlay owns the input.
func (m *Model) IsOpen() bool { return m.open }

// Open starts a fresh probe run for the given terminal identity.
func (m *Model) Open(terminal string) {
	m.open = true
	m.ph = probing
	m.terminal = terminal
	m.sess = keymap.NewProbeSession(keymap.ProbeTargets())
	m.skipKey = freeControlChord(m.sess.Targets())
}

// Close discards the overlay state without emitting a result.
func (m *Model) Close() { m.open = false }

// freeControlChord picks the skip key: a ctrl+letter (delivered everywhere)
// that is not itself a probe target, so pressing it can never be mistaken
// for a probed chord. The fallback only fires if the default keymap ever
// binds every candidate.
func freeControlChord(targets []string) string {
	taken := make(map[string]bool, len(targets))
	for _, t := range targets {
		taken[t] = true
	}
	for _, c := range []string{"ctrl+n", "ctrl+k", "ctrl+b", "ctrl+x", "ctrl+j", "ctrl+u"} {
		if !taken[c] {
			return c
		}
	}
	return "ctrl+n"
}

// RecordKey feeds a mouse nav button (already converted through
// keymap.FromMouseButton) into the session; a summary phase ignores it.
func (m *Model) RecordKey(k keymap.Key) {
	if m.open && m.ph == probing {
		m.sess.HandleKey(k)
	}
}

// Update receives every key press while the overlay is open — routed ahead
// of keymap resolution, so probing sees the raw chords the terminal
// actually delivers.
func (m *Model) Update(msg tea.KeyPressMsg) tea.Cmd {
	if !m.open {
		return nil
	}
	switch m.ph {
	case summary:
		return m.updateSummary(msg)
	case report:
		return m.updateReport(msg)
	}
	// Control keys first, by raw string: ctrl+d (delivered everywhere, not a
	// target) and esc (not a target either) end the probing phase; the
	// computed skip key advances past the current chord. Everything else is
	// probe evidence.
	switch msg.String() {
	case "ctrl+d", "esc":
		m.ph = summary
		return nil
	case m.skipKey:
		m.sess.SkipNext()
		return nil
	}
	if k, ok := keymap.FromKeyMsg(msg); ok {
		m.sess.HandleKey(k)
	}
	return nil
}

// updateSummary decides the run's fate with ordinary key semantics: enter/y
// saves, r saves and goes on to the dead-binding report, d/n discards, esc
// resumes probing.
func (m *Model) updateSummary(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "enter", "y", "r":
		res := ResultMsg{Terminal: m.terminal, Results: m.sess.Results(), Save: true, Review: msg.String() == "r"}
		m.open = false
		return func() tea.Msg { return res }
	case "d", "n":
		res := ResultMsg{Terminal: m.terminal, Save: false}
		m.open = false
		return func() tea.Msg { return res }
	case "esc":
		m.ph = probing
	}
	return nil
}

// View renders the overlay box for overlay.Center.
func (m *Model) View() string {
	if !m.open || m.width <= 0 {
		return ""
	}
	boxW := m.width - 4
	boxH := m.height - 2
	innerW, innerH := boxW-4, boxH-2
	var body string
	switch m.ph {
	case summary:
		body = m.summaryBody(innerW)
	case report:
		body = m.reportBody(innerW, innerH)
	default:
		body = m.probeBody(innerW, innerH)
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme().BorderFocus).
		Padding(0, 1).
		Width(boxW - 2).
		MaxHeight(boxH).
		Render(body)
}

// probeBody is the probing screen: instructions, the target grid, progress.
func (m *Model) probeBody(w, h int) string {
	sec := lipgloss.NewStyle().Foreground(m.theme().Secondary)
	title := lipgloss.NewStyle().Bold(true).Underline(true).Render("Keymap Doctor — " + m.terminal)
	head := []string{
		title,
		sec.Render("press each chord (click for mouse-back/forward) · " + m.skipKey + " skip · ctrl+d or esc finish"),
		"",
	}
	delivered, collapsed, skipped, pending := m.sess.Counts()
	foot := []string{
		"",
		sec.Render("last key: " + m.sess.Last() +
			" · ✓ " + strconv.Itoa(delivered) +
			" ≈ " + strconv.Itoa(collapsed) +
			" − " + strconv.Itoa(skipped) +
			" · " + strconv.Itoa(pending) + " pending"),
	}
	gridH := h - len(head) - len(foot)
	if gridH < 3 {
		gridH = 3
	}
	grid := m.targetGrid(w, gridH)
	return strings.Join(append(append(head, grid), foot...), "\n")
}

// targetGrid lays the targets out in as many columns as the height forces,
// clipped to the width — the run is order-independent, so a clipped column
// costs visibility, not correctness.
func (m *Model) targetGrid(w, h int) string {
	targets := m.sess.Targets()
	next := m.sess.Next()
	nextStyle := lipgloss.NewStyle().Background(m.theme().Selection).Foreground(m.theme().SelectionText).Bold(true)
	okStyle := lipgloss.NewStyle().Foreground(m.theme().Success)
	warnStyle := lipgloss.NewStyle().Foreground(m.theme().Warning)
	dimStyle := lipgloss.NewStyle().Foreground(m.theme().Secondary).Faint(true)
	colW := 0
	for _, t := range targets {
		if len(t) > colW {
			colW = len(t)
		}
	}
	colW += 5 // mark + gap
	var cols []string
	for start := 0; start < len(targets); start += h {
		end := min(start+h, len(targets))
		var lines []string
		for _, t := range targets[start:end] {
			mark, style := " · ", lipgloss.NewStyle()
			switch got := m.sess.Hit(t); {
			case got == t:
				mark, style = " ✓ ", okStyle
			case got != "":
				mark, style = " ≈ ", warnStyle
			case m.sess.Skipped(t):
				mark, style = " − ", dimStyle
			case t == next:
				style = nextStyle
			}
			lines = append(lines, style.Render(mark+pad(t, colW-3)))
		}
		cols = append(cols, strings.Join(lines, "\n"))
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(lipgloss.JoinHorizontal(lipgloss.Top, cols...))
}

// summaryBody is the finish screen: counts plus the save/discard decision.
func (m *Model) summaryBody(w int) string {
	sec := lipgloss.NewStyle().Foreground(m.theme().Secondary)
	warn := lipgloss.NewStyle().Foreground(m.theme().Warning)
	delivered, collapsed, skipped, pending := m.sess.Counts()
	lines := []string{
		lipgloss.NewStyle().Bold(true).Underline(true).Render("Keymap Doctor — " + m.terminal),
		"",
		"delivered: " + strconv.Itoa(delivered),
		"collapsed: " + strconv.Itoa(collapsed) + sec.Render("  (arrived as another key — recorded missing, with evidence)"),
		"skipped:   " + strconv.Itoa(skipped) + sec.Render("  (no verdict recorded)"),
		"missing:   " + strconv.Itoa(pending) + sec.Render("  (never arrived)"),
		"",
	}
	if pending > 0 {
		lines = append(lines, warn.Render("saving records the "+strconv.Itoa(pending)+" untried chords as missing — esc to keep probing, or skip them first"), "")
	}
	lines = append(lines, sec.Render("enter/y save for this terminal · r save + review dead bindings · d discard · esc resume probing"))
	clip := lipgloss.NewStyle().MaxWidth(w)
	for i := range lines {
		lines[i] = clip.Render(lines[i])
	}
	return strings.Join(lines, "\n")
}

// pad right-pads s to width n.
func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
