package debugpanel

import "strings"

// sanitize.go cleans debuggee output before it is buffered (#637): programs
// print ANSI colour/cursor escapes, carriage-return progress bars and tabs,
// none of which may reach the TUI raw — an escape injected into a rendered
// row corrupts the whole frame, and truncate would happily cut mid-sequence.

// StripANSI removes ANSI escape sequences from s: CSI (ESC [ … final byte),
// OSC (ESC ] … BEL or ESC \) and two-byte ESC sequences. Plain text and the
// \n/\r/\t controls pass through untouched. Exported so the session-log
// writer (internal/app) strips the same way the panel does.
func StripANSI(s string) string {
	if !strings.ContainsRune(s, 0x1b) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != 0x1b {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(s) {
			break
		}
		switch s[i] {
		case '[': // CSI: parameter/intermediate bytes, then a final byte 0x40–0x7e
			for i++; i < len(s); i++ {
				if s[i] >= 0x40 && s[i] <= 0x7e {
					break
				}
			}
		case ']': // OSC: runs until BEL or the ESC \ string terminator
			for i++; i < len(s); i++ {
				if s[i] == 0x07 {
					break
				}
				if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
					i++
					break
				}
			}
		default:
			// A two-byte ESC sequence (ESC c, ESC =, …): the byte at i is the
			// sequence's second half and is dropped with the ESC.
		}
	}
	return b.String()
}

// sanitizeLine cleans one completed output line for display: ANSI escapes are
// stripped; a carriage return keeps only the text after the last \r (the
// progress-bar overwrite semantic, minimal form — a trailing \r from a CRLF
// line ending is dropped first so Windows-style output survives intact); tabs
// expand to 8-column stops; remaining C0 control bytes are removed.
func sanitizeLine(s string) string {
	s = StripANSI(s)
	s = strings.TrimSuffix(s, "\r")
	if i := strings.LastIndexByte(s, '\r'); i >= 0 {
		s = s[i+1:]
	}
	if strings.IndexFunc(s, isControl) < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	col := 0
	for _, r := range s {
		switch {
		case r == '\t':
			n := 8 - col%8
			b.WriteString(strings.Repeat(" ", n))
			col += n
		case isControl(r):
			// Other control runes (BEL, backspace, …) carry no text.
		default:
			b.WriteRune(r)
			col++
		}
	}
	return b.String()
}

// isControl reports a C0 control rune or DEL.
func isControl(r rune) bool { return r < 0x20 || r == 0x7f }
