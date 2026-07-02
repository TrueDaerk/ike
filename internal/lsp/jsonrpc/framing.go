package jsonrpc

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// framing.go implements the LSP base protocol: each message is a JSON payload
// prefixed with a `Content-Length: N\r\n\r\n` header block (other headers, e.g.
// Content-Type, are tolerated and ignored).

// writeFrame writes payload with its Content-Length header.
func writeFrame(w io.Writer, payload []byte) error {
	if _, err := io.WriteString(w, fmt.Sprintf("Content-Length: %d\r\n\r\n", len(payload))); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// readFrame reads one framed message, returning its JSON payload. It returns the
// underlying error (e.g. io.EOF) when the stream ends.
func readFrame(r *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break // end of headers
		}
		if v, ok := cutHeader(line, "Content-Length:"); ok {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil {
				return nil, fmt.Errorf("jsonrpc: bad Content-Length %q: %w", v, err)
			}
			contentLength = n
		}
		// Any other header (Content-Type, …) is ignored.
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("jsonrpc: missing Content-Length header")
	}
	buf := make([]byte, contentLength)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// cutHeader splits a header line on its name prefix (case-insensitive).
func cutHeader(line, name string) (string, bool) {
	if len(line) < len(name) {
		return "", false
	}
	if !strings.EqualFold(line[:len(name)], name) {
		return "", false
	}
	return line[len(name):], true
}
