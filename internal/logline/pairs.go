// pairs.go scans the logfmt `key=value` pairs of a whole log line (#1633).
// Parse only classifies the *header* — it stops at the first key it does not
// know, because everything after that is message payload — but logrus/slog
// text output keeps emitting pairs past that point, and left unstyled the
// structural noise ("Failed=0 Scanned=2 notify=no") reads as loud as the
// message itself. ScanPairs walks the full line with quote awareness so
// `msg="Session done"` stays one pair, and classifies each key so the span
// layer can dim the keys, emphasize the message and push secondary timestamps
// into the background like the leading one.
package logline

// PairKind is the role a logfmt key plays in the line.
type PairKind int

const (
	// KindPlain is a key the log layer knows nothing about; only its key
	// dims, the value renders in the default foreground.
	KindPlain PairKind = iota
	KindTime
	KindLevel
	KindName
	KindMessage
)

// Pair is one `key=value` pair found anywhere on the line.
type Pair struct {
	Key   Range // the key together with its "=" separator
	Value Range // the value, surrounding double quotes excluded
	Kind  PairKind
	Text  string // the value text, quotes stripped
}

// pairKinds maps the well-known logfmt keys onto their role. The spellings
// mirror what logrus, slog, zap and zerolog emit by default.
var pairKinds = map[string]PairKind{
	"time": KindTime, "ts": KindTime, "timestamp": KindTime,
	"datetime": KindTime, "date": KindTime,
	"level": KindLevel, "lvl": KindLevel, "severity": KindLevel, "sev": KindLevel,
	"logger": KindName, "thread": KindName, "caller": KindName, "name": KindName,
	"module": KindName, "component": KindName, "source": KindName,
	"msg": KindMessage, "message": KindMessage,
}

// ScanPairs returns every logfmt pair on the line, left to right. A line that
// carries no pair at all yields nothing, so plain prose and stack-trace frames
// render unchanged.
func ScanPairs(line string) []Pair {
	runes := []rune(line)
	var out []Pair
	i := 0
	for i < len(runes) {
		for i < len(runes) && isSpace(runes[i]) {
			i++
		}
		if i >= len(runes) {
			break
		}
		ks := i
		for i < len(runes) && !isSpace(runes[i]) && runes[i] != '=' {
			i++
		}
		key := string(runes[ks:i])
		if i >= len(runes) || runes[i] != '=' || ks == i || !keyRe.MatchString(key) {
			// Not a pair — skip the rest of the word. A quoted stretch is
			// skipped as a whole so a space inside it cannot start a "key".
			i = skipWord(runes, i)
			continue
		}
		i++ // the '='
		vs := i
		i = scanValue(runes, i)
		ve := i
		if ve-vs >= 2 && runes[vs] == '"' && runes[ve-1] == '"' {
			vs, ve = vs+1, ve-1
		}
		out = append(out, Pair{
			Key:   Range{ks, ks + len([]rune(key)) + 1},
			Value: Range{vs, ve},
			Kind:  pairKinds[lower(key)],
			Text:  string(runes[vs:ve]),
		})
	}
	return out
}

// Logfmt reports whether the pairs are structured enough to style. A single
// unknown pair — an `a=b` inside a prose sentence — is left alone; two pairs,
// or one recognized key, mark the line as logfmt output.
func Logfmt(pairs []Pair) bool {
	if len(pairs) >= 2 {
		return true
	}
	for _, p := range pairs {
		if p.Kind != KindPlain {
			return true
		}
	}
	return false
}

// scanValue consumes the value starting at i: a double-quoted run (backslash
// escapes honored) or everything up to the next whitespace.
func scanValue(runes []rune, i int) int {
	if i < len(runes) && runes[i] == '"' {
		i++
		for i < len(runes) {
			switch runes[i] {
			case '\\':
				i += 2
				continue
			case '"':
				return i + 1
			}
			i++
		}
		return len(runes)
	}
	for i < len(runes) && !isSpace(runes[i]) {
		i++
	}
	return i
}

// skipWord consumes the remainder of a non-pair word, quoted stretches
// included.
func skipWord(runes []rune, i int) int {
	for i < len(runes) && !isSpace(runes[i]) {
		if runes[i] == '"' {
			i = scanValue(runes, i)
			continue
		}
		i++
	}
	return i
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' }

// lower is ASCII-only lowercasing — logfmt keys never carry anything else and
// this keeps the per-line scan allocation free for the common case.
func lower(s string) string {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 'A' && c <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if b[j] >= 'A' && b[j] <= 'Z' {
					b[j] += 'a' - 'A'
				}
			}
			return string(b)
		}
	}
	return s
}
