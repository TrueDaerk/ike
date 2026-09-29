package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/host"
	"ike/internal/jqplay"
	"ike/internal/theme"
)

// playopts.go holds the playground's raw / compact / slurp toggles (#2784),
// jq's `-r`, `-c` and `-s`: the chips at the head of the info row, the
// commands that flip them (json.jqToggleRaw / …Compact / …Slurp) and the
// per-source memory that restores them with the last program. The toggles
// belong to the jq and yq dialects; xmq's engine is an external binary with
// options of its own, so its playground shows no chips and the commands say
// so instead of flipping state that would change nothing.

// PlayOption names one of the toggles.
type PlayOption int

// The toggles, in chip order.
const (
	PlayOptRaw PlayOption = iota
	PlayOptCompact
	PlayOptSlurp
)

// TogglePlayOptionMsg flips one toggle of the open playground and reruns it.
type TogglePlayOptionMsg struct{ Option PlayOption }

// playChips are the chip labels in order; each is two cells, one space apart,
// so the chip under a click is its cell index divided by three.
var playChips = [...]string{"-r", "-c", "-s"}

// playChipsW is the chips' width in cells: "-r -c -s".
const playChipsW = 8

// playOptionName is a toggle's spelled-out name for the status line.
func playOptionName(o PlayOption) string {
	switch o {
	case PlayOptCompact:
		return "compact output (-c)"
	case PlayOptSlurp:
		return "slurp input (-s)"
	}
	return "raw output (-r)"
}

// playOptionOn reports whether toggle o is set in opts.
func playOptionOn(opts jqplay.Options, o PlayOption) bool {
	switch o {
	case PlayOptCompact:
		return opts.Compact
	case PlayOptSlurp:
		return opts.Slurp
	}
	return opts.Raw
}

// flipPlayOption returns opts with toggle o flipped.
func flipPlayOption(opts jqplay.Options, o PlayOption) jqplay.Options {
	switch o {
	case PlayOptCompact:
		opts.Compact = !opts.Compact
	case PlayOptSlurp:
		opts.Slurp = !opts.Slurp
	default:
		opts.Raw = !opts.Raw
	}
	return opts
}

// togglePlayOption flips toggle o and reruns the program at once, so the
// result buffer — and with it copy, export and open-as-scratch, which all
// read the installed result — switches form under the user's eyes.
func (m *Model) togglePlayOption(o PlayOption) tea.Cmd {
	s := m.play
	if s == nil {
		return nil
	}
	if s.dialect == jqplay.DialectXMQ {
		m.host.Notify(host.Info, "the xmq playground has no -r / -c / -s toggles")
		return nil
	}
	s.opts = flipPlayOption(s.opts, o)
	state := "off"
	if playOptionOn(s.opts, o) {
		state = "on"
	}
	s.status, s.statusWarn = playOptionName(o)+" "+state, false
	return m.runPlayNow()
}

// playSeedOptions is the toggles a fresh playground starts with: those the
// source's remembered last program ran with (#1982/#2774), exactly when
// playSeedProgram restores that program — a path-seeded or identity open
// starts with every toggle off.
func (m Model) playSeedOptions(d jqplay.Dialect, src playInputSource, atPath bool) jqplay.Options {
	if atPath || d == jqplay.DialectXMQ {
		return jqplay.Options{}
	}
	if last := m.playLastProgram[src.key]; last != "" {
		return m.playLastOpts[src.key]
	}
	if src.path != "" {
		if last, ok := m.playLastStoreOf().Get(src.key); ok && last != "" {
			return jqplay.ParseFlags(m.playLastStoreOf().Flags(src.key))
		}
	}
	return jqplay.Options{}
}

// playChipsPrefix renders the chips that lead the info row, and the width
// left for the rest of it: "" and width unchanged for xmq, which has no
// toggles, or on a row too narrow to keep a readable line beside them.
func (m Model) playChipsPrefix(width int) (string, int) {
	s := m.play
	if s == nil || s.dialect == jqplay.DialectXMQ || width-playChipsW-1 < playInfoMinLine {
		return "", width
	}
	return m.playOptionChips() + " ", width - playChipsW - 1
}

// playOptionChips renders `-r -c -s` with the active toggles highlighted.
func (m Model) playOptionChips() string {
	pal := m.pal()
	off := lipgloss.NewStyle().Foreground(pal.Hint)
	fg := theme.Readable(pal.Accent, pal.Background, pal.Surface, pal.Foreground)
	on := lipgloss.NewStyle().Bold(true).Foreground(fg).Background(pal.Accent)
	parts := make([]string, len(playChips))
	for i, c := range playChips {
		if playOptionOn(m.play.opts, PlayOption(i)) {
			parts[i] = on.Render(c)
		} else {
			parts[i] = off.Render(c)
		}
	}
	return strings.Join(parts, " ")
}

// playChipAt maps a content-local x on the info row to the chip under it.
// x is relative to the chips' first cell.
func playChipAt(x int) (PlayOption, bool) {
	if x < 0 || x >= playChipsW || x%3 == 2 {
		return 0, false
	}
	return PlayOption(x / 3), true
}

// playInfoRowY is the content-local row of the info row: the header sits
// above the result at negative y, the stale banner (when up) between the two.
func (m Model) playInfoRowY() int { return -1 - m.playStaleRows() }

// clickPlayChip toggles the chip under a left press on the info row, reporting
// whether the press hit one.
func (m *Model) clickPlayChip(x, y int) (tea.Cmd, bool) {
	s := m.play
	r, ok := m.lay.Panes[s.paneKey]
	if !ok || y != m.playInfoRowY() {
		return nil, false
	}
	cx := m.playChipsX(paneInterior(r.W, paneChromeW))
	if cx < 0 {
		return nil, false
	}
	o, ok := playChipAt(x - cx)
	if !ok {
		return nil, false
	}
	return m.togglePlayOption(o), true
}

// playChipsX is the column the chips start at on an info row of width — they
// lead it — or -1 when the row does not show them. It asks the renderer's own
// question (playChipsPrefix), so a click lands on the chip drawn under it.
func (m Model) playChipsX(width int) int {
	if width < 20 {
		width = 20 // playInlineBody's floor
	}
	if chips, _ := m.playChipsPrefix(width); chips == "" {
		return -1
	}
	return 0
}
