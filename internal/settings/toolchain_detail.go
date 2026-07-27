package settings

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/config"
	"ike/internal/lang"
)

// toolchain_detail.go puts the toolchain page on the panel's grid (0460,
// #1299). Two things were wrong before: a flat list mixed twelve
// "(not found)" rows in with the three that matter, and the candidate picker
// only appeared *after* pressing enter — so a first run looked like an empty
// slate you were expected to type paths into. Now the languages group by
// state, the noise folds, and the detail column always shows the real finds
// for the selection, with an accept-all for the first run.

// tcState classifies a language row for grouping.
type tcState int

const (
	tcConfigured tcState = iota // an explicit lang.<id>.interpreter
	tcDetected                  // found by discovery, not configured
	tcMissing                   // nothing found
)

// stateOf classifies a language by how its interpreter resolves.
func (t *ToolchainPage) stateOf(l lang.Language) tcState {
	path, source := t.interpreter(l.ID)
	switch {
	case path == "":
		return tcMissing
	case source == "config":
		return tcConfigured
	default:
		return tcDetected
	}
}

// groupTitles name the three groups in list order.
var groupTitles = map[tcState]string{
	tcConfigured: "configured",
	tcDetected:   "detected · not configured",
	tcMissing:    "not installed",
}

// detectedUnconfigured lists the languages an accept-all would adopt.
func (t *ToolchainPage) detectedUnconfigured() []lang.Language {
	var out []lang.Language
	for _, l := range t.languages() {
		if t.stateOf(l) == tcDetected {
			out = append(out, l)
		}
	}
	return out
}

// acceptAll writes the detected interpreter for every language that has one
// but no explicit setting (#1299): the first run is one key, not one guided
// picker per language.
func (t *ToolchainPage) acceptAll() tea.Cmd {
	langs := t.detectedUnconfigured()
	if len(langs) == 0 {
		t.envState = "nothing to accept — every detected toolchain is already configured"
		return nil
	}
	muts := make([]config.Mutation, 0, len(langs))
	var probes []tea.Cmd
	for _, l := range langs {
		path, _ := t.interpreter(l.ID)
		muts = append(muts, config.Mutation{Scope: config.ProjectScope, Key: "lang." + l.ID + ".interpreter", Value: path})
		probes = append(probes, t.probe(l.ID, path))
	}
	t.envState = "accepted " + strconv.Itoa(len(muts)) + " detected toolchains"
	return tea.Batch(append(probes, config.ApplyAndReload(t.opts, muts), t.restartCmd())...)
}

// renderDetail renders the toolchain page's detail column: what the category
// is for when nothing is selected, and otherwise the selected language's
// candidates — real finds with their versions and provenance, never an empty
// field to type into.
func (t *ToolchainPage) renderDetail(w, h int) string {
	pal := t.theme()
	clip := lipgloss.NewStyle().MaxWidth(w)
	title := lipgloss.NewStyle().Foreground(pal.BorderFocus).Bold(true)
	dim := lipgloss.NewStyle().Foreground(pal.Secondary)
	good := lipgloss.NewStyle().Foreground(pal.Success)
	warn := lipgloss.NewStyle().Foreground(pal.Error)

	l, ok := t.current()
	if !ok {
		return strings.Join(padTo(t.onboardingDetail(w, clip, title, dim), h), "\n")
	}

	lines := []string{clip.Render(title.Render(" " + l.ID + " — interpreter"))}
	lines = append(lines, wrapDetail(w, dim, clip,
		"One interpreter per language. Run, Debug and the language servers all follow this choice.")...)
	lines = append(lines, clip.Render(dim.Render(" "+strings.Repeat("─", maxInt(w-2, 1)))))
	t.detailHead = len(lines)

	cur, source := t.interpreter(l.ID)
	switch {
	case t.custom:
		lines = append(lines, clip.Render(" ✎ "+t.inputField.View()))
		if t.invalid != "" {
			lines = append(lines, clip.Render(warn.Render(" ✗ "+t.invalid)))
		}
		for _, s := range t.suggest.lines() {
			lines = append(lines, clip.Render(dim.Render(s)))
		}
		lines = append(lines, clip.Render(dim.Render(" tab completes · enter applies · esc cancels")))
	case t.picking && len(t.candidates) == 0:
		// Discovery is asynchronous; the column says so rather than showing
		// an empty field.
		lines = append(lines, clip.Render(dim.Render(" scanning…")))
	default:
		lines = append(lines, t.candidateLines(l, cur, w, clip, dim, good)...)
	}

	var foot []string
	if cur == "" {
		foot = append(foot, clip.Render(warn.Render(" ✗ nothing found on this machine")))
	} else {
		foot = append(foot, clip.Render(dim.Render(" in use: @"+source)))
	}
	foot = append(foot, clip.Render(dim.Render(" enter pick · p probe version · r reset to detection")))
	for len(lines) < h-len(foot) {
		lines = append(lines, "")
	}
	lines = append(lines, foot...)
	return strings.Join(padTo(lines, h), "\n")
}

// candidateLines renders the discovered interpreters for a language: every
// entry is a real find, and typing a path by hand is the last row, never the
// first.
func (t *ToolchainPage) candidateLines(l lang.Language, cur string, w int, clip, dim, good lipgloss.Style) []string {
	pal := t.theme()
	sel := lipgloss.NewStyle().Background(pal.Selection).Foreground(pal.SelectionText).Bold(true)
	cands := t.candidates
	if !t.picking {
		// Browsing: show the candidates already discovered, or at least the
		// interpreter in use, so the column is never blank.
		if len(cands) == 0 && cur != "" {
			cands = []string{cur}
		}
	}
	out := []string{clip.Render(dim.Render(" found · " + strconv.Itoa(len(cands))))}
	t.candTop = t.detailHead + len(out)
	curKey := resolvedKey(cur)
	for i, c := range cands {
		mark := "○"
		style := lipgloss.NewStyle().Foreground(pal.Foreground)
		if c == cur || resolvedKey(c) == curKey {
			mark, style = "●", good
		}
		// The tail — provenance and version — is what distinguishes two
		// candidates, so the *path* yields when the column is too narrow.
		tail := ""
		if l.ID == "python" {
			if p := t.provenance(c); p != "" {
				tail += "  [" + p + "]"
			}
		}
		if v := t.versions[c]; v != "" {
			tail += "  " + v
		}
		line := " " + mark + " " + elideLeft(c, w-4-lipgloss.Width(tail)) + tail
		if t.picking && i == t.pick {
			style = sel
		}
		out = append(out, clip.Render(style.Render(line)))
	}
	manual := " ○ enter a path manually…"
	style := dim
	if t.picking && t.pick >= len(cands) {
		style = sel
	}
	out = append(out, clip.Render(style.Render(manual)))
	return out
}

// wrapDetail word-wraps prose to the detail column instead of clipping it —
// a truncated sentence teaches nothing.
func wrapDetail(w int, style, clip lipgloss.Style, text string) []string {
	var out []string
	for _, l := range strings.Split(ansi.Wordwrap(text, maxInt(w-2, 8), ""), "\n") {
		out = append(out, clip.Render(style.Render(" "+l)))
	}
	return out
}

// elideLeft shortens a path from the left, keeping the distinguishing tail
// ("…/uv/python/3.13/bin/python3"). Paths shorter than w are returned as-is.
func elideLeft(s string, w int) string {
	if w < 4 || lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	return "…" + string(r[len(r)-(w-1):])
}

// onboardingDetail is the no-selection state — and the first run's whole
// point: explain the category and offer to take every recommendation at once.
func (t *ToolchainPage) onboardingDetail(w int, clip, title, dim lipgloss.Style) []string {
	out := []string{clip.Render(title.Render(" What is Toolchain?"))}
	out = append(out, wrapDetail(w, dim, clip,
		"One interpreter or compiler per language. Run, Debug and the language servers then know which binary to use.")...)
	out = append(out, "")
	pending := t.detectedUnconfigured()
	if len(pending) == 0 {
		out = append(out, clip.Render(dim.Render(" Everything detected here is already configured.")))
		return out
	}
	names := make([]string, 0, len(pending))
	for _, l := range pending {
		names = append(names, l.ID)
	}
	out = append(out, wrapDetail(w, dim, clip, "detected, not configured: "+strings.Join(names, ", "))...)
	out = append(out,
		"",
		clip.Render(lipgloss.NewStyle().Foreground(t.theme().Info).Render(
			" a · accept all "+strconv.Itoa(len(pending))+" recommendations")),
	)
	out = append(out, wrapDetail(w, dim, clip, "You do not have to type anything here.")...)
	return out
}
