package transport

import (
	"os"
	"strings"
)

// errtail.go: crash forensics helpers (#990). A crashing server's stderr tail
// often buries the decisive error under noise — Node prints the offending
// source "line" (megabytes of minified bundle) before the real
// `SomeError: message` plus stack. ErrorLine digs the message out so status
// toasts can name the error instead of just "crashed"; FreshLine keeps log
// markers off unterminated stderr lines.

// maxErrorLineLen: anything longer is dump noise (a minified bundle line),
// never a human-written message.
const maxErrorLineLen = 300

// errorLineCap bounds what a status toast has to carry.
const errorLineCap = 200

// ErrorLine extracts the decisive error message from a stderr tail: scanning
// backwards, the last reasonably short non-stack-frame line that names an
// error (error/exception/panic/fatal/failed). Returns "" when nothing
// qualifies — callers keep their generic message then.
func ErrorLine(stderr string) string {
	lines := strings.Split(stderr, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" || len(l) > maxErrorLineLen || !looksLikeError(l) {
			continue
		}
		if r := []rune(l); len(r) > errorLineCap {
			l = string(r[:errorLineCap]) + "…"
		}
		return l
	}
	return ""
}

// looksLikeError reports whether a trimmed stderr line reads like an error
// message rather than a stack frame or plain chatter.
func looksLikeError(l string) bool {
	low := strings.ToLower(l)
	if strings.HasPrefix(low, "at ") {
		return false // stack frame ("at Object.error (…)") — keep scanning up
	}
	for _, w := range []string{"error", "exception", "panic:", "fatal", "failed"} {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}

// FreshLine pads f with a newline when its last byte is not one, so a marker
// line never glues onto an unterminated stderr write. Needs a read-capable
// handle; best-effort like all logging here.
func FreshLine(f *os.File) {
	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		return
	}
	b := make([]byte, 1)
	if _, err := f.ReadAt(b, st.Size()-1); err != nil || b[0] == '\n' {
		return
	}
	_, _ = f.WriteString("\n")
}
