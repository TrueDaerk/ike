package numhint

import (
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"ike/internal/pathglob"
)

// units.go is the user-configurable half of the field context (#1685). The
// built-in heuristics in spans.go read a unit off the key's words, which is a
// guess: `size` is not necessarily a byte count, and a `duration` is counted
// in seconds as often as in milliseconds. The mapping here lets the user say
// it outright — `retention=s`, `size=none`, `created_at=timestamp-s` — and
// what the user says is final:
//
//   - A mapped field is rendered in its unit and nothing else. The value's
//     shape is not consulted, and neither are the built-in key words.
//   - A mapped field claims its columns even when the unit renders nothing
//     for that particular value (a `bytes` field holding 512): no other
//     family may put a stand-in there, so a byte count in the epoch range
//     never draws as a date.
//   - `none` maps a field to no conceal at all, which claims the columns the
//     same way — the way to silence a field the heuristics get wrong.
//
// The mapping is a process-wide global because the span producers are
// lang.Language.Spans hooks with no config plumbing of their own — the same
// arrangement internal/idcolor uses. app pushes editor.number_hint_units into
// it on every config load.

// UnitKind names the reading a mapped field's values are given.
type UnitKind int

const (
	// UnitOff disables every conceal family over the field's values.
	UnitOff UnitKind = iota
	// UnitBytes renders the value as a binary byte size.
	UnitBytes
	// UnitDuration renders the value as a duration counted in Unit.Base.
	UnitDuration
	// UnitTimestampSeconds / UnitTimestampMillis render the value as the UTC
	// form of a Unix timestamp counted in that unit.
	UnitTimestampSeconds
	UnitTimestampMillis
	// UnitOctal / UnitHex append the value's reading in that base.
	UnitOctal
	UnitHex
	// UnitGroup renders the value with its digits grouped.
	UnitGroup
)

// Unit is one field's configured reading: the family plus, for durations, the
// base unit the value counts in.
type Unit struct {
	Kind UnitKind
	Base time.Duration
}

// unitNames are the words a mapping entry's right-hand side may use, beyond
// the duration unit words of unitWords (`ms`, `seconds`, `h`, …), which name
// UnitDuration with that base.
var unitNames = map[string]Unit{
	"none":         {Kind: UnitOff},
	"off":          {Kind: UnitOff},
	"bytes":        {Kind: UnitBytes},
	"byte":         {Kind: UnitBytes},
	"size":         {Kind: UnitBytes},
	"timestamp":    {Kind: UnitTimestampSeconds},
	"timestamp-s":  {Kind: UnitTimestampSeconds},
	"timestamp-ms": {Kind: UnitTimestampMillis},
	"octal":        {Kind: UnitOctal},
	"hex":          {Kind: UnitHex},
	"group":        {Kind: UnitGroup},
}

// ParseUnit reads a mapping entry's unit word. Duration units are the same
// words the key heuristics accept, so `ms`, `msec`, `seconds` and `h` all work
// on the right-hand side too.
func ParseUnit(name string) (Unit, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	if u, ok := unitNames[n]; ok {
		return u, true
	}
	if d, ok := unitWords[n]; ok {
		return Unit{Kind: UnitDuration, Base: d}, true
	}
	return Unit{}, false
}

// rule maps one field-name pattern to a unit.
type rule struct {
	pattern string // lowercase, `*` wildcards
	unit    Unit
}

// rules holds the installed mapping, installed holds the entries it was built
// from so a config reload can tell whether anything actually changed, and
// skipped holds the entries the install dropped (#2008) so the app can report
// them. Atomic pointers: config reloads run on the UI loop while span
// production may read from a render elsewhere.
var (
	rules     atomic.Pointer[[]rule]
	installed atomic.Pointer[string]
	skipped   atomic.Pointer[[]InvalidEntry]
)

// InvalidEntry is one mapping line SetFieldUnits could not use, with the
// reason it was dropped.
type InvalidEntry struct {
	Entry  string
	Reason string
}

// unitVocabulary lists the units a mapping entry may name, in the order the
// setting's description lists them — the message a rejected entry is worded
// with has to say what would have worked.
var unitVocabulary = []string{
	"bytes", "ns", "us", "ms", "s", "min", "h", "d",
	"timestamp-s", "timestamp-ms", "octal", "hex", "group", "none",
}

// UnitVocabulary is the list of unit words a mapping entry's right-hand side
// may use — one spelling per reading, the ones UnitName writes.
func UnitVocabulary() []string { return append([]string(nil), unitVocabulary...) }

// EntryError explains why entry cannot serve as a `pattern=unit` mapping line,
// or returns "" when it can. A blank line is not an error — an empty list
// element is nothing, not a typo. It is shared between SetFieldUnits, which
// skips the entry and lets the app report it, and the Settings form, which
// rejects the input outright with the same message (#2008): an entry silently
// dropped is exactly how a field keeps rendering in the built-in base while
// the user believes the mapping is in force.
func EntryError(entry string) string {
	if strings.TrimSpace(entry) == "" {
		return ""
	}
	name, unit, ok := strings.Cut(entry, "=")
	if !ok {
		return `not a "pattern=unit" entry — write the field name, "=", then the unit`
	}
	if strings.TrimSpace(name) == "" {
		return `the field pattern before "=" is empty`
	}
	if _, ok := ParseUnit(unit); !ok {
		return "unknown unit " + strconv.Quote(strings.TrimSpace(unit)) +
			" — use one of: " + strings.Join(unitVocabulary, ", ")
	}
	return ""
}

// InvalidEntries returns the entries the installed mapping had to skip, each
// with its reason (#2008). The app turns them into config diagnostics the same
// way it reports an unknown language id in a file association: a rule that
// gates nothing must be visible, not silently inert.
func InvalidEntries() []InvalidEntry {
	if s := skipped.Load(); s != nil {
		return append([]InvalidEntry(nil), *s...)
	}
	return nil
}

// SetFieldUnits installs the user's field-name mapping, each entry written
// `pattern=unit` (`*_bytes=bytes`, `retention=s`, `session_id=none`).
// Malformed entries and unknown units are skipped rather than failing the
// whole mapping — a typo in one line must not silence the rest. Earlier
// entries win, so a specific name can precede a wildcard covering it.
//
// It reports whether the mapping changed, which is what tells the app a config
// reload has to re-parse the open editors: the spans are cached until then.
func SetFieldUnits(entries []string) bool {
	joined := strings.Join(entries, "\n")
	if prev := installed.Load(); prev != nil && *prev == joined {
		return false
	}
	installed.Store(&joined)
	var out []rule
	var bad []InvalidEntry
	for _, e := range entries {
		if msg := EntryError(e); msg != "" {
			bad = append(bad, InvalidEntry{Entry: e, Reason: msg})
			continue
		}
		name, unit, ok := strings.Cut(e, "=")
		if !ok {
			continue // a blank entry: nothing to map, nothing to report
		}
		name = strings.ToLower(strings.TrimSpace(name))
		u, _ := ParseUnit(unit)
		out = append(out, rule{pattern: name, unit: u})
	}
	rules.Store(&out)
	skipped.Store(&bad)
	return true
}

// FieldUnit returns the unit a field name is mapped to. Matching is
// case-insensitive over the whole name, with `*` matching any run of
// characters; a camel-case name is additionally matched in its snake_case
// form, so `max_body_size` covers `maxBodySize` too.
func FieldUnit(key string) (Unit, bool) {
	_, u, ok := FieldRule(key)
	return u, ok
}

// FieldRule is FieldUnit with the pattern of the entry that matched (#1998):
// the explain popover names the rule that decided a value's reading, and with
// a wildcard entry the pattern is the only way to point at it in the settings.
func FieldRule(key string) (pattern string, u Unit, ok bool) {
	rs := rules.Load()
	if rs == nil || len(*rs) == 0 || key == "" {
		return "", Unit{}, false
	}
	lower := strings.ToLower(key)
	snake := strings.Join(keyWords(key), "_")
	for _, r := range *rs {
		if globMatch(r.pattern, lower) || (snake != lower && globMatch(r.pattern, snake)) {
			return r.pattern, r.unit, true
		}
	}
	return "", Unit{}, false
}

// UnitName is the mapping word for a unit — the right-hand side an entry in
// editor.number_hint_units is written with. It is the inverse of ParseUnit for
// every unit ParseUnit produces, so a reading the heuristics chose can be
// pinned as a rule verbatim (#1998).
func UnitName(u Unit) string {
	switch u.Kind {
	case UnitOff:
		return "none"
	case UnitBytes:
		return "bytes"
	case UnitTimestampSeconds:
		return "timestamp-s"
	case UnitTimestampMillis:
		return "timestamp-ms"
	case UnitOctal:
		return "octal"
	case UnitHex:
		return "hex"
	case UnitGroup:
		return "group"
	case UnitDuration:
		for _, name := range durationUnitNames {
			if unitWords[name] == u.Base {
				return name
			}
		}
	}
	return ""
}

// durationUnitNames are the duration words UnitName prefers, one per base —
// unitWords holds several spellings of each ("ms", "msec", "millis") and the
// shortest is the one a written rule reads best with.
var durationUnitNames = []string{"ns", "us", "ms", "s", "min", "h", "d"}

// Label renders a unit the way the explain popover names a reading: the
// mapping word, with the duration base spelled out.
func (u Unit) Label() string {
	switch u.Kind {
	case UnitOff:
		return "none (no stand-in)"
	case UnitBytes:
		return "binary byte size"
	case UnitDuration:
		return "duration in " + UnitName(u)
	case UnitTimestampSeconds:
		return "Unix timestamp in seconds"
	case UnitTimestampMillis:
		return "Unix timestamp in milliseconds"
	case UnitOctal:
		return "octal reading"
	case UnitHex:
		return "hex reading"
	case UnitGroup:
		return "grouped digits"
	}
	return ""
}

// globMatch delegates to pathglob.StarMatch, shared with the secret-pattern
// key matcher (internal/secret).
func globMatch(pattern, s string) bool { return pathglob.StarMatch(pattern, s) }
