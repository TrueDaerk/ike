// delta.go turns the timestamp range the #1621 parser marks on each line into
// an actual wall-clock instant, and from the instants into the inter-line
// elapsed times the editor renders as dimmed `+30s` hints (#1651). Stalls and
// slow operations are the thing one reads a log for, and a column of deltas
// makes them visible without doing the subtraction by eye.
//
// Two properties matter for the chain. A line whose timestamp does not parse —
// a stack-trace frame, a wrapped message, a banner — produces no hint but does
// *not* reset the previous instant, so the next stamped line measures against
// the last real one instead of restarting. And a delta that is not strictly
// positive produces no hint either: second-resolution logs put many lines in
// the same second, and a `+0ms` on most rows would be pure noise.
package logline

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"ike/internal/epochtime"
)

// Stamp is one line's parsed timestamp. HasDate records whether the layout
// carried a calendar date: a time-only stamp ("10:11:12,345") anchors on an
// arbitrary base day, so it may only be subtracted from another dateless one.
type Stamp struct {
	Time    time.Time
	HasDate bool
}

// Delta is the elapsed time before one line. OK is false for a line with no
// hint — no parseable stamp, no predecessor, or a non-positive difference.
// Gap marks a delta at or above the file's stall threshold.
type Delta struct {
	D   time.Duration
	OK  bool
	Gap bool
}

// baseYear anchors the layouts that carry no year. 2000 is a leap year, so a
// syslog "Feb 29" still parses.
const baseYear = 2000

// Gap thresholding. A stall is relative to what the file usually does — a
// millisecond-cadence trace and a heartbeat that ticks every minute stall at
// very different scales — so the threshold is a multiple of the median delta,
// floored so a fast file does not flag ordinary jitter.
const (
	gapFactor = 10
	gapFloor  = time.Second
)

var (
	// stampRe matches the ISO-ish layouts: an optional date, the clock, an
	// optional fraction (".345", ",345", ":345") and an optional zone.
	stampRe = regexp.MustCompile(
		`^(?:(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})[T_ ]+)?` +
			`(\d{1,2}):(\d{2}):(\d{2})(?:[.,:](\d{1,9}))?` +
			`(Z|[+-]\d{2}:?\d{2})?$`)
	// syslogRe matches "Jan  2 10:11:12" — month name, day, clock, no year.
	syslogRe = regexp.MustCompile(
		`^(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+(\d{1,2})\s+` +
			`(\d{1,2}):(\d{2}):(\d{2})(?:[.,](\d{1,9}))?$`)
	digitsRe = regexp.MustCompile(`^\d+$`)
	months   = map[string]time.Month{
		"Jan": time.January, "Feb": time.February, "Mar": time.March,
		"Apr": time.April, "May": time.May, "Jun": time.June,
		"Jul": time.July, "Aug": time.August, "Sep": time.September,
		"Oct": time.October, "Nov": time.November, "Dec": time.December,
	}
)

// ParseStamp returns the instant of one log line's header timestamp. It reuses
// Parse for *locating* the stamp — so every layout the renderer recognizes,
// including a `time=…` logfmt value, is covered — and only decodes the text it
// marked. ANSI-styled lines are stripped first, like the span builder does.
func ParseStamp(line string) (Stamp, bool) {
	escapes, _ := ScanSGR(line)
	visible, _ := visibleText(line, escapes)
	p := Parse(visible)
	if p.Timestamp.Empty() {
		return Stamp{}, false
	}
	runes := []rune(visible)
	start, end := p.Timestamp.Start, p.Timestamp.End
	if start < 0 || start >= len(runes) {
		return Stamp{}, false
	}
	if end > len(runes) {
		end = len(runes)
	}
	return parseStampText(string(runes[start:end]))
}

// parseStampText decodes one timestamp token: a bracketed group unwraps, a
// bare digit run decodes as a Unix epoch, and the two regexes cover the
// dated/dateless ISO layouts and syslog.
func parseStampText(text string) (Stamp, bool) {
	text = strings.TrimSpace(text)
	if inner, ok := stripWrap(text, '[', ']'); ok {
		text = strings.TrimSpace(inner)
	}
	if digitsRe.MatchString(text) {
		if t, ok := epochtime.Time(text); ok {
			return Stamp{Time: t, HasDate: true}, true
		}
		return Stamp{}, false
	}
	if m := stampRe.FindStringSubmatch(text); m != nil {
		year, month, day := baseYear, 1, 1
		hasDate := m[1] != ""
		if hasDate {
			year, month, day = atoi(m[1]), atoi(m[2]), atoi(m[3])
		}
		t := time.Date(year, time.Month(month), day,
			atoi(m[4]), atoi(m[5]), atoi(m[6]), fraction(m[7]), zone(m[8]))
		return Stamp{Time: t, HasDate: hasDate}, true
	}
	if m := syslogRe.FindStringSubmatch(text); m != nil {
		t := time.Date(baseYear, months[m[1]], atoi(m[2]),
			atoi(m[3]), atoi(m[4]), atoi(m[5]), fraction(m[6]), time.UTC)
		return Stamp{Time: t, HasDate: true}, true
	}
	return Stamp{}, false
}

// fraction turns a sub-second digit group into nanoseconds, right-padding the
// short forms ("345" is 345 ms, not 345 ns).
func fraction(digits string) int {
	if digits == "" {
		return 0
	}
	if len(digits) > 9 {
		digits = digits[:9]
	}
	return atoi(digits+strings.Repeat("0", 9-len(digits))) % int(time.Second)
}

// zone decodes an offset suffix ("Z", "+02:00", "-0500"); an absent one means
// UTC, which is consistent across the file and therefore cancels out in every
// subtraction.
func zone(spec string) *time.Location {
	if spec == "" || spec == "Z" {
		return time.UTC
	}
	digits := strings.ReplaceAll(spec[1:], ":", "")
	if len(digits) != 4 {
		return time.UTC
	}
	off := atoi(digits[:2])*3600 + atoi(digits[2:])*60
	if spec[0] == '-' {
		off = -off
	}
	return time.FixedZone(spec, off)
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// Deltas returns one Delta per line: the elapsed time since the previous line
// that carried a parseable timestamp of a comparable kind. Unparseable lines
// leave the chain intact, and a dated stamp is never subtracted from a
// dateless one — their base days are unrelated.
func Deltas(lines []string) []Delta {
	out := make([]Delta, len(lines))
	var prev Stamp
	var havePrev bool
	var seen []time.Duration
	for i, line := range lines {
		s, ok := ParseStamp(line)
		if !ok {
			continue
		}
		if havePrev && s.HasDate == prev.HasDate {
			d := s.Time.Sub(prev.Time)
			if d < 0 && !s.HasDate {
				// A time-only file crossing midnight: the clock wrapped, the
				// day did not, so the difference comes out negative by a day.
				d += 24 * time.Hour
			}
			if d > 0 {
				out[i] = Delta{D: d, OK: true}
				seen = append(seen, d)
			}
		}
		prev, havePrev = s, true
	}
	th := GapThreshold(seen)
	for i := range out {
		if out[i].OK && out[i].D >= th {
			out[i].Gap = true
		}
	}
	return out
}

// SpanDelta returns the elapsed time between the first and the last
// timestamped line of a block — what a visual selection over a log measures
// (#1729). Only the outermost stamps count: everything between them may be
// stack frames, wrapped messages or banners without a timestamp.
//
// ok is false with fewer than two comparable stamps. Comparability follows the
// per-line chain: the first stamp's kind wins, and a stamp of the other kind
// (dated vs time-only, whose base day is arbitrary) is ignored rather than
// subtracted. A time-only block crossing midnight wraps forward, like Deltas.
func SpanDelta(lines []string) (time.Duration, bool) {
	var first, last Stamp
	var have int
	for _, line := range lines {
		s, ok := ParseStamp(line)
		if !ok {
			continue
		}
		if have == 0 {
			first = s
			have = 1
			continue
		}
		if s.HasDate != first.HasDate {
			continue
		}
		last = s
		have = 2
	}
	if have < 2 {
		return 0, false
	}
	d := last.Time.Sub(first.Time)
	if d < 0 && !first.HasDate {
		d += 24 * time.Hour
	}
	return d, true
}

// GapThreshold returns the duration at which a delta counts as a stall for a
// file with these deltas: ten times the median cadence, never below a second.
// An empty input yields the floor, so a file with a single delta only flags a
// genuinely long one.
func GapThreshold(deltas []time.Duration) time.Duration {
	if len(deltas) == 0 {
		return gapFloor
	}
	sorted := append([]time.Duration(nil), deltas...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	median := sorted[len(sorted)/2]
	if th := median * gapFactor; th > gapFloor {
		return th
	}
	return gapFloor
}

// deltaParts splits an elapsed time into the two unit fields the hint shows:
// the coarsest unit that carries a signal plus the next one down, dropped when
// it is zero or below the scale worth reading ("+45s" says everything; "+45s
// 123ms" is noise). Both fields are empty for a non-positive duration.
func deltaParts(d time.Duration) (hi, lo string) {
	num := func(v time.Duration, unit string) string {
		return strconv.FormatInt(int64(v), 10) + unit
	}
	drop := func(hi string, v time.Duration, unit string) (string, string) {
		if v == 0 {
			return hi, ""
		}
		return hi, num(v, unit)
	}
	switch {
	case d <= 0:
		return "", ""
	case d < time.Second:
		return "", num(d/time.Millisecond, "ms")
	case d < 10*time.Second:
		// Sub-second precision still reads at this scale: "+2s 100ms" beats
		// both "+2s" and "+2100ms".
		return drop(num(d/time.Second, "s"), (d%time.Second)/time.Millisecond, "ms")
	case d < time.Minute:
		return num(d/time.Second, "s"), ""
	case d < time.Hour:
		return drop(num(d/time.Minute, "m"), (d%time.Minute)/time.Second, "s")
	case d < 24*time.Hour:
		return drop(num(d/time.Hour, "h"), (d%time.Hour)/time.Minute, "m")
	default:
		return drop(num(d/(24*time.Hour), "d"), (d%(24*time.Hour))/time.Hour, "h")
	}
}

// FormatDelta renders an elapsed time as a standalone hint: the two unit fields
// separated by a space — "+450ms", "+2s 100ms", "+30s", "+2m 30s", "+1h 5m",
// "+1d 2h". An empty string means there is nothing to show. Buffer-wide hints
// go through DeltaLayout instead, which pads the same fields into a column.
func FormatDelta(d time.Duration) string {
	hi, lo := deltaParts(d)
	switch {
	case hi == "" && lo == "":
		return ""
	case hi == "":
		return "+" + lo
	case lo == "":
		return "+" + hi
	}
	return "+" + hi + " " + lo
}

// DeltaLayout is the shared shape of one buffer's delta hints (#1730): the
// column width of the coarse and the fine unit field, taken over every delta in
// the file. Formatting through it right-aligns both fields, so adjacent rows
// line up on their unit boundaries instead of forming a ragged trail — the
// whole point of putting the hints in a column. The zero layout formats like
// FormatDelta.
type DeltaLayout struct {
	Hi, Lo int
}

// LayoutDeltas measures the hint column of a whole buffer. Only deltas that
// actually render count; the chain is cached per document version anyway, so
// the extra pass is cheap.
func LayoutDeltas(ds []Delta) DeltaLayout {
	var l DeltaLayout
	for _, d := range ds {
		if !d.OK {
			continue
		}
		hi, lo := deltaParts(d.D)
		if n := len(hi); n > l.Hi {
			l.Hi = n
		}
		if n := len(lo); n > l.Lo {
			l.Lo = n
		}
	}
	return l
}

// Width is the column width every hint of this layout occupies, trailing pad
// included — what the renderer reserves at the row's right edge.
func (l DeltaLayout) Width() int {
	w := 1 + l.Hi + l.Lo // the "+" plus both fields
	if l.Hi > 0 && l.Lo > 0 {
		w++ // the separating space
	}
	return w
}

// Format renders one delta padded to the layout. Every hint of a buffer comes
// out the same width, with the coarse and the fine field right-aligned in their
// own columns, so "+ 7s 300ms", "+    598ms" and "+12m   8s" stack. An empty
// string means there is nothing to show.
func (l DeltaLayout) Format(d time.Duration) string {
	hi, lo := deltaParts(d)
	if hi == "" && lo == "" {
		return ""
	}
	if l.Hi == 0 && l.Lo == 0 {
		return FormatDelta(d)
	}
	var b strings.Builder
	b.Grow(l.Width())
	b.WriteByte('+')
	if l.Hi > 0 {
		b.WriteString(padLeft(hi, l.Hi))
	}
	if l.Lo > 0 {
		if l.Hi > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(padLeft(lo, l.Lo))
	}
	return b.String()
}

// padLeft right-aligns s in a field of width w. The hint text is ASCII digits
// and unit letters, so byte length is column width.
func padLeft(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return strings.Repeat(" ", w-len(s)) + s
}
