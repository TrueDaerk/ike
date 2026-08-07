package editor

import (
	"fmt"
	"sort"
	"strings"

	"ike/internal/editor/buffer"
	"ike/internal/editor/excmd"
	"ike/internal/editor/history"
)

// sortFlags holds the parsed ":sort" flag letters plus the "!" reverse bang.
type sortFlags struct {
	numeric bool // n — compare the first decimal number in the line
	ignore  bool // i — case-insensitive comparison
	unique  bool // u — drop duplicate lines after sorting
	reverse bool // ! — invert the comparison
}

// exSort runs ":[range]sort[!] [flags]" — reorder the resolved range's lines in
// place. Unlike the other range commands, an empty range means the whole buffer
// (vim's default), matching how sorting is actually used. The rewrite is a
// single Edit over the range, so one "u" reverts it.
func (m Model) exSort(cmd excmd.Command) Model {
	start, end := 0, m.buf.LineCount()-1
	if cmd.Range.Count > 0 {
		s, e, rerr := cmd.Range.Resolve(m.exResolver(), m.cursor.Line)
		if rerr != "" {
			m.cmdMsg = "E: " + rerr
			return m
		}
		start, end = s, e
	}

	flags, ferr := parseSortFlags(cmd.Args)
	if ferr != "" {
		m.cmdMsg = "E: " + ferr
		return m
	}
	flags.reverse = cmd.Bang

	lines := make([]string, 0, end-start+1)
	for i := start; i <= end; i++ {
		lines = append(lines, m.buf.Line(i))
	}
	sorted, dropped := sortLines(lines, flags)

	if equalLines(lines, sorted) {
		m.cmdMsg = "already sorted"
		return m
	}
	m.mutate(func(rec *history.Recorder) buffer.Position {
		r := buffer.Range{
			Start: buffer.Position{Line: start, Col: 0},
			End:   buffer.Position{Line: end, Col: m.buf.RuneLen(end)},
		}
		rec.Apply(buffer.Edit{Range: r, Text: strings.Join(sorted, "\n")})
		return buffer.Position{Line: start, Col: 0}
	})
	m.cmdMsg = fmt.Sprintf("sorted %d line%s", len(sorted), plural(len(sorted), "s"))
	if dropped > 0 {
		m.cmdMsg += fmt.Sprintf(", %d duplicate%s removed", dropped, plural(dropped, "s"))
	}
	return m
}

// sortLine is one line decorated with its comparison keys, computed once so the
// comparator stays allocation-free.
type sortLine struct {
	text string
	key  string // lexicographic key (lowercased under "i")
	num  int64  // first number in the line, for "n"
	has  bool   // whether the line holds a number at all
}

// sortLines returns the sorted (and, with "u", deduplicated) copy of lines and
// the number of duplicates dropped. The sort is stable, so equal lines keep
// their original order — including under "!", which inverts the comparison
// rather than reversing the result.
func sortLines(lines []string, f sortFlags) ([]string, int) {
	dec := make([]sortLine, len(lines))
	for i, l := range lines {
		d := sortLine{text: l, key: l}
		if f.ignore {
			d.key = strings.ToLower(l)
		}
		if f.numeric {
			d.num, d.has = firstNumber(l)
		}
		dec[i] = d
	}
	sort.SliceStable(dec, func(i, j int) bool {
		if f.reverse {
			i, j = j, i
		}
		return sortLess(dec[i], dec[j], f.numeric)
	})

	out := make([]string, 0, len(dec))
	dropped := 0
	for i, d := range dec {
		// "u" drops a line equal to the one before it. Duplicates are adjacent
		// after sorting, and the identity is the (case-folded) whole line — the
		// numeric key groups lines that are not actually equal.
		if f.unique && i > 0 && d.key == dec[i-1].key {
			dropped++
			continue
		}
		out = append(out, d.text)
	}
	return out, dropped
}

// sortLess is the ascending comparison of two decorated lines. Under "n" the
// key is the first decimal number; lines without a number sort before every
// numbered line (vim's rule) and compare equal to each other, so stability keeps
// them in their original order.
func sortLess(a, b sortLine, numeric bool) bool {
	if numeric {
		switch {
		case !a.has && !b.has:
			return false
		case !a.has:
			return true
		case !b.has:
			return false
		}
		return a.num < b.num
	}
	return a.key < b.key
}

// firstNumber scans the first decimal number in s, including a directly
// preceding "-" sign. ok is false when the line holds no digits. The value
// saturates instead of wrapping so absurdly long digit runs still order
// sensibly.
func firstNumber(s string) (int64, bool) {
	i := strings.IndexFunc(s, func(r rune) bool { return r >= '0' && r <= '9' })
	if i < 0 {
		return 0, false
	}
	neg := i > 0 && s[i-1] == '-'
	var n int64
	for ; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		d := int64(s[i] - '0')
		if n > (1<<62)/10 {
			n = 1 << 62
			continue
		}
		n = n*10 + d
	}
	if neg {
		n = -n
	}
	return n, true
}

// parseSortFlags reads the n/i/u flag letters; an unknown letter is an error.
// "r" (sort only the matched pattern) and "/pat/" are not supported yet.
func parseSortFlags(args string) (sortFlags, string) {
	var f sortFlags
	for _, r := range args {
		switch r {
		case 'n':
			f.numeric = true
		case 'i':
			f.ignore = true
		case 'u':
			f.unique = true
		case ' ', '\t':
		default:
			return f, "unknown sort flag: " + string(r)
		}
	}
	return f, ""
}

// equalLines reports whether two line slices hold the same text in the same
// order — used to skip a no-op sort (and its undo entry).
func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
