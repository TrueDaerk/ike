package editor

import "ike/internal/vcs"

// hostsigns.go lets a host put its own glyphs in the gutter's sign column for
// a synthetic read-only buffer (#2789). The jq playground marks the first line
// of each output value with a type glyph there, so where one value ends and
// the next starts is visible in a multi-output stream. The glyph overwrites
// the sign cell like every other marker, so the gutter width — and with it the
// line numbers — never moves. Host signs rank lowest among the glyph markers:
// a breakpoint, mark or coverage bar on the same line wins.

// SetHostSigns installs the host's sign-column glyphs keyed by 0-based line;
// nil clears them. They render in the muted hint tone.
func (m *Model) SetHostSigns(signs map[int]string) { m.hostSigns = signs }

// SetHostChanges installs line-change marks a host computed itself (#2787) —
// the jq playground's "changed since the previous run" diff — keyed by 0-based
// line; nil clears them. Added and changed lines draw a bar in the sign column
// and tint the line number in the Info tone; a deletion draws a thin top bar on
// the line after the gap. On a line that also carries a host sign the glyph
// keeps its shape and takes the Info tone, so the value boundary stays visible
// and the change still reads. Like host signs they rank below every built-in
// glyph marker.
func (m *Model) SetHostChanges(marks map[int]vcs.LineMark) { m.hostChanges = marks }

// HostChanges returns the installed host change marks (nil when none).
func (m Model) HostChanges() map[int]vcs.LineMark { return m.hostChanges }

// hostChangeSign is the sign-column glyph of a host change mark: a bar for an
// added or changed line, a thin top bar for lines deleted right above.
func hostChangeSign(mk vcs.LineMark) string {
	if mk == vcs.LineDeleted {
		return "▔"
	}
	return "▎"
}
