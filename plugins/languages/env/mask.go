package langenv

// mask.go masks the values people should not have on screen by accident
// (#1623): in a dotenv file the key names the value, so `STRIPE_SECRET_KEY=`
// or `DB_PASSWORD=` can be recognised without ever looking at what follows.
// The value is emitted as a stand-in span carrying secret.Mask, which the
// editor's conceal layer draws instead of the source runes — and reveals
// positionally under the caret (#1594), so reading or editing one value never
// exposes the rest of the file to the room.
//
// The heuristic itself lives in internal/secret, shared with any other host
// that grows the same need.

import (
	"ike/internal/lang"
	"ike/internal/secret"
)

// maskSpans returns the stand-in span for the value of a secret-suspect key on
// one line, or nil when the line assigns nothing, the key looks harmless, or
// the value is empty (there is nothing to hide, and a mask over nothing only
// reads as a value that is not there).
func maskSpans(li int, runes []rune, start, end int) []lang.Span {
	e := parseEntry(runes, start, end)
	if e.sep < 0 || e.valEnd <= e.valStart || !secret.Suspect(e.key) {
		return nil
	}
	return []lang.Span{{
		Line:     li,
		StartCol: e.valStart,
		EndCol:   e.valEnd,
		Capture:  secret.Capture,
		Replace:  secret.Mask,
	}}
}
