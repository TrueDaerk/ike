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
//
// An active project group (0510, #2574) bends that one list rather than
// forking it: the group's members sort to the front, keeping their MRU order
// among themselves, and the rest of the history follows. Every consumer of
// MRUOrder / MRUTargets — both picker flavours, the Recent Projects column
// and the digit chords — therefore still agrees on which project is number 4,
// and inside a group the digits reach the projects one is actually working
// across. project.switchLast is deliberately *not* reordered: it stays the
// MRU parked workspace, group or not.

// MaxMRU is how many recent projects the digit chords reach: one per digit
// key.
const MaxMRU = 9

// MRUOrder returns the recent-project entries in the order every project list
// renders them: the active group's members first, in their MRU order, then
// the rest of the history, newest first. The currently open project is
// dropped — cur is its absolute, cleaned path ("" drops nothing). A zero
// Group (no group active) leaves the plain MRU order untouched.
func MRUOrder(history []Entry, cur string, group Group) []Entry {
	members := make([]Entry, 0, len(history))
	rest := make([]Entry, 0, len(history))
	for _, e := range history {
		if cur != "" && filepath.Clean(e.Path) == cur {
			continue
		}
		if group.Contains(e.Path) {
			members = append(members, e)
			continue
		}
		rest = append(rest, e)
	}
	return append(members, rest...)
}

// MRUTargets returns the roots of MRUOrder's entries: the digit chords' target
// list.
func MRUTargets(history []Entry, cur string, group Group) []string {
	entries := MRUOrder(history, cur, group)
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Path)
	}
	return out
}
