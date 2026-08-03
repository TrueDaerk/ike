package editor

import (
	"image/color"
	"strconv"

	"charm.land/lipgloss/v2"
)

// Raw control bytes in a buffer (#1469) — think a log file that still carries
// ANSI colour escapes — must never reach the host terminal verbatim: the
// terminal would interpret them, so the screen and the buffer model disagree
// about how wide the line is, which desyncs click mapping and lets the file's
// SGR state bleed into the editor's own caret/selection styling. Instead every
// C0 control (tab keeps its expansion, newlines never occur inside a line)
// renders as its one-cell Unicode Control Pictures glyph (␛ for ESC), so one
// buffer rune is one display cell and all mapping stays trivially correct.
//
// The colours are kept as a deliberate feature: SGR sequences (CSI ... 'm')
// are parsed per line and the text they govern renders with the mapped
// colour/attributes through the editor's own styling pipeline — the sequence
// bytes themselves show dim, and caret, selection and search overlays still
// win exactly as they do over syntax highlighting.

// isCtrlRune reports whether r is a control rune that must not be emitted raw:
// any C0 control except tab (which renderSpan expands itself), plus DEL.
func isCtrlRune(r rune) bool {
	return (r < 0x20 && r != '\t') || r == 0x7f
}

// ctrlGlyph returns the one-cell placeholder for a control rune: the matching
// Unicode Control Pictures glyph (U+2400 block), e.g. ESC → ␛, NUL → ␀.
func ctrlGlyph(r rune) string {
	if r == 0x7f {
		return "␡"
	}
	return string('␀' + r)
}

// hasCtrlRune reports whether the line contains any control rune — the cheap
// gate that keeps the SGR parse off the render path for ordinary lines.
func hasCtrlRune(runes []rune) bool {
	for _, r := range runes {
		if isCtrlRune(r) {
			return true
		}
	}
	return false
}

// sgrSpan is one column run [Start, End) of a line that an SGR escape governs.
// Seq marks the escape sequence's own runes (rendered dim, like the ␛ glyph);
// otherwise the span is text carrying the accumulated SGR state.
type sgrSpan struct {
	Start, End int
	Seq        bool

	FG, BG                         color.Color // nil = terminal default
	Bold, Faint, Italic, Underline bool
}

// styled reports whether the span changes anything over the default rendering.
func (s sgrSpan) styled() bool {
	return s.FG != nil || s.BG != nil || s.Bold || s.Faint || s.Italic || s.Underline
}

// apply composes the span's SGR state over st. The file's own foreground
// replaces the syntax colour — that is the point of keeping the escapes as a
// feature — while cursor, selection and search overlays still win in
// renderSpan's earlier branches.
func (s sgrSpan) apply(st lipgloss.Style) lipgloss.Style {
	if s.FG != nil {
		st = st.Foreground(s.FG)
	}
	if s.BG != nil {
		st = st.Background(s.BG)
	}
	if s.Bold {
		st = st.Bold(true)
	}
	if s.Faint {
		st = st.Faint(true)
	}
	if s.Italic {
		st = st.Italic(true)
	}
	if s.Underline {
		st = st.Underline(true)
	}
	return st
}

// sgrSpanAt returns the span covering col, if any. Spans are sorted and
// non-overlapping; the list is short (one per escape sequence on the line).
func sgrSpanAt(spans []sgrSpan, col int) (sgrSpan, bool) {
	for _, s := range spans {
		if col >= s.Start && col < s.End {
			return s, true
		}
	}
	return sgrSpan{}, false
}

// parseSGRSpans scans one line for SGR escape sequences (ESC '[' params 'm')
// and returns the resulting spans: each sequence's own rune range (Seq), and
// between sequences the text runs carrying the accumulated state. State does
// not carry across lines — log files overwhelmingly set and reset per line,
// and per-line parsing keeps the render line-cache exact. Unrecognized or
// unterminated escapes yield no span; their runes still render safely through
// the control-glyph substitution.
func parseSGRSpans(runes []rune) []sgrSpan {
	var spans []sgrSpan
	var cur sgrSpan // accumulated state; Start of the pending text run
	flush := func(end int) {
		if cur.styled() && end > cur.Start {
			s := cur
			s.End = end
			spans = append(spans, s)
		}
	}
	for i := 0; i < len(runes); i++ {
		if runes[i] != 0x1b || i+1 >= len(runes) || runes[i+1] != '[' {
			continue
		}
		end, params, ok := scanCSI(runes, i+2)
		if !ok {
			continue
		}
		flush(i)
		spans = append(spans, sgrSpan{Start: i, End: end, Seq: true})
		st := cur
		st.Start = end
		cur = applySGRParams(st, params)
		i = end - 1
	}
	flush(len(runes))
	return spans
}

// scanCSI scans a CSI body starting at i (past ESC '['): parameter runes
// (digits, ';', ':') followed by the final byte. It reports the index one past
// the final byte and the parsed numeric parameters, with ok=false when the
// sequence is unterminated, carries non-parameter bytes, or does not end in
// 'm' (only SGR is interpreted).
func scanCSI(runes []rune, i int) (end int, params []int, ok bool) {
	n, hasDigit := 0, false
	push := func() {
		if hasDigit {
			params = append(params, n)
		} else {
			params = append(params, 0) // an empty parameter means 0 (reset)
		}
		n, hasDigit = 0, false
	}
	for ; i < len(runes); i++ {
		switch r := runes[i]; {
		case r >= '0' && r <= '9':
			n = n*10 + int(r-'0')
			hasDigit = true
		case r == ';' || r == ':':
			push()
		case r == 'm':
			push()
			return i + 1, params, true
		default:
			return 0, nil, false
		}
	}
	return 0, nil, false
}

// applySGRParams folds one sequence's parameters into the running state.
// Unknown parameters are ignored. Basic and indexed colours map onto the
// terminal palette (lipgloss numeric colours), so the file's colours render
// exactly as the terminal would have shown them.
func applySGRParams(st sgrSpan, params []int) sgrSpan {
	for i := 0; i < len(params); i++ {
		switch p := params[i]; {
		case p == 0:
			st = sgrSpan{Start: st.Start}
		case p == 1:
			st.Bold = true
		case p == 2:
			st.Faint = true
		case p == 3:
			st.Italic = true
		case p == 4:
			st.Underline = true
		case p == 22:
			st.Bold, st.Faint = false, false
		case p == 23:
			st.Italic = false
		case p == 24:
			st.Underline = false
		case p >= 30 && p <= 37:
			st.FG = lipgloss.Color(strconv.Itoa(p - 30))
		case p == 38 || p == 48:
			c, skip := extendedColor(params[i+1:])
			if c == nil {
				return st // malformed extended colour: stop interpreting
			}
			if p == 38 {
				st.FG = c
			} else {
				st.BG = c
			}
			i += skip
		case p == 39:
			st.FG = nil
		case p >= 40 && p <= 47:
			st.BG = lipgloss.Color(strconv.Itoa(p - 40))
		case p == 49:
			st.BG = nil
		case p >= 90 && p <= 97:
			st.FG = lipgloss.Color(strconv.Itoa(p - 90 + 8))
		case p >= 100 && p <= 107:
			st.BG = lipgloss.Color(strconv.Itoa(p - 100 + 8))
		}
	}
	return st
}

// extendedColor decodes the 38/48 colour forms from the parameters following
// the introducer: "5;n" (256-colour index) or "2;r;g;b" (truecolor). It
// returns the colour and how many parameters were consumed, or nil when the
// form is malformed.
func extendedColor(rest []int) (color.Color, int) {
	if len(rest) >= 2 && rest[0] == 5 {
		return lipgloss.Color(strconv.Itoa(rest[1])), 2
	}
	if len(rest) >= 4 && rest[0] == 2 {
		clamp := func(v int) uint8 {
			if v < 0 {
				return 0
			}
			if v > 255 {
				return 255
			}
			return uint8(v)
		}
		return color.RGBA{R: clamp(rest[1]), G: clamp(rest[2]), B: clamp(rest[3]), A: 255}, 4
	}
	return nil, 0
}
