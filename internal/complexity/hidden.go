package complexity

// hidden.go is the hidden set (Roadmap 0551, #3031): the functions a
// project has decided not to look at in the Complexity surfaces at all —
// anonymous module initialisers, generated units, a vendored helper the
// exclude globs cannot single out. It is the stronger sibling of the
// baseline: an accepted function keeps its row and merely stops being
// flagged, a hidden one is dropped from every list, aggregate, feed, export
// and CLI table as if it had never been measured (it still is, so it can be
// un-hidden). The set lives in the baseline file as its `hidden` section,
// keyed exactly like an accepted entry (file, name, ordinal among the
// same-named units in source order), and is independent of the
// complexity.baseline setting: the file is read and written for the hidden
// set even while the baseline is off.
//
// File-level hides are not stored here: hiding a file adds a glob to
// complexity.exclude, so the index never measures it and the Settings UI
// and the index share one source of truth.

import (
	"encoding/json"
	"fmt"
	"sort"
)

// HiddenEntry names one hidden function: the project-relative slash path,
// Function.Name and the ordinal among the file's same-named units.
type HiddenEntry struct {
	File     string `json:"file"`
	Function string `json:"function"`
	Ordinal  int    `json:"ordinal"`
}

// Key identifies the entry's function.
func (e HiddenEntry) Key() EntryKey { return EntryKey{e.File, e.Function, e.Ordinal} }

// HiddenSet is the set of hidden functions. It marshals as the JSON array
// of its entries; the zero value and nil are empty sets every method
// accepts.
type HiddenSet struct {
	entries []HiddenEntry
	index   map[EntryKey]bool
}

// MarshalJSON writes the sorted entry array.
func (h *HiddenSet) MarshalJSON() ([]byte, error) {
	if h == nil {
		return []byte("[]"), nil
	}
	h.sort()
	return json.Marshal(h.entries)
}

// UnmarshalJSON reads an entry array.
func (h *HiddenSet) UnmarshalJSON(data []byte) error {
	var entries []HiddenEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}
	h.entries, h.index = entries, nil
	return nil
}

// validate rejects an entry without a file or function or with a negative
// ordinal; path names the file for the message.
func (h *HiddenSet) validate(path string) error {
	if h == nil {
		return nil
	}
	for i, e := range h.entries {
		if e.File == "" || e.Function == "" || e.Ordinal < 0 {
			return fmt.Errorf("%s: hidden[%d] needs a file, a function and an ordinal ≥ 0", path, i)
		}
	}
	return nil
}

// sort orders the entries by file, function, ordinal and drops duplicate
// keys.
func (h *HiddenSet) sort() {
	sort.SliceStable(h.entries, func(i, j int) bool {
		a, c := h.entries[i], h.entries[j]
		if a.File != c.File {
			return a.File < c.File
		}
		if a.Function != c.Function {
			return a.Function < c.Function
		}
		return a.Ordinal < c.Ordinal
	})
	out := h.entries[:0]
	for i, e := range h.entries {
		if i+1 < len(h.entries) && h.entries[i+1].Key() == e.Key() {
			continue
		}
		out = append(out, e)
	}
	h.entries = out
	h.index = nil
}

// Len is the entry count; 0 for a nil set.
func (h *HiddenSet) Len() int {
	if h == nil {
		return 0
	}
	return len(h.entries)
}

// Entries lists the hidden functions sorted by file, function, ordinal.
func (h *HiddenSet) Entries() []HiddenEntry {
	if h == nil {
		return nil
	}
	h.sort()
	return append([]HiddenEntry(nil), h.entries...)
}

// Has reports whether k is hidden; a nil set hides nothing.
func (h *HiddenSet) Has(k EntryKey) bool {
	if h == nil {
		return false
	}
	if h.index == nil {
		h.index = make(map[EntryKey]bool, len(h.entries))
		for _, e := range h.entries {
			h.index[e.Key()] = true
		}
	}
	return h.index[k]
}

// Add hides k and reports whether it was not hidden before.
func (h *HiddenSet) Add(k EntryKey) bool {
	if h.Has(k) {
		return false
	}
	h.entries = append(h.entries, HiddenEntry{File: k.File, Function: k.Function, Ordinal: k.Ordinal})
	h.index = nil
	return true
}

// Remove un-hides k and reports whether it was hidden.
func (h *HiddenSet) Remove(k EntryKey) bool {
	if !h.Has(k) {
		return false
	}
	for i, e := range h.entries {
		if e.Key() == k {
			h.entries = append(h.entries[:i], h.entries[i+1:]...)
			break
		}
	}
	h.index = nil
	return true
}

// Mask reports, per function of file (project-relative slash path) in
// source order, whether it is hidden — the ordinals come from Ordinals, so
// (anonymous)#2 hides exactly the second anonymous unit and a function
// that moves inside its file stays hidden. A nil or empty set yields nil,
// which every consumer reads as "nothing hidden".
func (h *HiddenSet) Mask(file string, fns []Function) []bool {
	if h.Len() == 0 {
		return nil
	}
	out := make([]bool, len(fns))
	for i, ord := range Ordinals(fns) {
		out[i] = h.Has(EntryKey{file, fns[i].Name, ord})
	}
	return out
}

// Visible returns fns without the hidden units of file, and how many it
// dropped; the slice is fns itself when nothing is hidden.
func (h *HiddenSet) Visible(file string, fns []Function) ([]Function, int) {
	mask := h.Mask(file, fns)
	if mask == nil {
		return fns, 0
	}
	out := make([]Function, 0, len(fns))
	dropped := 0
	for i, f := range fns {
		if mask[i] {
			dropped++
			continue
		}
		out = append(out, f)
	}
	return out, dropped
}

// HiddenOf is the baseline's hidden set, nil-safe: a nil baseline hides
// nothing.
func (b *Baseline) HiddenOf() *HiddenSet {
	if b == nil {
		return nil
	}
	return b.Hidden
}

// Hide adds k to the baseline's hidden set, creating it, and reports
// whether k was not hidden before.
func (b *Baseline) Hide(k EntryKey) bool {
	if b.Hidden == nil {
		b.Hidden = &HiddenSet{}
	}
	return b.Hidden.Add(k)
}

// Unhide drops k from the hidden set and reports whether it was hidden;
// an emptied set is removed so the file loses its `hidden` section.
func (b *Baseline) Unhide(k EntryKey) bool {
	ok := b.Hidden.Remove(k)
	if b.Hidden.Len() == 0 {
		b.Hidden = nil
	}
	return ok
}
