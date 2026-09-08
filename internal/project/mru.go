package project

import "path/filepath"

// mru.go owns the MRU numbering behind project.switchMRU1…9 (#2489): the
// direct digit chords onto the nine most recently used *other* projects. The
// numbering is derived from one list — the recent-projects history, newest
// first, with the project one is standing in dropped — so the picker
// (project.switch), the Recent Projects column of the recent-files dialog
// (#778) and the chords all agree on which project is number 4. The numbers
// are not rendered anywhere since #2532: the rows were carrying their digit
// as a leading hint, which read as noise next to the project names; the
// chords stay discoverable through their palette command titles.
//
// Entry number one is therefore the project one came from, i.e. the same
// target project.switchLast resumes; the digits generalize that toggle to the
// rest of the list.

// MaxMRU is how many recent projects the digit chords reach: one per digit
// key.
const MaxMRU = 9

// MRUTargets returns the recent-project roots in MRU order (newest first),
// with the currently open project dropped — cur is its absolute, cleaned path
// ("" drops nothing). It is the digit chords' target list.
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

