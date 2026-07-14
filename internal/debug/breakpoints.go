// Package debug holds the debugger-side data model (0350). This file is the
// breakpoint store (#577): line breakpoints keyed by project-relative file
// path, persisted per project in .ike/breakpoints.json (IKE_CONFIG_DIR
// override like the other state stores). Lines are 0-based buffer lines,
// matching the editor; the JSON stores them 0-based too.
package debug

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// Breakpoints is the per-project breakpoint set. Not safe for concurrent use;
// it lives on the root model and is only touched inside Update.
type Breakpoints struct {
	files map[string][]int
}

// NewBreakpoints returns an empty store.
func NewBreakpoints() *Breakpoints {
	return &Breakpoints{files: map[string][]int{}}
}

// File returns the path of the persisted store.
func File() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "breakpoints.json")
	}
	return filepath.Join(".ike", "breakpoints.json")
}

// Load reads the persisted set; missing or malformed files load empty —
// breakpoints are convenience state, never a startup error.
func Load() *Breakpoints {
	b := NewBreakpoints()
	data, err := os.ReadFile(File())
	if err != nil {
		return b
	}
	var files map[string][]int
	if json.Unmarshal(data, &files) != nil || files == nil {
		return b
	}
	for path, lines := range files {
		for _, l := range lines {
			b.set(path, l)
		}
	}
	return b
}

// Save persists the set; an error is the caller's to surface (never fatal).
func (b *Breakpoints) Save() error {
	data, err := json.MarshalIndent(b.files, "", "  ")
	if err != nil {
		return err
	}
	path := File()
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0o644)
}

// Toggle flips the breakpoint on path:line and reports whether one exists
// afterwards.
func (b *Breakpoints) Toggle(path string, line int) bool {
	if b.Has(path, line) {
		b.clear(path, line)
		return false
	}
	b.set(path, line)
	return true
}

// Has reports whether path:line carries a breakpoint.
func (b *Breakpoints) Has(path string, line int) bool {
	for _, l := range b.files[path] {
		if l == line {
			return true
		}
	}
	return false
}

// Lines returns path's breakpoint lines, sorted ascending.
func (b *Breakpoints) Lines(path string) []int {
	src := b.files[path]
	out := make([]int, len(src))
	copy(out, src)
	return out
}

// All returns every file's breakpoint lines (sorted; the map is a copy).
func (b *Breakpoints) All() map[string][]int {
	out := make(map[string][]int, len(b.files))
	for path := range b.files {
		out[path] = b.Lines(path)
	}
	return out
}

// Count reports the total number of breakpoints.
func (b *Breakpoints) Count() int {
	n := 0
	for _, lines := range b.files {
		n += len(lines)
	}
	return n
}

// AdjustEdit shifts path's breakpoints after a buffer edit that changed the
// line count by delta, with the cursor on cursorAfter (0-based) once the edit
// applied. Insertions move breakpoints at or below the insertion point down;
// deletions pull the ones below the removed range up (a breakpoint inside the
// removed range collapses onto the line that takes its place, deduplicated).
func (b *Breakpoints) AdjustEdit(path string, cursorAfter, delta int) {
	if delta == 0 {
		return
	}
	lines := b.files[path]
	if len(lines) == 0 {
		return
	}
	// Insertion of N lines ending at cursorAfter: lines at or after the first
	// inserted row (cursorAfter-delta+1) shift down. Deletion of N lines at
	// cursorAfter: lines beyond it shift up, clamped at the cursor row.
	threshold := cursorAfter - delta + 1
	if delta < 0 {
		threshold = cursorAfter + 1
	}
	next := make([]int, 0, len(lines))
	seen := map[int]bool{}
	for _, l := range lines {
		if l >= threshold {
			l += delta
			if l < cursorAfter {
				l = cursorAfter
			}
		}
		if l < 0 || seen[l] {
			continue
		}
		seen[l] = true
		next = append(next, l)
	}
	sort.Ints(next)
	b.files[path] = next
}

func (b *Breakpoints) set(path string, line int) {
	if line < 0 || b.Has(path, line) {
		return
	}
	lines := append(b.files[path], line)
	sort.Ints(lines)
	b.files[path] = lines
}

func (b *Breakpoints) clear(path string, line int) {
	lines := b.files[path]
	for i, l := range lines {
		if l == line {
			lines = append(lines[:i], lines[i+1:]...)
			break
		}
	}
	if len(lines) == 0 {
		delete(b.files, path)
		return
	}
	b.files[path] = lines
}
