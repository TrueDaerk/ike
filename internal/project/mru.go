package project

import (
	"path/filepath"
	"strconv"
)

// mru.go owns the MRU numbering behind project.switchMRU1…9 (#2489): the
// direct digit chords onto the nine most recently used *other* projects. The
// numbering is derived from one list — the recent-projects history, newest
// first, with the project one is standing in dropped — so the picker
// (project.switch), the Recent Projects column of the recent-files dialog
// (#778) and the chords all agree on which project is number 4.
//
// Entry number one is therefore the project one came from, i.e. the same
// target project.switchLast resumes; the digits generalize that toggle to the
// rest of the list.

// MaxMRU is how many recent projects the digit chords reach: one per digit
// key, the same span the palette rows carry a hint for.
const MaxMRU = 9

// MRUTargets returns the recent-project roots in MRU order (newest first),
// with the currently open project dropped — cur is its absolute, cleaned path
// ("" drops nothing). It is the digit chords' target list and the numbering
// the palette hints render.
func MRUTargets(history []Entry, cur string) []string {
	out := make([]string, 0, len(history))
	for _, e := range history {
		if cur != "" && filepath.Clean(e.Path) == cur {
			continue
		}
		out = append(out, e.Path)
	}
	return out
}

// MRUHint returns the digit label of the i-th (0-based) entry of an
// MRUTargets list: "1"…"9" for the reachable ones, "" past the ninth, which
// no chord addresses.
func MRUHint(i int) string {
	if i < 0 || i >= MaxMRU {
		return ""
	}
	return strconv.Itoa(i + 1)
}
