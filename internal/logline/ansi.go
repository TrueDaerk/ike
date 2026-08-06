// ansi.go scans a log line for ANSI escape sequences (#1621): every CSI
// sequence is reported for concealment (raw escape bytes are unreadable
// noise), and the SGR ones ("…m") additionally drive a running style so the
// text between them renders in the colors the emitting program intended. The
// style travels as a capture spec ("ansi.fg1.bold") through the ordinary
// span pipeline; Spec/ParseSpec are the two ends of that encoding.
package logline

import (
	"fmt"
	"strconv"
	"strings"
)

// SGRStyle is the accumulated SGR state applying to a run of text. Fg/Bg are
// "" for unset, a decimal palette index ("1", "196") or a "#rrggbb" literal
// from a 24-bit sequence. Indexes 0–15 resolve against the theme's terminal
// palette in the editor; higher ones stay 256-color indexes.
type SGRStyle struct {
	Fg, Bg                         string
	Bold, Faint, Italic, Underline bool
}

// IsZero reports whether the style carries nothing.
func (s SGRStyle) IsZero() bool { return s == SGRStyle{} }

// Escape is the [Start, End) rune-column range of one escape sequence.
type Escape struct {
	Start, End int
}

// Run is a stretch of visible text with a non-zero SGR style.
type Run struct {
	Start, End int
	Style      SGRStyle
}

// ScanSGR scans line for CSI escape sequences: escapes lists every sequence's
// range (for concealment), runs the styled text stretches between them. An
// unterminated sequence stays raw text.
func ScanSGR(line string) (escapes []Escape, runs []Run) {
	runes := []rune(line)
	var cur SGRStyle
	runStart := -1
	closeRun := func(end int) {
		if runStart >= 0 && end > runStart && !cur.IsZero() {
			runs = append(runs, Run{Start: runStart, End: end, Style: cur})
		}
		runStart = -1
	}
	i := 0
	for i < len(runes) {
		if runes[i] == 0x1b && i+1 < len(runes) && runes[i+1] == '[' {
			j := i + 2
			for j < len(runes) && !(runes[j] >= 0x40 && runes[j] <= 0x7e) {
				j++
			}
			if j < len(runes) {
				closeRun(i)
				escapes = append(escapes, Escape{Start: i, End: j + 1})
				if runes[j] == 'm' {
					cur = applySGR(cur, string(runes[i+2:j]))
				}
				i = j + 1
				continue
			}
		}
		if runStart < 0 {
			runStart = i
		}
		i++
	}
	closeRun(len(runes))
	return escapes, runs
}

// applySGR folds one SGR parameter string into s. Parameters split on ";"
// (and ":" — the colon form some emitters use for extended colors).
func applySGR(s SGRStyle, params string) SGRStyle {
	if params == "" {
		return SGRStyle{} // bare ESC[m is a reset
	}
	fields := strings.FieldsFunc(params, func(r rune) bool { return r == ';' || r == ':' })
	nums := make([]int, 0, len(fields))
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil {
			return s // malformed: leave the style untouched
		}
		nums = append(nums, n)
	}
	for k := 0; k < len(nums); k++ {
		switch n := nums[k]; {
		case n == 0:
			s = SGRStyle{}
		case n == 1:
			s.Bold = true
		case n == 2:
			s.Faint = true
		case n == 3:
			s.Italic = true
		case n == 4:
			s.Underline = true
		case n == 21 || n == 22:
			s.Bold, s.Faint = false, false
		case n == 23:
			s.Italic = false
		case n == 24:
			s.Underline = false
		case n >= 30 && n <= 37:
			s.Fg = strconv.Itoa(n - 30)
		case n == 38 || n == 48:
			var c string
			c, k = extColor(nums, k)
			if c != "" {
				if n == 38 {
					s.Fg = c
				} else {
					s.Bg = c
				}
			}
		case n == 39:
			s.Fg = ""
		case n >= 40 && n <= 47:
			s.Bg = strconv.Itoa(n - 40)
		case n == 49:
			s.Bg = ""
		case n >= 90 && n <= 97:
			s.Fg = strconv.Itoa(n - 90 + 8)
		case n >= 100 && n <= 107:
			s.Bg = strconv.Itoa(n - 100 + 8)
		}
	}
	return s
}

// extColor decodes an extended color at nums[k] (38/48): "5;n" indexed or
// "2;r;g;b" truecolor. It returns the color token and the last index it
// consumed; a malformed tail consumes everything and yields no color.
func extColor(nums []int, k int) (string, int) {
	if k+1 < len(nums) {
		switch nums[k+1] {
		case 5:
			if k+2 < len(nums) && nums[k+2] >= 0 && nums[k+2] <= 255 {
				return strconv.Itoa(nums[k+2]), k + 2
			}
		case 2:
			if k+4 < len(nums) {
				return fmt.Sprintf("#%02x%02x%02x",
					nums[k+2]&0xff, nums[k+3]&0xff, nums[k+4]&0xff), k + 4
			}
		}
	}
	return "", len(nums)
}

// Spec encodes the style as the dotted capture suffix ("fg1.bold"); "" for
// the zero style.
func (s SGRStyle) Spec() string {
	var parts []string
	if s.Fg != "" {
		parts = append(parts, "fg"+s.Fg)
	}
	if s.Bg != "" {
		parts = append(parts, "bg"+s.Bg)
	}
	if s.Bold {
		parts = append(parts, "bold")
	}
	if s.Faint {
		parts = append(parts, "faint")
	}
	if s.Italic {
		parts = append(parts, "italic")
	}
	if s.Underline {
		parts = append(parts, "underline")
	}
	return strings.Join(parts, ".")
}

// ParseSpec decodes a Spec back into a style; ok=false for an empty or
// unrecognized spec.
func ParseSpec(spec string) (SGRStyle, bool) {
	var s SGRStyle
	if spec == "" {
		return s, false
	}
	for _, p := range strings.Split(spec, ".") {
		switch {
		case p == "bold":
			s.Bold = true
		case p == "faint":
			s.Faint = true
		case p == "italic":
			s.Italic = true
		case p == "underline":
			s.Underline = true
		case strings.HasPrefix(p, "fg") && len(p) > 2:
			s.Fg = p[2:]
		case strings.HasPrefix(p, "bg") && len(p) > 2:
			s.Bg = p[2:]
		default:
			return SGRStyle{}, false
		}
	}
	return s, true
}
