package editor

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
