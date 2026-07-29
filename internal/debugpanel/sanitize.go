package debugpanel

import "strings"

// sanitize.go holds the ANSI stripper the debug session log shares (#637):
// escapes must not reach the transcript file raw.

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
