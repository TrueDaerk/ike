// Package bookmarks is the project-level bookmark store (#55): JetBrains
// style line bookmarks, keyed by file path plus 0-based line, each with an
// optional mnemonic digit (0-9, unique across the project) and a free-text
// note. Unlike the vim marks of the marks package — letter-keyed positions
// owned by the editor — a bookmark belongs to the project and is listed,
// annotated and stepped through as a set.
//
// The store persists per project in .ike/bookmarks.json (IKE_CONFIG_DIR
// overrides the directory, like every other state store). Bookmarks are
// convenience state: a missing or malformed file loads empty, never a
// startup error.
package bookmarks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// version is the on-disk schema version; anything else reads as empty.
const version = 1

// Bookmark is one bookmarked line. Path is the store's key — the app hands
// in project-relative paths so the set travels with the repository — and
// Line is 0-based, matching the editor. Mnemonic is 0 for an anonymous
// bookmark, otherwise a digit rune '0'-'9'.
type Bookmark struct {
	Path     string
	Line     int
	Mnemonic rune
	Note     string
}

// Anonymous reports whether b carries no mnemonic.
func (b Bookmark) Anonymous() bool { return b.Mnemonic == 0 }

// Sign renders the bookmark's gutter glyph: its mnemonic digit, or the flag
// of an anonymous bookmark.
func (b Bookmark) Sign() string {
	if b.Mnemonic == 0 {
		return "⚑"
	}
	return string(b.Mnemonic)
}

// Mnemonic reports whether r is a usable mnemonic (0-9). Letters stay with
// the vim marks (marks.Global), so the two namespaces never collide.
func Mnemonic(r rune) bool { return r >= '0' && r <= '9' }

// Store is the per-project bookmark set, keyed by path then line. Not safe
// for concurrent use: it lives on the root model and is only touched inside
// Update.
type Store struct {
	files map[string]map[int]Bookmark
}

// New returns an empty store.
func New() *Store { return &Store{files: map[string]map[int]Bookmark{}} }

// File returns the path of the persisted store.
func File() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "bookmarks.json")
	}
	return filepath.Join(".ike", "bookmarks.json")
}

// entry is one bookmark on disk, inside its file's line-sorted list.
type entry struct {
	Line     int    `json:"line"`
	Mnemonic string `json:"mnemonic,omitempty"`
	Note     string `json:"note,omitempty"`
}

// persisted is the on-disk schema.
type persisted struct {
	Version int                `json:"version"`
	Files   map[string][]entry `json:"files"`
}

// Load reads the persisted set; a missing or malformed file loads empty.
func Load() *Store {
	s := New()
	data, err := os.ReadFile(File())
	if err != nil {
		return s
	}
	var p persisted
	if json.Unmarshal(data, &p) != nil || p.Version != version {
		return s
	}
	for path, entries := range p.Files {
		if path == "" {
			continue
		}
		for _, e := range entries {
			if e.Line < 0 {
				continue
			}
			b := Bookmark{Path: path, Line: e.Line, Note: e.Note}
			if r := []rune(e.Mnemonic); len(r) == 1 && Mnemonic(r[0]) {
				b.Mnemonic = r[0]
			}
			s.put(b)
		}
	}
	// A hand-edited file may repeat a mnemonic; keeping one occurrence
	// leaves ByMnemonic unambiguous.
	s.dedupeMnemonics()
	return s
}

// Save persists the set; an error is the caller's to surface (never fatal).
// An empty store removes the file rather than leaving an empty husk.
func (s *Store) Save() error {
	path := File()
	if s.Count() == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	p := persisted{Version: version, Files: map[string][]entry{}}
	for file, byLine := range s.files {
		if len(byLine) == 0 {
			continue
		}
		out := make([]entry, 0, len(byLine))
		for line, b := range byLine {
			e := entry{Line: line, Note: b.Note}
			if b.Mnemonic != 0 {
				e.Mnemonic = string(b.Mnemonic)
			}
			out = append(out, e)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Line < out[j].Line })
		p.Files[file] = out
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0o644)
}

// put stores b under its path/line, replacing whatever sat there.
func (s *Store) put(b Bookmark) {
	if s.files == nil {
		s.files = map[string]map[int]Bookmark{}
	}
	if s.files[b.Path] == nil {
		s.files[b.Path] = map[int]Bookmark{}
	}
	s.files[b.Path][b.Line] = b
}

// dedupeMnemonics keeps one bookmark per mnemonic (the lowest path/line
// wins), clearing the digit everywhere else.
func (s *Store) dedupeMnemonics() {
	seen := map[rune]bool{}
	for _, b := range s.All() {
		if b.Mnemonic == 0 {
			continue
		}
		if seen[b.Mnemonic] {
			b.Mnemonic = 0
			s.put(b)
			continue
		}
		seen[b.Mnemonic] = true
	}
}

// Count returns the number of bookmarks in the store.
func (s *Store) Count() int {
	n := 0
	for _, byLine := range s.files {
		n += len(byLine)
	}
	return n
}

// Has reports whether path:line carries a bookmark.
func (s *Store) Has(path string, line int) bool {
	_, ok := s.files[path][line]
	return ok
}

// At returns the bookmark on path:line, ok=false when there is none.
func (s *Store) At(path string, line int) (Bookmark, bool) {
	b, ok := s.files[path][line]
	return b, ok
}

// Add records an anonymous bookmark on path:line, keeping an existing one
// (with its mnemonic and note) untouched.
func (s *Store) Add(path string, line int) {
	if path == "" || line < 0 || s.Has(path, line) {
		return
	}
	s.put(Bookmark{Path: path, Line: line})
}

// Toggle flips the bookmark on path:line, reporting whether one exists
// afterwards. Removing takes the mnemonic and note with it.
func (s *Store) Toggle(path string, line int) bool {
	if s.Has(path, line) {
		s.Remove(path, line)
		return false
	}
	s.Add(path, line)
	return true
}

// Remove drops the bookmark on path:line, reporting whether one existed.
func (s *Store) Remove(path string, line int) bool {
	if !s.Has(path, line) {
		return false
	}
	delete(s.files[path], line)
	if len(s.files[path]) == 0 {
		delete(s.files, path)
	}
	return true
}

// SetMnemonic assigns mnemonic r to path:line, creating the bookmark when
// the line has none. Mnemonics are unique across the project, so the digit
// is cleared wherever it sat before (JetBrains' behaviour). A zero r clears
// the line's mnemonic and keeps the bookmark anonymous.
func (s *Store) SetMnemonic(path string, line int, r rune) {
	if path == "" || line < 0 || (r != 0 && !Mnemonic(r)) {
		return
	}
	if r != 0 {
		if old, ok := s.ByMnemonic(r); ok && (old.Path != path || old.Line != line) {
			old.Mnemonic = 0
			s.put(old)
		}
	}
	b, ok := s.At(path, line)
	if !ok {
		b = Bookmark{Path: path, Line: line}
	}
	b.Mnemonic = r
	s.put(b)
}

// SetNote annotates path:line, creating the bookmark when the line has none;
// an empty note drops the annotation but keeps the bookmark.
func (s *Store) SetNote(path string, line int, note string) {
	if path == "" || line < 0 {
		return
	}
	b, ok := s.At(path, line)
	if !ok {
		b = Bookmark{Path: path, Line: line}
	}
	b.Note = strings.TrimSpace(note)
	s.put(b)
}

// ByMnemonic returns the bookmark carrying mnemonic r, ok=false when the
// digit is unused.
func (s *Store) ByMnemonic(r rune) (Bookmark, bool) {
	if !Mnemonic(r) {
		return Bookmark{}, false
	}
	for _, b := range s.All() {
		if b.Mnemonic == r {
			return b, true
		}
	}
	return Bookmark{}, false
}

// InFile returns path's bookmarks sorted by line.
func (s *Store) InFile(path string) []Bookmark {
	byLine := s.files[path]
	out := make([]Bookmark, 0, len(byLine))
	for _, b := range byLine {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out
}

// All returns every bookmark sorted by path then line — the picker's and the
// next/previous stepper's canonical order.
func (s *Store) All() []Bookmark {
	out := make([]Bookmark, 0, s.Count())
	for _, byLine := range s.files {
		for _, b := range byLine {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// Signs returns path's gutter glyphs by 0-based line (mnemonic digit, or the
// anonymous flag) — the editor's bookmark source.
func (s *Store) Signs(path string) map[int]string {
	byLine := s.files[path]
	if len(byLine) == 0 {
		return nil
	}
	out := make(map[int]string, len(byLine))
	for line, b := range byLine {
		out[line] = b.Sign()
	}
	return out
}

// AdjustEdit shifts path's bookmarks after a buffer edit that changed the
// line count by delta, with the cursor on cursorAfter (0-based) once the
// edit applied — the breakpoint store's semantics
// (debug.Breakpoints.AdjustEdit): insertions move bookmarks at or below the
// insertion point down, deletions pull the ones below the removed range up,
// clamped at the cursor row. Two bookmarks squeezed onto the same line merge
// into one, the lower source line keeping the slot.
func (s *Store) AdjustEdit(path string, cursorAfter, delta int) {
	if delta == 0 || len(s.files[path]) == 0 {
		return
	}
	threshold := cursorAfter - delta + 1
	if delta < 0 {
		threshold = cursorAfter + 1
	}
	moved := map[int]Bookmark{}
	sources := map[int]int{}
	for _, b := range s.InFile(path) {
		src := b.Line
		if b.Line >= threshold {
			b.Line += delta
			if b.Line < cursorAfter {
				b.Line = cursorAfter
			}
			if b.Line < 0 {
				b.Line = 0
			}
		}
		if prev, clash := sources[b.Line]; clash && prev < src {
			continue // the lower source line keeps the slot
		}
		sources[b.Line] = src
		moved[b.Line] = b
	}
	s.files[path] = moved
}

// Rename re-keys the bookmarks of old to new after a file rename/move, and
// of every file below it when old named a directory (the explorer's rename
// hook). Bookmarks already sitting on new are replaced.
func (s *Store) Rename(old, new string) {
	if old == "" || new == "" || old == new {
		return
	}
	sep := string(filepath.Separator)
	for path, byLine := range s.files {
		var target string
		switch {
		case path == old:
			target = new
		case strings.HasPrefix(path, old+sep):
			target = new + strings.TrimPrefix(path, old)
		default:
			continue
		}
		delete(s.files, path)
		moved := make(map[int]Bookmark, len(byLine))
		for line, b := range byLine {
			b.Path = target
			moved[line] = b
		}
		s.files[target] = moved
	}
}
