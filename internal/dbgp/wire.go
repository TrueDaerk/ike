// Package dbgp implements the client side of the DBGp protocol (Xdebug's
// native debugging protocol, https://xdebug.org/docs/dbgp): NUL-delimited
// XML packets from the engine, NUL-terminated command lines to it. The
// package is protocol-only — the DAP bridge (#699) builds on it.
package dbgp

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// maxPacket bounds one engine packet; a larger announced length means a
// corrupt stream, not a legitimate payload.
const maxPacket = 64 << 20

// readPacket reads one engine packet: ASCII length, NUL, XML payload, NUL.
func readPacket(r *bufio.Reader) ([]byte, error) {
	lenStr, err := r.ReadString(0)
	if err != nil {
		return nil, err
	}
	lenStr = strings.TrimSuffix(lenStr, "\x00")
	var n int
	if _, err := fmt.Sscanf(lenStr, "%d", &n); err != nil {
		return nil, fmt.Errorf("dbgp: malformed packet length %q", lenStr)
	}
	if n < 0 || n > maxPacket {
		return nil, fmt.Errorf("dbgp: implausible packet length %d", n)
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	// Trailing NUL after the payload.
	if b, err := r.ReadByte(); err != nil {
		return nil, err
	} else if b != 0 {
		return nil, fmt.Errorf("dbgp: missing packet terminator (got %#x)", b)
	}
	return data, nil
}

// writeCommand frames one command line: `name -i tid [flags...] [-- b64data]`
// terminated by NUL. Flag values are pre-quoted by the caller (quoteArg).
func writeCommand(w io.Writer, line string) error {
	_, err := io.WriteString(w, line+"\x00")
	return err
}

// quoteArg quotes a command argument for the DBGp command line. Plain
// tokens pass through; anything with spaces, quotes, or backslashes is
// wrapped in double quotes with backslash escaping, per the spec.
func quoteArg(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"\\'\x00") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}
