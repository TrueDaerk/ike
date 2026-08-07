// Package numhint turns hard-to-read numeric literals into readable inline
// hints (#1627). Four families, each with its own capture, conceal channel and
// toggle in the editor:
//
//   - Byte sizes — `10485760` draws as `10 MiB`, in binary units, either
//     because the key names a byte count or because the value is a multiple of
//     1024 (the shape nothing but a byte count has).
//   - Durations — `86400000` draws as `24h` when the key names a timeout, a
//     TTL or an interval, with the unit read off the key (`*_ms`, `*_seconds`,
//     … explicitly; the conventional base otherwise).
//   - Digit grouping — a plain integer of five digits or more draws grouped,
//     `1000000` as `1_000_000`.
//   - Radix — a hex literal gains its decimal reading (`0x1F4  = 500`), and a
//     decimal in a permission or flag context gains its octal/hex one
//     (`420  = 0o644`).
//
// All four are display-only and ride the #1585 stand-in mechanic: the raw
// literal reappears under the caret (#1594), so the buffer bytes are always
// one motion away and edits operate on the source.
//
// Which family a literal gets is decided by the field it hangs off first and
// by the value's shape second (#1685): a field name that says the unit wins
// over every pattern, including the epoch reading of the same digits, and the
// user can map field names to units outright.
//
// It is a leaf package — pure Go over internal/lang's span type plus the epoch
// guard of internal/epochtime — so every config-format producer (JSON, YAML,
// TOML, ini, dotenv) can call it without a dependency cycle. The context
// heuristics and the buffer scan live in spans.go, the user mapping in
// units.go; this file is formatting.
package numhint

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// The highlight captures carried by the produced spans, one per family. The
// editor routes each into its own conceal channel, gated by
// editor.byte_size_hints, editor.duration_hints, editor.digit_grouping and
// editor.radix_hints respectively.
const (
	SizeCapture     = "number.size"
	DurationCapture = "number.duration"
	GroupCapture    = "number.group"
	RadixCapture    = "number.radix"
)

// Gap separates a literal from an appended hint inside a stand-in — the same
// two spaces the cron hints use (#1624): wide enough to read as annotation,
// not as buffer text. The size, duration and grouping families replace the
// literal instead of annotating it, so only the radix family uses it.
const Gap = "  "

// GroupSeparator is the digit-group separator, and groupMinDigits the shortest
// run that gets grouped: four-digit numbers are years, ports and HTTP status
// codes far more often than they are quantities worth grouping.
const (
	GroupSeparator  = "_"
	groupMinDigits  = 5
	groupMaxDigits  = 21 // beyond an int64's reach the run is an id, not a count
	sizeStepBytes   = 1 << 10
	radixMaxHexRuns = 8 // longer hex runs are hashes; their decimal says nothing
)

// byteUnits are the binary units, largest first. Byte sizes are written in
// powers of two — that is what makes `10485760` a round number at all — so the
// decimal SI units (kB, MB) are deliberately not offered.
var byteUnits = []struct {
	n    uint64
	name string
}{
	{1 << 50, "PiB"},
	{1 << 40, "TiB"},
	{1 << 30, "GiB"},
	{1 << 20, "MiB"},
	{1 << 10, "KiB"},
}

// FormatBytes renders v as a binary byte size: an exact multiple prints whole
// ("10 MiB"), anything else to one decimal ("1.5 KiB"). It reports false below
// 1 KiB, where the raw number is already the readable form.
func FormatBytes(v uint64) (string, bool) {
	for i, u := range byteUnits {
		if v < u.n {
			continue
		}
		if v%u.n == 0 {
			return fmt.Sprintf("%d %s", v/u.n, u.name), true
		}
		f := float64(v) / float64(u.n)
		if math.Round(f*10)/10 >= float64(sizeStepBytes) && i > 0 {
			// Rounding carried into the next unit: 1048570 is 1023.99 KiB,
			// which must print as 1.0 MiB rather than 1024.0 KiB.
			u = byteUnits[i-1]
			f = float64(v) / float64(u.n)
		}
		return fmt.Sprintf("%.1f %s", f, u.name), true
	}
	return "", false
}

// durUnits are the duration units a hint is rendered in, largest first.
var durUnits = []struct {
	d      time.Duration
	suffix string
}{
	{24 * time.Hour, "d"},
	{time.Hour, "h"},
	{time.Minute, "m"},
	{time.Second, "s"},
	{time.Millisecond, "ms"},
	{time.Microsecond, "us"},
	{time.Nanosecond, "ns"},
}

// dayThreshold is where the day unit starts being used. A single day still
// reads "24h": timeouts and TTLs are compared against each other in hours, and
// "1d" hides that 86400000 and 90000000 are an hour apart.
const dayThreshold = 48 * time.Hour

// FormatDuration renders v units of base as a compact duration — "24h",
// "1h30m", "1s500ms" — using at most two components, the second dropped when
// it is zero. It reports false when the value says nothing the key did not
// already say: a duration whose largest unit is the base unit itself (500 in a
// `*_ms` key is just 500ms), and anything that overflows.
func FormatDuration(v uint64, base time.Duration) (string, bool) {
	if v == 0 || base <= 0 {
		return "", false
	}
	if v > uint64(math.MaxInt64)/uint64(base) {
		return "", false
	}
	d := time.Duration(v) * base
	top := -1
	for i, u := range durUnits {
		if u.suffix == "d" && d < dayThreshold {
			continue
		}
		if d >= u.d {
			top = i
			break
		}
	}
	if top < 0 || durUnits[top].d <= base {
		return "", false
	}
	u := durUnits[top]
	out := fmt.Sprintf("%d%s", d/u.d, u.suffix)
	if rem := d % u.d; rem != 0 && top+1 < len(durUnits) {
		if next := durUnits[top+1]; rem/next.d > 0 {
			out += fmt.Sprintf("%d%s", rem/next.d, next.suffix)
		}
	}
	return out, true
}

// Group inserts the digit-group separator every three digits from the right.
// It reports false for runs too short to be worth grouping, runs long enough
// to be identifiers rather than quantities, and zero-padded runs (an id, a
// version part — never a magnitude).
func Group(digits string) (string, bool) {
	if len(digits) < groupMinDigits || len(digits) > groupMaxDigits {
		return "", false
	}
	if digits[0] == '0' {
		return "", false
	}
	var b strings.Builder
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteString(GroupSeparator)
		}
		b.WriteRune(r)
	}
	return b.String(), true
}

// DecimalOf renders the hex digit run (without its 0x prefix) as decimal. It
// reports false for runs long enough to be a hash rather than a number, and
// for values below 10, where the hex and decimal forms are the same digit.
func DecimalOf(hexDigits string) (string, bool) {
	if len(hexDigits) == 0 || len(hexDigits) > radixMaxHexRuns {
		return "", false
	}
	v, err := strconv.ParseUint(hexDigits, 16, 64)
	if err != nil || v < 10 {
		return "", false
	}
	return strconv.FormatUint(v, 10), true
}

// HexOf renders v as a hex literal, the form a bit mask is read in.
func HexOf(v uint64) string { return "0x" + strings.ToUpper(strconv.FormatUint(v, 16)) }

// OctalOf renders v as an octal literal, the form a file mode is read in.
func OctalOf(v uint64) string { return "0o" + strconv.FormatUint(v, 8) }
