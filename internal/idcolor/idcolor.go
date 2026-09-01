// Package idcolor detects opaque identifiers — UUIDs and long hex hashes
// (git SHAs, request/trace/correlation IDs) — and maps each one onto a stable
// slot of the shared rainbow palette (#1626).
//
// The slot is the hash of the identifier itself, so every occurrence of the
// same identifier renders in the same color across lines, panes and files: in
// a log or a JSON payload it becomes visible at a glance which lines belong to
// the same request, without reading a single hex digit. It is the #1589
// palette mechanic keyed on content instead of column index, the same trick
// the log thread/logger names use (internal/logline).
//
// Detection is deliberately conservative — an identifier only counts when it
// stands on its own (delimited by a non-alphanumeric rune), a bare hex run has
// to reach the configurable minimum length, and an all-digit run never counts,
// because a plain number is not an identifier. Color literals (`#deadbe`) are
// excluded so the inline color preview (#790) keeps them.
package idcolor

import (
	"hash/fnv"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	"ike/internal/highlight"
)

// DefaultMinLength is the default minimum length of a bare hex run: 7, the
// length of an abbreviated git SHA. Shorter runs are far more often ordinary
// literals than identifiers.
const DefaultMinLength = 7

// MinLengthFloor is the smallest configurable minimum: below it a hex run
// carries no identity at all (and `#rrggbb` colors would start matching).
const MinLengthFloor = 6

// Span is one detected identifier: the half-open rune-column range on the
// line plus the rainbow slot its text hashes to.
type Span struct {
	Start, End int
	Slot       int
}

var (
	// uuidRe is the canonical 8-4-4-4-12 form. Matched first, so its groups
	// never also match as bare hex runs.
	uuidRe = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	// hexRe is a bare hex run; the length floor and the all-digit rejection
	// are applied by the scanner, not the pattern, because the minimum is
	// configurable.
	hexRe = regexp.MustCompile(`[0-9a-fA-F]+`)
)

// enabled / minLength are the global settings for consumers that have no
// config plumbing of their own (the .http response pane). The editor keeps a
// per-view toggle instead. Atomics: config reloads run on the UI loop while
// renders may read from elsewhere.
var (
	disabled atomic.Bool
	minLen   atomic.Int64
)

// SetEnabled gates identifier coloring for the global consumers.
func SetEnabled(on bool) { disabled.Store(!on) }

// Enabled reports whether identifier coloring is active globally.
func Enabled() bool { return !disabled.Load() }

// SetMinLength sets the global minimum hex-run length (clamped at the floor).
func SetMinLength(n int) { minLen.Store(int64(Clamp(n))) }

// MinLength returns the global minimum hex-run length.
func MinLength() int {
	if n := minLen.Load(); n > 0 {
		return int(n)
	}
	return DefaultMinLength
}

// Clamp bounds a configured minimum length to something meaningful.
func Clamp(n int) int {
	if n < MinLengthFloor {
		return MinLengthFloor
	}
	return n
}

// Scan returns the identifiers on one line, in ascending column order. minLen
// is the minimum length of a bare hex run; values below the floor are clamped.
func Scan(line string, minLen int) []Span {
	minLen = Clamp(minLen)
	var cands [][2]int              // accepted byte ranges, per pass in order
	claimed := make([][2]int, 0, 4) // byte ranges the UUID pass took
	for _, loc := range uuidRe.FindAllStringIndex(line, -1) {
		// Claimed even when the UUID itself is not standalone, so the hex
		// pass never colors a lone group of a rejected UUID.
		claimed = append(claimed, [2]int{loc[0], loc[1]})
		if !standalone(line, loc[0], loc[1]) {
			continue
		}
		cands = append(cands, [2]int{loc[0], loc[1]})
	}
	for _, loc := range hexRe.FindAllStringIndex(line, -1) {
		if loc[1]-loc[0] < minLen || overlaps(claimed, loc[0], loc[1]) {
			continue
		}
		if !standalone(line, loc[0], loc[1]) || allDigits(line[loc[0]:loc[1]]) {
			continue
		}
		cands = append(cands, [2]int{loc[0], loc[1]})
	}
	sortByStart(cands)
	// One incremental byte→rune walk over the whole line converts every
	// candidate: counting from the line start per match made a line with many
	// identifiers quadratic (#2386) — the response viewer paid it per frame.
	out := make([]Span, 0, len(cands))
	bytePos, runeCol := 0, 0
	for _, c := range cands {
		runeCol += utf8.RuneCountInString(line[bytePos:c[0]])
		start := runeCol
		runeCol += utf8.RuneCountInString(line[c[0]:c[1]])
		bytePos = c[1]
		out = append(out, Span{Start: start, End: runeCol, Slot: Slot(line[c[0]:c[1]])})
	}
	return out
}

// standalone reports whether the byte range [start, end) is delimited on both
// sides: neither neighbour may be a letter or digit (that would make the run
// part of a longer word, e.g. the tail of `0xdeadbeef` or of a base64 blob),
// and a leading `#` marks a color literal, which the color preview owns.
func standalone(line string, start, end int) bool {
	if start > 0 {
		r, _ := utf8.DecodeLastRuneInString(line[:start])
		if r == '#' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	if end < len(line) {
		r, _ := utf8.DecodeRuneInString(line[end:])
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// allDigits reports whether s carries no hex letter — a plain number, never an
// identifier worth a color.
func allDigits(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool {
		return r < '0' || r > '9'
	}) < 0
}

func overlaps(ranges [][2]int, start, end int) bool {
	for _, r := range ranges {
		if start < r[1] && r[0] < end {
			return true
		}
	}
	return false
}

// sortByStart keeps the merged UUID and hex byte ranges in column order; the
// two passes each produce sorted, non-overlapping output, so merging them is
// one linear pass — an insertion sort over a line full of hex runs behind one
// early UUID would degrade quadratically (#2386).
func sortByStart(ranges [][2]int) {
	if len(ranges) < 2 {
		return
	}
	split := len(ranges)
	for i := 1; i < len(ranges); i++ {
		if ranges[i][0] < ranges[i-1][0] {
			split = i // the first hex range sorting before a UUID range
			break
		}
	}
	if split == len(ranges) {
		return
	}
	merged := make([][2]int, 0, len(ranges))
	a, b := ranges[:split], ranges[split:]
	for len(a) > 0 && len(b) > 0 {
		if a[0][0] <= b[0][0] {
			merged, a = append(merged, a[0]), a[1:]
		} else {
			merged, b = append(merged, b[0]), b[1:]
		}
	}
	merged = append(append(merged, a...), b...)
	copy(ranges, merged)
}

// Slot hashes an identifier onto a rainbow slot. Case folds first, so the same
// SHA in upper and lower case shares one color.
func Slot(id string) int {
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(id)))
	return int(h.Sum32() % uint32(highlight.RainbowColors))
}

// Capture is the theme capture a slot renders with — the shared rainbow
// palette (#789/#1589), so a `theme.captures.rainbow.N` override moves
// identifier colors along with bracket and column colors.
func Capture(slot int) string {
	return "rainbow." + strconv.Itoa(slot%highlight.RainbowColors)
}
