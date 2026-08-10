// variable.go locates the parts of a log line that move between two otherwise
// identical repeats (#1758). #1650 folds a run of consecutive equal lines into
// one row with a `×N` badge, and #1621's timestamps were the only moving part
// it knew about — so a service that also prints an elapsed time, a page number
// or a counter kept filling the viewport with hundreds of near-identical rows.
//
// Two sources feed the ranges: the *value* of a logfmt key whose name says the
// value counts up (`elapsed=`, `page=`, `offset=`), and free-standing patterns
// in the message text (`340ms`, `page 17 of 240`, `1500 rows`, `42%`).
//
// The rule everywhere is to stay conservative: only a key or a shape that is
// recognizably "running" is blanked. `status=500` vs `status=200`, two ports,
// two ids and two HTTP paths are semantically different lines and must keep
// different keys, so a bare number is never blanked on its own — it needs a
// duration unit, a counter noun, a pagination keyword or a ratio around it.
//
// Ranges are rune columns, like everything else in this package; RepeatKey
// merges and splices them.
package logline

import "regexp"

// durationKeys are the logfmt keys whose value is an elapsed time. Their value
// still has to *look* like a duration (durationValue) — `took=forever` is
// prose, not a moving number.
var durationKeys = map[string]bool{
	"elapsed": true, "elapsed_ms": true, "elapsed_time": true, "elapsedms": true,
	"duration": true, "duration_ms": true, "duration_s": true, "durationms": true,
	"took": true, "took_ms": true, "latency": true, "latency_ms": true,
	"dt": true, "rtt": true, "runtime_ms": true, "time_ms": true, "ms": true,
}

// counterKeys are the logfmt keys whose value is a page, an offset or a plain
// running counter. Deliberately narrow: `status`, `port`, `id`, `code` and
// friends are *not* here, because two lines differing in those differ in what
// they say.
var counterKeys = map[string]bool{
	"page": true, "pages": true, "offset": true, "cursor": true,
	"attempt": true, "attempts": true, "retry": true, "retries": true,
	"count": true, "rows": true, "progress": true, "percent": true, "pct": true,
	"seq": true, "index": true, "idx": true, "batch": true, "chunk": true,
	"processed": true, "remaining": true, "iteration": true, "step": true,
}

var (
	// goDurationRe is a Go-style duration token: one or more number+unit
	// groups ("340ms", "1.2s", "2m30s", "500µs").
	goDurationRe = regexp.MustCompile(
		`^(?:\d+(?:\.\d+)?(?:ns|us|µs|μs|ms|s|m|h))+$`)
	// clockDurationRe is an "HH:MM:SS" duration ("00:00:12", "1:02:03.5").
	clockDurationRe = regexp.MustCompile(`^\d{1,3}:\d{2}:\d{2}(?:[.,]\d+)?$`)
	// numberRe is a plain number, thousands separators, a decimal fraction and
	// a percent sign allowed ("3", "1_500", "3,400", "12.5", "42%").
	numberRe = regexp.MustCompile(`^[-+]?\d[\d_,]*(?:\.\d+)?%?$`)
	// ratioRe is a progress ratio value ("17/240").
	ratioRe = regexp.MustCompile(`^\d+/\d+$`)
	// unitNumberRe is a number carrying a size/duration unit suffix
	// ("1500ms", "20kb") — accepted for duration keys only.
	unitNumberRe = regexp.MustCompile(`^[-+]?\d+(?:\.\d+)?\s*[A-Za-zµμ]{1,3}$`)
)

// variableRanges collects every moving range of one line: the timestamps
// #1621 recognizes (timestampRanges), the values of duration and counter
// logfmt keys, and the free-standing message patterns. The result is sorted
// and merged, so RepeatKey can splice it out in one pass.
func variableRanges(line string) []Range {
	rs := timestampRanges(line)
	runes := []rune(line)
	for _, pr := range ScanPairs(line) {
		if pairMoves(pr, pairKey(runes, pr)) {
			rs = append(rs, pr.Value)
		}
	}
	rs = append(rs, patternRanges(line)...)
	return mergeRanges(rs)
}

// pairKey returns a pair's key, lowercased and without the trailing "=" its
// range covers (the span layer dims key and separator together).
func pairKey(runes []rune, pr Pair) string {
	s, e := pr.Key.Start, pr.Key.End-1
	if s < 0 || e <= s || e > len(runes) {
		return ""
	}
	return lower(string(runes[s:e]))
}

// pairMoves reports whether a logfmt pair's value is a moving part: a
// duration-shaped value under a duration key, or a number under a
// pagination/counter key. Timestamps are not handled here — timestampRanges
// already covers every KindTime value. A cursor is the one key whose value
// need not be numeric: an opaque pagination token is a moving part by
// definition.
func pairMoves(pr Pair, key string) bool {
	if pr.Value.Empty() {
		return false
	}
	switch {
	case durationKeys[key]:
		return durationValue(pr.Text)
	case key == "cursor":
		return true
	case counterKeys[key]:
		return numberRe.MatchString(pr.Text) || ratioRe.MatchString(pr.Text)
	}
	return false
}

// durationValue reports whether a value reads as an elapsed time: a Go
// duration, an "HH:MM:SS" span, a plain number (a `_ms` key carries its unit in
// the key) or a number with a short unit suffix.
func durationValue(v string) bool {
	return goDurationRe.MatchString(v) || clockDurationRe.MatchString(v) ||
		numberRe.MatchString(v) || unitNumberRe.MatchString(v)
}

// The word sets the message scan keys on. counterWords precede their number
// ("page 17", "attempt #3"), countedNouns follow it ("1500 rows"), and
// durationWords are the spelled-out units ("3 seconds").
var (
	counterWords = map[string]bool{
		"page": true, "pages": true, "offset": true, "offsets": true,
		"cursor": true, "attempt": true, "attempts": true,
		"retry": true, "retries": true, "batch": true, "batches": true,
		"chunk": true, "chunks": true, "step": true, "steps": true,
		"iteration": true, "iterations": true, "iter": true,
		"round": true, "rounds": true, "seq": true, "index": true,
		"item": true, "items": true, "part": true, "parts": true,
		"progress": true, "count": true, "counts": true,
		"processed": true, "remaining": true, "total": true,
	}
	countedNouns = map[string]bool{
		"row": true, "rows": true, "record": true, "records": true,
		"item": true, "items": true, "file": true, "files": true,
		"byte": true, "bytes": true, "entry": true, "entries": true,
		"message": true, "messages": true, "event": true, "events": true,
		"line": true, "lines": true, "doc": true, "docs": true,
		"document": true, "documents": true, "object": true, "objects": true,
		"key": true, "keys": true, "request": true, "requests": true,
		"user": true, "users": true, "task": true, "tasks": true,
		"job": true, "jobs": true, "page": true, "pages": true,
	}
	durationWords = map[string]bool{
		"ms": true, "millis": true, "millisecond": true, "milliseconds": true,
		"sec": true, "secs": true, "second": true, "seconds": true,
		"min": true, "mins": true, "minute": true, "minutes": true,
		"hr": true, "hrs": true, "hour": true, "hours": true,
		"day": true, "days": true,
	}
)

// patternRanges finds the moving parts that sit in the message text rather
// than in a logfmt value: durations, ratios, percentages, and the numbers
// around a pagination keyword or a counting noun.
//
// The scan is hand-written rather than a regex sweep on purpose. RepeatKey
// runs over every line of a log buffer on every document version, and an
// alternation wide enough to cover these shapes costs tens of microseconds per
// line — seconds on a large file. Anchoring on digit runs instead touches a
// handful of positions per line.
func patternRanges(line string) []Range {
	runes := []rune(line)
	var out []Range
	for i := 0; i < len(runes); i++ {
		if !isDigit(runes[i]) || !numberStart(runes, i) {
			continue
		}
		rs, end := movingAt(runes, i)
		out = append(out, rs...)
		if end > i {
			i = end - 1 // the loop's i++ resumes right after the match
		}
	}
	return out
}

// numberStart reports whether a digit at i may open a moving part. A word
// boundary is not enough: the digit must also not hang off a dot or a slash,
// which is what separates the "3s" of "1.2.3s" and the "240" of "/api/v1/240"
// from a real duration or ratio.
func numberStart(runes []rune, i int) bool {
	if i == 0 {
		return true
	}
	switch r := runes[i-1]; {
	case isDigit(r), isLetter(r), r == '_', r == '.', r == '/':
		return false
	}
	return true
}

// movingAt classifies the number starting at i and returns the ranges to blank
// plus the position to continue the scan from. An "N of M" pair yields two
// ranges — the "of" between them is fixed text and stays. end is i when the
// number is not a moving part.
func movingAt(runes []rune, i int) ([]Range, int) {
	// A duration token carries its unit, so the unit blanks with it: a run
	// whose elapsed time moves from "980ms" to "1.2s" is still one run.
	if e, ok := goDurationAt(runes, i); ok && tokenEnds(runes, e) {
		return []Range{{i, e}}, e
	}
	if e, ok := clockAt(runes, i); ok && tokenEnds(runes, e) {
		return []Range{{i, e}}, e
	}
	n := numberEnd(runes, i)
	if e, ok := ratioAt(runes, n); ok && tokenEnds(runes, e) {
		return []Range{{i, e}}, e
	}
	if e, ok := percentAt(runes, n); ok && tokenEnds(runes, e) {
		return []Range{{i, e}}, e
	}
	word, we := wordAfter(runes, n)
	switch {
	case durationWords[word]:
		// Spelled out, the unit blanks along with the number too — otherwise
		// "1 second" and "3 seconds" would differ in their plural alone.
		return []Range{{i, we}}, we
	case word == "of":
		if s, e, ok := numberAfter(runes, we); ok {
			return []Range{{i, n}, {s, e}}, e
		}
	case countedNouns[word] && tokenEnds(runes, n):
		// Only the count blanks here: the noun says *what* was counted, and
		// "12 files" must not collide with "12 rows".
		return []Range{{i, n}}, n
	}
	if counterWords[wordBefore(runes, i)] && tokenEnds(runes, n) {
		return []Range{{i, n}}, n
	}
	return nil, i
}

// tokenEnds reports whether a match ending at e ends a token: anything but a
// letter, a digit or an underscore may follow. It keeps "1.2sx" and "5mb" from
// passing as durations.
func tokenEnds(runes []rune, e int) bool {
	if e >= len(runes) {
		return true
	}
	r := runes[e]
	return !isDigit(r) && !isLetter(r) && r != '_'
}

// numberEnd consumes a plain number at i: digits, thousands separators between
// digits ("1,500", "1_500") and at most one decimal fraction.
func numberEnd(runes []rune, i int) int {
	for i < len(runes) && isDigit(runes[i]) {
		i++
		if i+1 < len(runes) && (runes[i] == ',' || runes[i] == '_') && isDigit(runes[i+1]) {
			i++
		}
	}
	if i+1 < len(runes) && runes[i] == '.' && isDigit(runes[i+1]) {
		i++
		for i < len(runes) && isDigit(runes[i]) {
			i++
		}
	}
	return i
}

// goDurationAt matches a Go-style duration token at i — one or more
// number+unit groups, "340ms", "1.2s", "2m30s", "1h2m3.5s".
func goDurationAt(runes []rune, i int) (int, bool) {
	groups := 0
	for i < len(runes) && isDigit(runes[i]) {
		n := plainNumberEnd(runes, i)
		u, ok := durationUnit(runes, n)
		if !ok {
			break
		}
		i, groups = u, groups+1
	}
	return i, groups > 0
}

// plainNumberEnd consumes digits and at most one decimal fraction — a duration
// never carries thousands separators.
func plainNumberEnd(runes []rune, i int) int {
	for i < len(runes) && isDigit(runes[i]) {
		i++
	}
	if i+1 < len(runes) && runes[i] == '.' && isDigit(runes[i+1]) {
		i++
		for i < len(runes) && isDigit(runes[i]) {
			i++
		}
	}
	return i
}

// durationUnit consumes a duration unit at i. The two-rune units are tried
// first, so "2m30s" reads as minutes plus seconds while "340ms" reads as
// milliseconds.
func durationUnit(runes []rune, i int) (int, bool) {
	if i >= len(runes) {
		return i, false
	}
	if i+1 < len(runes) {
		switch pair := [2]rune{toLower(runes[i]), toLower(runes[i+1])}; pair {
		case [2]rune{'n', 's'}, [2]rune{'u', 's'}, [2]rune{'µ', 's'}, [2]rune{'μ', 's'}, [2]rune{'m', 's'}:
			return i + 2, true
		}
	}
	switch toLower(runes[i]) {
	case 's', 'm', 'h':
		return i + 1, true
	}
	return i, false
}

// clockAt matches an "HH:MM:SS" duration at i ("00:00:12", "1:02:03.5").
func clockAt(runes []rune, i int) (int, bool) {
	j := i
	for j < len(runes) && isDigit(runes[j]) {
		j++
	}
	if j-i < 1 || j-i > 3 {
		return i, false
	}
	for f := 0; f < 2; f++ {
		if j >= len(runes) || runes[j] != ':' {
			return i, false
		}
		if j+2 >= len(runes) || !isDigit(runes[j+1]) || !isDigit(runes[j+2]) {
			return i, false
		}
		j += 3
	}
	if j+1 < len(runes) && (runes[j] == '.' || runes[j] == ',') && isDigit(runes[j+1]) {
		j++
		for j < len(runes) && isDigit(runes[j]) {
			j++
		}
	}
	return j, true
}

// ratioAt matches the "/M" tail of a progress ratio at n, the position right
// after the first number. A second slash means the text is a date or a path,
// not a ratio.
func ratioAt(runes []rune, n int) (int, bool) {
	if n >= len(runes) || runes[n] != '/' || n+1 >= len(runes) || !isDigit(runes[n+1]) {
		return n, false
	}
	e := n + 1
	for e < len(runes) && isDigit(runes[e]) {
		e++
	}
	if e < len(runes) && runes[e] == '/' {
		return n, false
	}
	return e, true
}

// percentAt matches a percent sign after the number at n, one optional space
// allowed ("42%", "99.5 %").
func percentAt(runes []rune, n int) (int, bool) {
	e := n
	if e < len(runes) && runes[e] == ' ' {
		e++
	}
	if e < len(runes) && runes[e] == '%' {
		return e + 1, true
	}
	return n, false
}

// wordAfter returns the lowercased word following the number at n, separated
// by blanks, together with its end. An empty word means none follows.
func wordAfter(runes []rune, n int) (string, int) {
	i := n
	for i < len(runes) && (runes[i] == ' ' || runes[i] == '\t') {
		i++
	}
	if i == n || i >= len(runes) || !isLetter(runes[i]) {
		return "", n
	}
	s := i
	for i < len(runes) && isLetter(runes[i]) {
		i++
	}
	return lower(string(runes[s:i])), i
}

// numberAfter returns the range of a blank-separated number following we — the
// "M" of an "N of M" pair. ok is false when none follows.
func numberAfter(runes []rune, we int) (int, int, bool) {
	i := we
	for i < len(runes) && (runes[i] == ' ' || runes[i] == '\t') {
		i++
	}
	if i == we || i >= len(runes) || !isDigit(runes[i]) {
		return 0, 0, false
	}
	e := numberEnd(runes, i)
	if !tokenEnds(runes, e) {
		return 0, 0, false
	}
	return i, e, true
}

// wordBefore returns the lowercased word in front of the number at i, skipping
// blanks and one optional ":"/"#"/"=" separator — the "page" of "page 17", the
// "attempt" of "attempt #3".
func wordBefore(runes []rune, i int) string {
	j := i
	for j > 0 && (runes[j-1] == ' ' || runes[j-1] == '\t') {
		j--
	}
	if j > 0 && (runes[j-1] == ':' || runes[j-1] == '#' || runes[j-1] == '=') {
		j--
		for j > 0 && (runes[j-1] == ' ' || runes[j-1] == '\t') {
			j--
		}
	}
	e := j
	for j > 0 && isLetter(runes[j-1]) {
		j--
	}
	if j == e {
		return ""
	}
	return lower(string(runes[j:e]))
}

func isDigit(r rune) bool  { return r >= '0' && r <= '9' }
func isLetter(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }

func toLower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}
