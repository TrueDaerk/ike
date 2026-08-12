//go:build cgo

package langjson

import (
	"strings"
	"testing"

	"ike/internal/highlight"
)

// TestRainbowBracketAdjacentToHintSpans (#1785): Go-produced spans (epoch
// stand-ins #1618, number hints #1627, escape decodes #1620, network and cron
// hints) are prepended in HighlightScoped and win over bracket spans under
// first-covering-wins — so a hint whose EndCol overshot by one cell would
// paint the neighbouring bracket in the hint's colour. That is the one
// mechanism in the pipeline that could produce the reported symptom (last
// bracket of the file in a wrong rainbow colour), so every hint family is
// pinned here with its literal directly against a closing bracket that is the
// file's last byte.
func TestRainbowBracketAdjacentToHintSpans(t *testing.T) {
	cases := []string{
		`{"t":1755000000}`,  // epoch stand-in directly before the final }
		`{"a":[123456789]}`, // digit-grouping hint before ]
		`{"n":1234567}`,
		`{"b":[1,2,{"t":1754953327}]}`,
		`{"size":1048576}`, // byte-size hint
		`{"ms":86400000}`,  // duration hint
		`{"hex":"0xdeadbeef"}`,
		"{\"s\":\"a\\u00e9A\"}", // unicode-escape decode before "}
		`{"s":"aéA"}`,           // multibyte rune before "} (colMapper path)
		`{"host":"xn--e1awd7f.example"}`,
		`{"cidr":"10.0.0.0/8"}`,
		`{"cron":"*/5 * * * *"}`,
		`{"password":"xyz"}`, // secret mask (#1813) before the closing quote and }
	}
	for _, doc := range cases {
		lines := []string{doc}
		spans := highlight.Highlight("x.json", lines)
		ix := highlight.NewIndex(spans)
		last := len([]rune(doc)) - 1
		if got, want := ix.CaptureAt(0, last), highlight.RainbowCapture(0); got != want {
			t.Errorf("%s: final bracket capture %q, want %q", doc, got, want)
		}
		brackets := map[rune]bool{'{': true, '}': true, '[': true, ']': true}
		inString := false
		for col, r := range []rune(doc) {
			if r == '"' {
				inString = !inString
			}
			if !inString && brackets[r] {
				if c := ix.CaptureAt(0, col); !strings.HasPrefix(c, "rainbow.") {
					t.Errorf("%s: bracket %q at col %d capture %q, not a rainbow slot", doc, string(r), col, c)
				}
			}
		}
	}
}
