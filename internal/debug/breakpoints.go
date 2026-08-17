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
// it lives on the root model and is only touched inside Update. Every
// breakpoint is enabled unless it appears in the disabled subset (#1377);
// disabled breakpoints keep their gutter marker but are never pushed to a
// debug adapter.
type Breakpoints struct {
	files    map[string][]int
	disabled map[string]map[int]bool
	meta     map[string]map[int]Meta
}

// Meta are the optional refinements of one breakpoint (#1914): a condition
// expression, a hit-count condition and a log message. A breakpoint with a
// log message is a logpoint — it logs instead of stopping. The zero Meta is
// a plain breakpoint.
type Meta struct {
	Condition    string `json:"condition,omitempty"`
	HitCondition string `json:"hit_condition,omitempty"`
	LogMessage   string `json:"log_message,omitempty"`
}

// IsZero reports whether m carries no refinement.
func (m Meta) IsZero() bool { return m == Meta{} }

// Spec is one enabled breakpoint with its refinements — the shape the debug
// adapters receive (#1914). Line is 0-based like the rest of the store.
type Spec struct {
	Line int
	Meta
}

// NewBreakpoints returns an empty store.
func NewBreakpoints() *Breakpoints {
	return &Breakpoints{
		files:    map[string][]int{},
		disabled: map[string]map[int]bool{},
		meta:     map[string]map[int]Meta{},
	}
}

// File returns the path of the persisted store.
func File() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "breakpoints.json")
	}
	return filepath.Join(".ike", "breakpoints.json")
}

// persisted is the on-disk shape since #1377: the full set plus the disabled
// subset. Files written before #1377 hold the bare files map; Load falls back
// to that layout, so old stores upgrade silently on the next Save.
type persisted struct {
	Files    map[string][]int           `json:"files"`
	Disabled map[string][]int           `json:"disabled,omitempty"`
	Meta     map[string][]persistedMeta `json:"meta,omitempty"`
}

// persistedMeta is one line's refinements on disk (#1914); the embedded Meta
// flattens into the same object as the line.
type persistedMeta struct {
	Line int `json:"line"`
	Meta
}

// Load reads the persisted set; missing or malformed files load empty —
// breakpoints are convenience state, never a startup error.
func Load() *Breakpoints {
	b := NewBreakpoints()
	data, err := os.ReadFile(File())
	if err != nil {
		return b
	}
	var p persisted
	if json.Unmarshal(data, &p) != nil || p.Files == nil {
		// Legacy layout (pre-#1377): the bare files map.
		var files map[string][]int
		if json.Unmarshal(data, &files) != nil || files == nil {
			return b
		}
		p = persisted{Files: files}
	}
	for path, lines := range p.Files {
		for _, l := range lines {
			b.set(path, l)
		}
	}
	for path, lines := range p.Disabled {
		for _, l := range lines {
			b.SetEnabled(path, l, false)
		}
	}
	for path, entries := range p.Meta {
		for _, e := range entries {
			b.SetMeta(path, e.Line, e.Meta)
		}
	}
	return b
}

// Save persists the set; an error is the caller's to surface (never fatal).
func (b *Breakpoints) Save() error {
	p := persisted{Files: b.files}
	for path, lines := range b.disabled {
		if len(lines) == 0 {
			continue
		}
		if p.Disabled == nil {
			p.Disabled = map[string][]int{}
		}
		out := make([]int, 0, len(lines))
		for l := range lines {
			out = append(out, l)
		}
		sort.Ints(out)
		p.Disabled[path] = out
	}
	for path, byLine := range b.meta {
		if len(byLine) == 0 {
			continue
		}
		if p.Meta == nil {
			p.Meta = map[string][]persistedMeta{}
		}
		out := make([]persistedMeta, 0, len(byLine))
		for l, m := range byLine {
			out = append(out, persistedMeta{Line: l, Meta: m})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Line < out[j].Line })
		p.Meta[path] = out
	}
	data, err := json.MarshalIndent(p, "", "  ")
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

// Remove deletes the breakpoint on path:line, reporting whether one existed.
func (b *Breakpoints) Remove(path string, line int) bool {
	if !b.Has(path, line) {
		return false
	}
	b.clear(path, line)
	return true
}

// RemoveAll empties the store, reporting how many breakpoints it dropped.
func (b *Breakpoints) RemoveAll() int {
	n := b.Count()
	b.files = map[string][]int{}
	b.disabled = map[string]map[int]bool{}
	b.meta = map[string]map[int]Meta{}
	return n
}

// SetMeta attaches refinements to path:line (#1914); a zero Meta removes the
// entry, a line without a breakpoint is ignored.
func (b *Breakpoints) SetMeta(path string, line int, m Meta) {
	if !b.Has(path, line) {
		return
	}
	if m.IsZero() {
		delete(b.meta[path], line)
		if len(b.meta[path]) == 0 {
			delete(b.meta, path)
		}
		return
	}
	if b.meta[path] == nil {
		b.meta[path] = map[int]Meta{}
	}
	b.meta[path][line] = m
}

// MetaAt returns path:line's refinements (zero for a plain breakpoint).
func (b *Breakpoints) MetaAt(path string, line int) Meta {
	return b.meta[path][line]
}

// EnabledSpecs returns path's enabled breakpoints with their refinements,
// sorted ascending by line — the set a debug adapter receives (#1914).
func (b *Breakpoints) EnabledSpecs(path string) []Spec {
	lines := b.EnabledLines(path)
	out := make([]Spec, len(lines))
	for i, l := range lines {
		out[i] = Spec{Line: l, Meta: b.meta[path][l]}
	}
	return out
}

// SetEnabled marks path:line enabled or disabled (#1377); a line without a
// breakpoint is ignored.
func (b *Breakpoints) SetEnabled(path string, line int, on bool) {
	if !b.Has(path, line) {
		return
	}
	if on {
		delete(b.disabled[path], line)
		if len(b.disabled[path]) == 0 {
			delete(b.disabled, path)
		}
		return
	}
	if b.disabled[path] == nil {
		b.disabled[path] = map[int]bool{}
	}
	b.disabled[path][line] = true
}

// Enabled reports whether path:line carries an enabled breakpoint.
func (b *Breakpoints) Enabled(path string, line int) bool {
	return b.Has(path, line) && !b.disabled[path][line]
}

// EnabledLines returns path's enabled breakpoint lines, sorted ascending —
// the set a debug adapter receives (#1377).
func (b *Breakpoints) EnabledLines(path string) []int {
	out := make([]int, 0, len(b.files[path]))
	for _, l := range b.files[path] {
		if !b.disabled[path][l] {
			out = append(out, l)
		}
	}
	return out
}

// DisabledLines returns path's disabled breakpoint lines, sorted ascending —
// the gutter renders these hollow (#1377).
func (b *Breakpoints) DisabledLines(path string) []int {
	var out []int
	for _, l := range b.files[path] {
		if b.disabled[path][l] {
			out = append(out, l)
		}
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
	var disabled map[int]bool
	var meta map[int]Meta
	for _, l := range lines {
		wasDisabled := b.disabled[path][l]
		wasMeta := b.meta[path][l]
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
		// The disabled flag travels with its shifted line (#1377), as do the
		// refinements (#1914).
		if wasDisabled {
			if disabled == nil {
				disabled = map[int]bool{}
			}
			disabled[l] = true
		}
		if !wasMeta.IsZero() {
			if meta == nil {
				meta = map[int]Meta{}
			}
			meta[l] = wasMeta
		}
	}
	sort.Ints(next)
	b.files[path] = next
	if disabled == nil {
		delete(b.disabled, path)
	} else {
		b.disabled[path] = disabled
	}
	if meta == nil {
		delete(b.meta, path)
	} else {
		b.meta[path] = meta
	}
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
	delete(b.disabled[path], line)
	if len(b.disabled[path]) == 0 {
		delete(b.disabled, path)
	}
	delete(b.meta[path], line)
	if len(b.meta[path]) == 0 {
		delete(b.meta, path)
	}
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
