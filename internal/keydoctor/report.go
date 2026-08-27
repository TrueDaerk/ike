package keydoctor

import (
	"strconv"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/keymap"
)

// report.go is the dead-binding report phase (#2161): the audit's findings as
// a navigable list, one suggested rebind per finding, applied with a single
// enter. The write itself belongs to the app — the overlay emits RebindMsg
// and the root model runs it through the ordinary keymap customization path
// (a keymap.bindings.* override at user scope, then a config reload).

// RebindMsg asks the app to persist a suggested rebind: New binds Command,
// Old unbinds. Both writes land before one reload, exactly like the settings
// keymap page's rebind, so the table re-resolves atomically.
type RebindMsg struct {
	Command string
	Old     keymap.Chord
	New     keymap.Chord
}

// OpenReport opens the doctor straight into the dead-binding report for the
// given binding table, skipping the probe run: the verdicts come from the
// static platform rules plus whatever this terminal has already probed.
func (m *Model) OpenReport(env keymap.TerminalEnv, bindings []keymap.Binding) {
	m.open = true
	m.ph = report
	m.env = env
	m.terminal = env.Terminal
	m.findings = Analyze(env, bindings)
	m.sel, m.off = 0, 0
	m.pick = make([]int, len(m.findings))
	m.applied = map[string]bool{}
	m.note = ""
}

// Findings exposes the audit result of the open report (tests, diagnostics).
func (m *Model) Findings() []Finding { return m.findings }

// ReportOpen reports whether the overlay is showing the dead-binding report
// (as opposed to a probe run, or nothing at all).
func (m *Model) ReportOpen() bool { return m.open && m.ph == report }

// RefreshReport re-audits an open report against a freshly built binding
// table — the app calls it after every config reload, so an applied rebind
// drops off the list and the remaining suggestions are re-checked against the
// keymap as it now stands. The cursor keeps its position as far as the new
// list allows.
func (m *Model) RefreshReport(env keymap.TerminalEnv, bindings []keymap.Binding) {
	if !m.ReportOpen() {
		return
	}
	m.env = env
	m.terminal = env.Terminal
	m.findings = Analyze(env, bindings)
	m.pick = make([]int, len(m.findings))
	if m.sel >= len(m.findings) {
		m.sel = len(m.findings) - 1
	}
	if m.sel < 0 {
		m.sel = 0
	}
}

// updateReport drives the report: j/k move, tab cycles the offered chord,
// enter applies it, esc closes.
func (m *Model) updateReport(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		if m.sel < len(m.findings)-1 {
			m.sel++
			m.note = ""
		}
	case "k", "up":
		if m.sel > 0 {
			m.sel--
			m.note = ""
		}
	case "tab", "l", "right":
		m.cycleSuggestion(1)
	case "shift+tab", "h", "left":
		m.cycleSuggestion(-1)
	case "enter":
		return m.applySelected()
	case "esc", "q":
		m.open = false
	}
	return nil
}

// cycleSuggestion rotates the selected finding's offered chord.
func (m *Model) cycleSuggestion(delta int) {
	f, ok := m.currentFinding()
	if !ok || len(f.Suggestions) < 2 {
		return
	}
	n := len(f.Suggestions)
	m.pick[m.sel] = ((m.pick[m.sel]+delta)%n + n) % n
	m.note = ""
}

// currentFinding returns the selected finding.
func (m *Model) currentFinding() (Finding, bool) {
	if m.sel < 0 || m.sel >= len(m.findings) {
		return Finding{}, false
	}
	return m.findings[m.sel], true
}

// applySelected emits the rebind for the selected finding's offered chord.
// The overlay stays open — a setup with a dead alt family is repaired binding
// by binding — and marks the row applied so a second enter cannot double-write
// (the report's findings are the pre-rebind snapshot).
func (m *Model) applySelected() tea.Cmd {
	f, ok := m.currentFinding()
	if !ok {
		return nil
	}
	if m.applied[f.Binding.Chord.String()] {
		m.note = "already rebound in this run"
		return nil
	}
	if len(f.Suggestions) == 0 {
		m.note = "no conflict-free alternative for " + f.Binding.Chord.String() +
			" — unbind something, or rebind it on the settings Keymap page"
		return nil
	}
	chord := f.Suggestions[m.pick[m.sel]]
	m.applied[f.Binding.Chord.String()] = true
	m.note = "rebound " + f.Binding.Command + ": " + f.Binding.Chord.String() + " → " + chord.String()
	msg := RebindMsg{Command: f.Binding.Command, Old: f.Binding.Chord, New: chord}
	return func() tea.Msg { return msg }
}

// wrapLines word-wraps s to width w over at most max lines; an overlong last
// line is cut with an ellipsis rather than pushing the layout around.
func wrapLines(s string, w, max int) []string {
	if w < 8 {
		w = 8
	}
	indent := ""
	if strings.HasPrefix(s, " ") {
		indent = " " // the leading indent the caller asked for, on every line
	}
	var lines []string
	cur := indent
	for _, word := range strings.Fields(s) {
		switch {
		case strings.TrimSpace(cur) == "":
			cur += word
		case utf8.RuneCountInString(cur)+1+utf8.RuneCountInString(word) <= w:
			cur += " " + word
		default:
			lines = append(lines, cur)
			cur = indent + word
		}
	}
	if strings.TrimSpace(cur) != "" {
		lines = append(lines, cur)
	}
	if len(lines) > max {
		lines = lines[:max]
		if r := []rune(lines[max-1]); len(r) > w-1 {
			lines[max-1] = string(r[:w-1])
		}
		lines[max-1] += "…"
	}
	return lines
}

// reportBody renders the finding list with the selected finding's detail
// pinned below it.
func (m *Model) reportBody(w, h int) string {
	pal := m.theme()
	sec := lipgloss.NewStyle().Foreground(pal.Secondary)
	warn := lipgloss.NewStyle().Foreground(pal.Warning)
	bad := lipgloss.NewStyle().Foreground(pal.Error)
	ok := lipgloss.NewStyle().Foreground(pal.Success)
	selStyle := lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
	clip := lipgloss.NewStyle().MaxWidth(w)

	dead, risky := 0, 0
	for _, f := range m.findings {
		if f.Class == keymap.Dead {
			dead++
		} else {
			risky++
		}
	}
	head := []string{
		lipgloss.NewStyle().Bold(true).Underline(true).Render("Keymap Doctor — dead bindings — " + m.terminal),
		sec.Render("active keymap on " + m.env.GOOS + " · ✗ " + strconv.Itoa(dead) + " dead · ⚠ " + strconv.Itoa(risky) + " at risk"),
		"",
	}
	if len(m.findings) == 0 {
		return strings.Join(append(head,
			ok.Render(" every bound chord is deliverable in this terminal"),
			"",
			sec.Render("esc close"),
		), "\n")
	}

	// Footer: the selected finding's reason, its offer and the key legend.
	var foot []string
	if f, has := m.currentFinding(); has {
		foot = append(foot, "")
		// The reason phrases run long; wrapping them over a fixed two lines
		// keeps the footer's height (and with it the list window) constant.
		for _, line := range wrapLines(" why: "+f.Reason, w, 2) {
			foot = append(foot, clip.Render(sec.Render(line)))
		}
		switch {
		case m.applied[f.Binding.Chord.String()]:
			foot = append(foot, clip.Render(ok.Render(" ✓ rebound — reload applied")))
		case len(f.Suggestions) == 0:
			foot = append(foot, clip.Render(warn.Render(" no conflict-free alternative found")))
		default:
			offer := " rebind to: " + f.Suggestions[m.pick[m.sel]].String()
			if len(f.Suggestions) > 1 {
				offer += "  (" + strconv.Itoa(m.pick[m.sel]+1) + "/" + strconv.Itoa(len(f.Suggestions)) + ", tab cycles)"
			}
			foot = append(foot, clip.Render(ok.Render(offer)))
		}
		if m.note != "" {
			foot = append(foot, clip.Render(sec.Render(" "+m.note)))
		}
		foot = append(foot, clip.Render(sec.Render("j/k move · tab next suggestion · enter rebind · esc close")))
	}

	listH := h - len(head) - len(foot)
	if listH < 1 {
		listH = 1
	}
	if m.sel < m.off {
		m.off = m.sel
	}
	if m.sel >= m.off+listH {
		m.off = m.sel - listH + 1
	}
	if m.off < 0 {
		m.off = 0
	}
	chordW := 0
	for _, f := range m.findings {
		if n := len(f.Binding.Chord.String()); n > chordW {
			chordW = n
		}
	}
	var lines []string
	for i := m.off; i < len(m.findings) && i < m.off+listH; i++ {
		f := m.findings[i]
		mark, style := " ⚠ ", warn
		if f.Class == keymap.Dead {
			mark, style = " ✗ ", bad
		}
		if m.applied[f.Binding.Chord.String()] {
			mark, style = " ✓ ", ok
		}
		label := f.Binding.Command
		if f.Binding.Context != keymap.Global {
			label += " [" + string(f.Binding.Context) + "]"
		}
		if len(f.Suggestions) > 0 && !m.applied[f.Binding.Chord.String()] {
			label += "  → " + f.Suggestions[m.pick[i]].String()
		}
		line := mark + pad(f.Binding.Chord.String(), chordW+2) + label
		if i == m.sel {
			lines = append(lines, clip.Render(selStyle.Render(pad(line, w))))
			continue
		}
		lines = append(lines, clip.Render(style.Render(line)))
	}
	return strings.Join(append(append(head, lines...), foot...), "\n")
}
