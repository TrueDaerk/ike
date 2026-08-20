// Package concealexplain answers the one question the conceal families never
// answered for themselves (#1998): why does *this* value draw the way it does?
//
// Conceal and secret masking rest on heuristics — a field name mapped to a
// unit (#1685), a built-in key word, the shape of the digits, a secret key
// pattern (#1712) — and a heuristic that misfires is invisible from the
// buffer: the user sees `1970-01-20 12:23:12Z` where a byte count should be
// and has nowhere to look. This package resolves a concealed (or a plainly
// rendered) value back to the rule that decided it, in the words of that rule,
// and names the settings entry that would overrule it.
//
// It is a composition layer, not a second copy of the heuristics: the
// provenance comes from the hint sources themselves — numhint.HintAt carries
// the Why the producers filled in, secret.Explain replays the key tables in
// order, epochtime.Unit reports the digit-count reading, consthint.Eval
// re-evaluates a constant's right-hand side. What is added here is the mapping
// from a span on screen to the question those functions answer, plus the
// wording.
//
// The package is a leaf over the hint sources, so the editor popover and its
// tests share exactly one explanation.
package concealexplain

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"ike/internal/concealfilter"
	"ike/internal/consthint"
	"ike/internal/epochtime"
	"ike/internal/numhint"
	"ike/internal/secret"
)

// The settings a rule made in the popover is written to. They are the existing
// stores (#1685, #1712) rather than a parallel one, so a rule created here is
// listed and editable in the Settings UI like a hand-written entry.
const (
	UnitsSetting  = "editor.number_hint_units"
	SecretSetting = "editor.secret_masking_keys"
)

// Kind separates the two things a span can be: a number read in some unit, or
// a value hidden behind a mask.
type Kind int

const (
	// KindNumber is a numeric literal with a reading — byte size, duration,
	// timestamp, radix, digit grouping — or one that has none.
	KindNumber Kind = iota
	// KindSecret is a value the secret key tables have an opinion about,
	// masked or deliberately not.
	KindSecret
)

// Source names the rule level that decided the value.
type Source int

const (
	// SourceNone marks a value no rule matched.
	SourceNone Source = iota
	// SourceFieldRule marks a reading a configured editor.number_hint_units
	// entry decided (#1685).
	SourceFieldRule
	// SourceKeyWord marks a reading a built-in field-name word decided.
	SourceKeyWord
	// SourceShape marks a reading the value's own shape decided.
	SourceShape
	// SourceEpochRange marks a digit run the epoch guard decoded (#1618):
	// nothing about the field name is involved, only the run's length and
	// range.
	SourceEpochRange
	// SourceConstEval marks a computed constant right-hand side (#1701): the
	// expression was evaluated, and the result read in the name's unit.
	SourceConstEval
	// SourceSecretPattern / SourceSecretExempt mark a configured
	// editor.secret_masking_keys entry (#1712), positive or exempting.
	SourceSecretPattern
	SourceSecretExempt
	// SourceSecretBuiltin marks a built-in secret key word, SourceSecretPublic
	// a built-in public marker that cleared an otherwise-suspect key.
	SourceSecretBuiltin
	SourceSecretPublic
)

// Explanation is one value's provenance, in the terms the popover shows it.
type Explanation struct {
	Kind    Kind
	Source  Source
	Key     string // the field or constant name the value hangs off
	Raw     string // the value as the buffer holds it
	Display string // what draws instead ("" when the value draws as written)
	Family  string // the concealfilter family that gates the stand-in
	Rule    string // the rule that fired, in its own words
	Reading string // the interpretation the rule chose
	Unit    string // the number_hint_units word for Reading ("" when none)
	Masked  bool   // KindSecret: whether the value renders masked
	Start   int    // rune columns of the explained value on the line
	End     int
}

// Request is what the caller knows about the value: the line it sits on, the
// concealed span's columns when there is one (End above Start), the caret
// column otherwise, the span's capture and stand-in, and the buffer's language
// id for the constant evaluator.
type Request struct {
	Line    string
	Col     int
	Start   int
	End     int
	Capture string
	Display string
	Lang    string
}

// Explain resolves the value the request points at. It reports false only when
// there is no value there at all — an empty line, a caret in whitespace; a
// value nothing matched still explains, as "no rule matched", which is the
// answer to the other half of the question (#1930: why is this *not* masked).
func Explain(req Request) (Explanation, bool) {
	runes := []rune(req.Line)
	start, end := req.Start, req.End
	var raw, key string
	if end > start && start >= 0 && end <= len(runes) {
		raw = string(runes[start:end])
		if v, ok := numhint.ValueAt(req.Line, start); ok {
			key = v.Key
		}
		if key == "" {
			key, _, _, _, _ = assignment(req.Line, start)
		}
	} else if v, ok := numhint.ValueAt(req.Line, req.Col); ok {
		raw, key, start, end = v.Text, v.Key, v.Start, v.End
	} else if k, text, vs, ve, ok := assignment(req.Line, req.Col); ok {
		// The value is not a token the number scan reports — a URL, a quoted
		// sentence, a base64 blob. The `key = value` shape still says what it
		// hangs off, which is all the secret tables need.
		key, raw, start, end = k, text, vs, ve
	} else {
		return Explanation{}, false
	}
	ex := Explanation{Key: key, Raw: raw, Display: req.Display, Start: start, End: end}
	if req.Capture == secret.Capture || (req.Capture == "" && secretValue(raw, key)) {
		return explainSecret(ex), true
	}
	return explainNumber(ex, req), true
}

// explainSecret reports what the key tables say about the value's key, in the
// order Suspect walks them: the user's patterns first, then the strong words,
// the public markers, the marker words, the suffixes and the exact names.
func explainSecret(ex Explanation) Explanation {
	ex.Kind = KindSecret
	ex.Family = concealfilter.SecretMasking
	if ex.Key == "" {
		ex.Rule = "no key names this value, so secret masking never looks at it"
		ex.Reading = "not masked"
		return ex
	}
	v := secret.Explain(ex.Key)
	ex.Masked = v.Mask
	ex.Reading = "not masked"
	if v.Mask {
		ex.Reading = "masked with " + secret.Mask
	}
	switch v.Reason {
	case secret.ReasonUserPattern:
		ex.Source = SourceSecretPattern
		ex.Rule = fmt.Sprintf("your secret pattern %q matches the key %q", v.Pattern, ex.Key)
	case secret.ReasonUserExempt:
		ex.Source = SourceSecretExempt
		ex.Rule = fmt.Sprintf("your exempting secret pattern %q matches the key %q", "-"+v.Pattern, ex.Key)
	case secret.ReasonStrong:
		ex.Source = SourceSecretBuiltin
		ex.Rule = fmt.Sprintf("the built-in secret word %q is part of the key %q", v.Pattern, ex.Key)
	case secret.ReasonMarker:
		ex.Source = SourceSecretBuiltin
		ex.Rule = fmt.Sprintf("the built-in secret marker %q is part of the key %q", v.Pattern, ex.Key)
	case secret.ReasonSuffix:
		ex.Source = SourceSecretBuiltin
		ex.Rule = fmt.Sprintf("the key %q ends in the built-in secret suffix %q", ex.Key, v.Pattern)
	case secret.ReasonExact:
		ex.Source = SourceSecretBuiltin
		ex.Rule = fmt.Sprintf("the key %q is a built-in secret name on its own", ex.Key)
	case secret.ReasonPublicMarker:
		ex.Source = SourceSecretPublic
		ex.Rule = fmt.Sprintf("the built-in public marker %q clears the key %q", v.Pattern, ex.Key)
	default:
		ex.Source = SourceNone
		ex.Rule = fmt.Sprintf("no secret pattern matches the key %q", ex.Key)
	}
	return ex
}

// explainNumber reports which of the number families claimed the literal and
// why: the hint producers' own provenance first, the epoch guard second, the
// constant evaluator third.
func explainNumber(ex Explanation, req Request) Explanation {
	ex.Kind = KindNumber
	ex.Family = familyOf(req.Capture)
	if h, ok := numhint.HintAt(0, req.Line, ex.Start); ok && h.Span.StartCol == ex.Start {
		return fromWhy(ex, h.Why)
	}
	if unit, ok := epochtime.Unit(ex.Raw); ok {
		ex.Source = SourceEpochRange
		ex.Unit = "timestamp-s"
		ex.Reading = "Unix timestamp in seconds"
		if unit == time.Millisecond {
			ex.Unit, ex.Reading = "timestamp-ms", "Unix timestamp in milliseconds"
		}
		ex.Rule = fmt.Sprintf("the %d-digit run falls in the 2001–2100 epoch range, the shape of a Unix timestamp in %s",
			len(ex.Raw), unitPlural(unit))
		if ex.Family == "" {
			ex.Family = concealfilter.TimestampDecoding
		}
		return ex
	}
	if f, ok := consthint.FlavorForLang(req.Lang); ok {
		if res, ok := consthint.Eval(ex.Raw, f); ok && !res.Single {
			return fromConstEval(ex, res.Value)
		}
	}
	ex.Source = SourceNone
	ex.Reading = "plain number"
	if ex.Key == "" {
		ex.Rule = "no rule matches this value, and no field name names it"
		return ex
	}
	ex.Rule = fmt.Sprintf("no rule matches the field %q or the value's shape", ex.Key)
	return ex
}

// fromWhy turns a producer's provenance into its wording.
func fromWhy(ex Explanation, why numhint.Why) Explanation {
	ex.Unit = numhint.UnitName(why.Unit)
	ex.Reading = why.Unit.Label()
	if ex.Key == "" {
		ex.Key = why.Key
	}
	switch why.Source {
	case numhint.SourceFieldRule:
		ex.Source = SourceFieldRule
		ex.Rule = fmt.Sprintf("your field rule %q maps the field %q to %s",
			why.Pattern+"="+ex.Unit, why.Key, ex.Unit)
	case numhint.SourceKeyWord:
		ex.Source = SourceKeyWord
		ex.Rule = fmt.Sprintf("the built-in key word %q in the field %q names %s",
			why.Pattern, why.Key, ex.Reading)
	case numhint.SourceShape:
		ex.Source = SourceShape
		ex.Rule = why.Shape
	default:
		ex.Source = SourceNone
		ex.Rule = "no rule matches this value"
	}
	if ex.Display == "" && why.Unit.Kind == numhint.UnitOff {
		ex.Reading = "no stand-in"
	}
	return ex
}

// fromConstEval words a computed constant right-hand side: the expression was
// evaluated first, and the result then read in the name's unit exactly like a
// config value (#1701).
func fromConstEval(ex Explanation, value uint64) Explanation {
	ex.Source = SourceConstEval
	dec := strconv.FormatUint(value, 10)
	origin := "the value's shape"
	unit, reading := "", "plain number"
	if pattern, u, ok := numhint.FieldRule(ex.Key); ok {
		origin = fmt.Sprintf("your field rule %q", pattern+"="+numhint.UnitName(u))
		unit, reading = numhint.UnitName(u), u.Label()
	} else if word, u, ok := numhint.KeyWord(ex.Key); ok {
		origin = fmt.Sprintf("the built-in key word %q", word)
		unit, reading = numhint.UnitName(u), u.Label()
	} else if value >= 1024 && value%1024 == 0 {
		unit, reading = "bytes", numhint.Unit{Kind: numhint.UnitBytes}.Label()
	} else if _, ok := numhint.Group(dec); ok {
		unit, reading = "group", numhint.Unit{Kind: numhint.UnitGroup}.Label()
	}
	ex.Unit, ex.Reading = unit, reading
	ex.Rule = fmt.Sprintf("the constant expression %s evaluates to %s, read as %s from %s",
		ex.Raw, dec, reading, origin)
	return ex
}

// familyOf maps a stand-in capture onto the conceal family gating it, so the
// popover can name the toggle that would switch the whole family off.
func familyOf(capture string) string {
	switch capture {
	case numhint.SizeCapture:
		return concealfilter.ByteSizeHints
	case numhint.DurationCapture:
		return concealfilter.DurationHints
	case numhint.GroupCapture:
		return concealfilter.DigitGrouping
	case numhint.RadixCapture:
		return concealfilter.RadixHints
	case epochtime.Capture:
		return concealfilter.TimestampDecoding
	case secret.Capture:
		return concealfilter.SecretMasking
	}
	return ""
}

// assignment reads the `key = value` shape off a line: the name before the
// first separator and the trimmed rest as the value, reported when col sits at
// or after the separator. It is the fallback for values the number scan does
// not tokenise as one — a URL, a quoted sentence — where the key is still the
// thing that decides masking.
func assignment(line string, col int) (key, value string, start, end int, ok bool) {
	runes := []rune(line)
	sep := -1
	for i, r := range runes {
		if r == '=' || r == ':' {
			sep = i
			break
		}
	}
	if sep < 0 || col < sep {
		return "", "", 0, 0, false
	}
	start = sep + 1
	for start < len(runes) && isBlank(runes[start]) {
		start++
	}
	end = len(runes)
	for end > start && isBlank(runes[end-1]) {
		end--
	}
	if end <= start {
		return "", "", 0, 0, false
	}
	key = strings.Trim(strings.TrimSpace(string(runes[:sep])), `"'`)
	return key, string(runes[start:end]), start, end, true
}

func isBlank(r rune) bool { return r == ' ' || r == '\t' }

// secretValue reports whether an unconcealed value is one the secret tables
// answer for rather than the number families: anything that is not a numeric
// literal, plus a numeric value under a key the tables would mask — a
// credential whose masking is switched off is exactly the case the popover has
// to explain (#1930).
func secretValue(raw, key string) bool {
	return !numeric(raw) || secret.Explain(key).Mask
}

// numeric reports whether text is a literal the number families could read —
// a plain decimal run or a prefixed one. Everything else is a value only the
// secret tables have an opinion about.
func numeric(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if r < '0' || r > '9' {
			return prefixed(text)
		}
	}
	return true
}

// prefixed reports whether text is a 0x/0o/0b literal.
func prefixed(text string) bool {
	if len(text) < 3 || text[0] != '0' {
		return false
	}
	switch text[1] {
	case 'x', 'X', 'o', 'O', 'b', 'B':
		return true
	}
	return false
}

// unitPlural names a duration base in the plural, for the epoch wording.
func unitPlural(d time.Duration) string {
	if d == time.Millisecond {
		return "milliseconds"
	}
	return "seconds"
}

// Choice is one reclassification the popover offers: a one-key shortcut, the
// editor.number_hint_units word it writes and the reading it names.
type Choice struct {
	Key   rune
	Unit  string
	Label string
}

// choices are the readings a number can be reclassified to, in the order the
// popover lists them. Durations are offered in the two bases config files
// actually count in; a field counted in anything else is one entry away by
// hand in the Settings UI, which the popover writes to.
var choices = []Choice{
	{'1', "bytes", "byte size"},
	{'2', "ms", "duration (milliseconds)"},
	{'3', "s", "duration (seconds)"},
	{'4', "timestamp-s", "epoch timestamp (seconds)"},
	{'5', "timestamp-ms", "epoch timestamp (milliseconds)"},
	{'6', "octal", "octal reading"},
	{'7', "hex", "hex reading"},
	{'8', "group", "grouped digits"},
	{'9', "none", "plain number (no stand-in)"},
}

// Choices lists the reclassification targets.
func Choices() []Choice { return append([]Choice(nil), choices...) }

// ChoiceFor returns the choice a key press selects.
func ChoiceFor(key rune) (Choice, bool) {
	for _, c := range choices {
		if c.Key == key {
			return c, true
		}
	}
	return Choice{}, false
}

// UnitEntry renders the editor.number_hint_units entry pinning key to unit.
// The key is written lowercase, the form the mapping matches in.
func UnitEntry(key, unit string) string {
	return strings.ToLower(strings.TrimSpace(key)) + "=" + unit
}

// SecretEntry renders the editor.secret_masking_keys entry that masks key;
// SecretExemptEntry the one that exempts it.
func SecretEntry(key string) string { return strings.TrimSpace(key) }

// SecretExemptEntry renders the exempting form of SecretEntry.
func SecretExemptEntry(key string) string { return "-" + strings.TrimSpace(key) }

// EntryPattern returns the pattern half of an entry in either store — the
// left-hand side of a `pattern=unit` mapping, an exempting prefix stripped —
// so a rule for a key can replace the entry that already covers it instead of
// being appended behind it (earlier entries win in both stores).
func EntryPattern(entry string) string {
	s := strings.TrimSpace(entry)
	if i := strings.IndexByte(s, '='); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	for _, p := range []string{"-", "!", "+"} {
		s = strings.TrimPrefix(s, p)
	}
	return strings.ToLower(strings.TrimSpace(s))
}
