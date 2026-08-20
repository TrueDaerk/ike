package settings

import (
	"strings"

	"ike/internal/numhint"
)

// number_units.go validates an editor.number_hint_units element (#2008). The
// mapping decides the *input* unit a field's numbers are read in — with
// `request_timeout=s` the raw 1500 is 1500 seconds, drawn as "25m" — so an
// entry the installer has to skip does not simply do nothing: the field falls
// back to the built-in key word and keeps rendering in the conventional base
// (1500 milliseconds, "1s500ms"), which reads as the mapping being ignored.
// The form rejects the entry where the config loader only drops it with a
// diagnostic, so a typo is caught while it is being typed.

// numberUnitValidate rejects a mapping entry numhint could not install; ""
// accepts. The lookup seam is unused — an entry stands on its own.
func numberUnitValidate(_ func(key string) string, text string) string {
	if strings.TrimSpace(text) == "" {
		return "entry must not be empty"
	}
	return numhint.EntryError(text)
}

// numberUnitHints lists the unit words while the entry is being typed, so the
// vocabulary is visible without reading the description first.
func numberUnitHints(_ func(key string) string, text string) []string {
	_, unit, ok := strings.Cut(text, "=")
	if !ok {
		return []string{"write pattern=unit, e.g. request_timeout=s"}
	}
	unit = strings.ToLower(strings.TrimSpace(unit))
	if out := prefixed(numhint.UnitVocabulary(), unit); len(out) > 0 {
		return out
	}
	return numhint.UnitVocabulary()
}
