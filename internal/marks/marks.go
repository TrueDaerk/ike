// Package marks persists the global vim marks (m{A-Z}, #1151): letter-keyed
// path+position bookmarks that survive a restart. One JSON file lives under
// the state store (the same root the session/undo stores use: IKE_CONFIG_DIR
// when set, otherwise the project's ".ike" directory). The store loads
// lazily on first access and saves on every change, mirroring the undostore
// trade-off: failing to persist must never disrupt editing, so errors are
// swallowed and a malformed file simply reads as empty.
package marks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

const version = 1

// Mark is one global mark target: an absolute file path plus a 0-based
// position. Positions may drift when the file changes outside IKE; jumps
// clamp into the buffer, correctness over continuity.
type Mark struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
}

// Entry pairs a mark with its letter, for listings.
type Entry struct {
	Letter rune
	Mark
}

// envelope is the on-disk schema, letter-keyed ("A".."Z").
type envelope struct {
	Version int             `json:"version"`
	Marks   map[string]Mark `json:"marks"`
}

// Store holds the global marks. The zero value is ready to use; the file is
// read on first access.
type Store struct {
	loaded bool
	marks  map[string]Mark
	// file overrides the on-disk location (tests); empty means the default.
	file string
}

// path returns the store file: IKE_CONFIG_DIR overrides the base directory
// (tests redirect writes), otherwise the project's ".ike".
func (s *Store) path() string {
	if s.file != "" {
		return s.file
	}
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "marks.json")
	}
	return filepath.Join(".ike", "marks.json")
}

// ensure loads the file once; anything malformed reads as empty.
func (s *Store) ensure() {
	if s.loaded {
		return
	}
	s.loaded = true
	s.marks = map[string]Mark{}
	data, err := os.ReadFile(s.path())
	if err != nil {
		return
	}
	var env envelope
	if json.Unmarshal(data, &env) != nil || env.Version != version {
		return
	}
	for k, v := range env.Marks {
		if len([]rune(k)) == 1 && v.Path != "" {
			s.marks[k] = v
		}
	}
}

// save writes the current set; errors are swallowed (see package comment).
func (s *Store) save() {
	file := s.path()
	if len(s.marks) == 0 {
		_ = os.Remove(file)
		return
	}
	data, err := json.Marshal(envelope{Version: version, Marks: s.marks})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(file, data, 0o644)
}

// canon resolves a path to its absolute form, the store's comparison key.
func canon(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

// Global reports whether r names a global mark (A-Z).
func Global(r rune) bool { return r >= 'A' && r <= 'Z' }

// Set records mark r at path:line:col (0-based) and persists.
func (s *Store) Set(r rune, path string, line, col int) {
	if !Global(r) || path == "" {
		return
	}
	s.ensure()
	s.marks[string(r)] = Mark{Path: canon(path), Line: line, Col: col}
	s.save()
}

// Get returns mark r, ok=false when unset.
func (s *Store) Get(r rune) (Mark, bool) {
	s.ensure()
	mk, ok := s.marks[string(r)]
	return mk, ok
}

// Remove drops mark r and persists.
func (s *Store) Remove(r rune) {
	s.ensure()
	if _, ok := s.marks[string(r)]; !ok {
		return
	}
	delete(s.marks, string(r))
	s.save()
}

// All returns every mark sorted by letter.
func (s *Store) All() []Entry {
	s.ensure()
	out := make([]Entry, 0, len(s.marks))
	for k, v := range s.marks {
		out = append(out, Entry{Letter: []rune(k)[0], Mark: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Letter < out[j].Letter })
	return out
}

// Lines returns the 0-based marked lines of path (for the gutter).
func (s *Store) Lines(path string) []int {
	s.ensure()
	abs := canon(path)
	var lines []int
	for _, v := range s.marks {
		if v.Path == abs {
			lines = append(lines, v.Line)
		}
	}
	sort.Ints(lines)
	return lines
}

// AdjustEdit shifts path's marks after a buffer edit that changed the line
// count by delta, with the cursor on cursorAfter (0-based) once the edit
// applied — the breakpoint store's semantics (debug.Breakpoints.AdjustEdit):
// insertions move marks at or below the insertion point down; deletions pull
// the ones below the removed range up, clamped at the cursor row.
func (s *Store) AdjustEdit(path string, cursorAfter, delta int) {
	if delta == 0 {
		return
	}
	s.ensure()
	abs := canon(path)
	threshold := cursorAfter - delta + 1
	if delta < 0 {
		threshold = cursorAfter + 1
	}
	changed := false
	for k, v := range s.marks {
		if v.Path != abs || v.Line < threshold {
			continue
		}
		v.Line += delta
		if v.Line < cursorAfter {
			v.Line = cursorAfter
		}
		if v.Line < 0 {
			v.Line = 0
		}
		s.marks[k] = v
		changed = true
	}
	if changed {
		s.save()
	}
}
