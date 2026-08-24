package ui

// CopyChord reports whether key is the shared modified copy chord of the
// selectable list surfaces (#2062, #2071). It is the muscle-memory alias for
// the bare copy key ("y" in the list panels, "c"/"Y" in the DOM tree).
//
// ctrl+c is deliberately absent: the chord is the global quit and a list
// panel has no text selection that could claim it — only surfaces with a live
// selection (the HTTP response viewer) take it over.
func CopyChord(key string) bool {
	switch key {
	case "cmd+c", "super+c":
		return true
	}
	return false
}
