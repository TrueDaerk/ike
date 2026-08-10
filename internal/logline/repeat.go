// repeat.go builds the comparison key that collapses consecutive identical
// log lines (#1650). Polling services repeat the same line for pages, and what
// differs between the repeats is never the statement, only the numbers that
// run along with it: the timestamp, the elapsed time, the page, the counter.
// The key blanks every such moving part (variable.go finds them, #1758) and
// compares what is left. Lines without a recognized moving part compare
// verbatim.
package logline

import "sort"

// repeatBlank is the placeholder a blanked range leaves behind. Cutting the
// range out entirely could make two structurally different lines collide; a
// sentinel rune that never occurs in log text cannot.
const repeatBlank = "\x00"

// RepeatKey returns the key two lines must share to count as repeats of each
// other: the line with every moving part — timestamp, duration, page number,
// counter — replaced by a sentinel.
func RepeatKey(line string) string {
	rs := variableRanges(line)
	if len(rs) == 0 {
		return line
	}
	runes := []rune(line)
	var b []rune
	prev := 0
	for _, r := range rs {
		if r.Start > len(runes) {
			break
		}
		end := r.End
		if end > len(runes) {
			end = len(runes)
		}
		b = append(b, runes[prev:r.Start]...)
		b = append(b, []rune(repeatBlank)...)
		prev = end
	}
	if prev < len(runes) {
		b = append(b, runes[prev:]...)
	}
	return string(b)
}

// timestampRanges collects the timestamp ranges of one line — the header stamp
// Parse recognizes plus every KindTime logfmt value ScanPairs finds. It is the
// timestamp category of variableRanges, which adds the durations and counters.
func timestampRanges(line string) []Range {
	var rs []Range
	if p := Parse(line); !p.Timestamp.Empty() {
		rs = append(rs, p.Timestamp)
	}
	for _, pr := range ScanPairs(line) {
		if pr.Kind == KindTime && !pr.Value.Empty() {
			rs = append(rs, pr.Value)
		}
	}
	return mergeRanges(rs)
}

// mergeRanges sorts the ranges by start and merges the overlapping ones, so a
// splice over them stays ordered and never blanks the same span twice.
func mergeRanges(rs []Range) []Range {
	if len(rs) < 2 {
		return rs
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].Start < rs[j].Start })
	out := rs[:1]
	for _, r := range rs[1:] {
		last := &out[len(out)-1]
		if r.Start <= last.End {
			if r.End > last.End {
				last.End = r.End
			}
			continue
		}
		out = append(out, r)
	}
	return out
}
