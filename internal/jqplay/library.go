package jqplay

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// library.go is the named saved-filter store (#1995): the durable half of the
// playground's memory. The session history (History, #1977) answers "what did
// I run recently"; it rotates, it is unnamed, and it dies with the process. A
// filter that survives weeks — the mapping-flattening one written once for an
// Elasticsearch response — needs a name and a file, which is this.
//
// Two stores of the same shape live side by side, told apart only by the path
// the host hands in: the **project** one (the project's `.ike` state, next to
// the HTTP environment selection) and the **global** one (the user's `~/.ike`,
// next to the window layouts). Everything here is path-agnostic, so the two
// scopes share one implementation and one set of tests; internal/app owns the
// two paths and the scope labels the picker renders.
//
// The store is a *file per scope*, not a config key: a jq program is data the
// user creates from inside the IDE, like a saved layout, and hand-editing the
// config file to add one would be the wrong affordance.

// MaxFilters caps one scope's library. A saved filter is a deliberate act, so
// the cap is a runaway guard (a scripted Save loop, a hand-edited file), not a
// budget anyone should feel: Save refuses to grow a store past it.
const MaxFilters = 500

// ErrLibraryFull is returned when adding a new name would exceed MaxFilters.
// Overwriting an existing name never fails this way.
var ErrLibraryFull = errors.New("the filter library is full")

// ErrNoName is returned for an empty (or whitespace-only) filter name.
var ErrNoName = errors.New("a saved filter needs a name")

// ErrNoProgram is returned when there is no program to save under a name — an
// empty query line is not a filter.
var ErrNoProgram = errors.New("a saved filter needs a program")

// ErrNameTaken is returned by Rename when the new name already exists. Set
// deliberately does not use it: overwriting under the same name is how a
// filter is edited, and the host confirms that with a second enter.
var ErrNameTaken = errors.New("a filter with that name already exists")

// ErrNotFound names a filter the store does not hold.
var ErrNotFound = errors.New("no saved filter with that name")

// Scope names one of the two libraries. The zero value is the project scope,
// which is the default a save prompt opens on: a filter written against this
// project's data usually belongs to this project, and promoting it to the
// global library is one keystroke away.
type Scope int

// The two scopes: the project's own state directory and the user's home.
const (
	ScopeProject Scope = iota
	ScopeGlobal
)

// String is the scope's user-facing label — the picker's badge and the save
// prompt's toggle line both render it.
func (s Scope) String() string {
	if s == ScopeGlobal {
		return "global"
	}
	return "project"
}

// Other is the scope the save prompt's toggle switches to.
func (s Scope) Other() Scope {
	if s == ScopeGlobal {
		return ScopeProject
	}
	return ScopeGlobal
}

// Scopes lists the scopes in the order the picker shows them: the project's
// own filters first, the cross-project ones after.
func Scopes() []Scope { return []Scope{ScopeProject, ScopeGlobal} }

// Filter is one saved entry: the name it is listed and picked under, and the
// jq program it inserts into the query line.
type Filter struct {
	Name    string `json:"name"`
	Program string `json:"program"`
}

// Library is one scope's saved filters, held sorted by name so the picker's
// order is the file's order and a diff of the store reads sensibly.
type Library struct {
	Filters []Filter `json:"filters"`
}

// LoadLibrary reads the store at path. A missing, unreadable or malformed
// file yields an *empty* library rather than an error — the same tolerance
// every other state file in the IDE has, because a broken store must never
// stop the playground from opening. Entries with an empty name or program are
// dropped on the way in, so a hand-edited file cannot produce an unpickable
// row.
func LoadLibrary(path string) *Library {
	lib := &Library{}
	if path == "" {
		return lib
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return lib
	}
	var onDisk Library
	if json.Unmarshal(data, &onDisk) != nil {
		return lib
	}
	seen := make(map[string]bool, len(onDisk.Filters))
	for _, f := range onDisk.Filters {
		f.Name = strings.TrimSpace(f.Name)
		f.Program = strings.TrimSpace(f.Program)
		if f.Name == "" || f.Program == "" || seen[f.Name] {
			continue
		}
		seen[f.Name] = true
		lib.Filters = append(lib.Filters, f)
	}
	lib.sort()
	return lib
}

// Save writes the store to path, creating the directory when needed. Unlike
// the load side an error is reported: a save the user asked for and that did
// not happen is worth a message.
func (l *Library) Save(path string) error {
	if path == "" {
		return errors.New("no filter library path")
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Len reports how many filters the scope holds.
func (l *Library) Len() int {
	if l == nil {
		return 0
	}
	return len(l.Filters)
}

// All returns the filters in name order. The slice is the store's own — the
// callers here only read it — so a mutation by a caller is a bug, not a
// supported edit; every change goes through Set/Rename/Delete.
func (l *Library) All() []Filter {
	if l == nil {
		return nil
	}
	return l.Filters
}

// Get returns the filter saved under name.
func (l *Library) Get(name string) (Filter, bool) {
	if l == nil {
		return Filter{}, false
	}
	for _, f := range l.Filters {
		if f.Name == name {
			return f, true
		}
	}
	return Filter{}, false
}

// Has reports whether name is taken — what the host's overwrite guard asks
// before the first enter of the save prompt.
func (l *Library) Has(name string) bool {
	_, ok := l.Get(strings.TrimSpace(name))
	return ok
}

// Set saves program under name, replacing an entry with the same name (which
// is how an edited filter is written back). Name and program are trimmed;
// either being empty is an error, as is growing the store past MaxFilters.
func (l *Library) Set(name, program string) error {
	name, program = strings.TrimSpace(name), strings.TrimSpace(program)
	if name == "" {
		return ErrNoName
	}
	if program == "" {
		return ErrNoProgram
	}
	for i, f := range l.Filters {
		if f.Name == name {
			l.Filters[i].Program = program
			return nil
		}
	}
	if len(l.Filters) >= MaxFilters {
		return ErrLibraryFull
	}
	l.Filters = append(l.Filters, Filter{Name: name, Program: program})
	l.sort()
	return nil
}

// Rename moves the entry from to to, refusing a name that is already taken —
// a rename must never silently swallow another filter, which is exactly the
// case the deliberate Set overwrite does not cover. Renaming to the same name
// is a no-op, not an error.
func (l *Library) Rename(from, to string) error {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if to == "" {
		return ErrNoName
	}
	idx := -1
	for i, f := range l.Filters {
		if f.Name == from {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrNotFound
	}
	if from == to {
		return nil
	}
	if l.Has(to) {
		return ErrNameTaken
	}
	l.Filters[idx].Name = to
	l.sort()
	return nil
}

// Delete removes the entry saved under name.
func (l *Library) Delete(name string) error {
	name = strings.TrimSpace(name)
	for i, f := range l.Filters {
		if f.Name == name {
			l.Filters = append(l.Filters[:i], l.Filters[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

// sort orders the filters by name, case-insensitively so "Errors" and
// "errors" sit together in the picker instead of in two alphabets; equal
// folds fall back to the exact name for a stable order.
func (l *Library) sort() {
	sort.SliceStable(l.Filters, func(i, j int) bool {
		a, b := strings.ToLower(l.Filters[i].Name), strings.ToLower(l.Filters[j].Name)
		if a == b {
			return l.Filters[i].Name < l.Filters[j].Name
		}
		return a < b
	})
}

// Preview renders a program as one line for the picker's detail chip: newlines
// and runs of whitespace collapse to single spaces, and a long program is cut
// with an ellipsis. A saved filter can be a multi-line pipeline, and the row
// only has to say *which* one this is.
func Preview(program string, width int) string {
	one := strings.Join(strings.Fields(program), " ")
	if width <= 0 {
		return one
	}
	r := []rune(one)
	if len(r) <= width {
		return one
	}
	if width == 1 {
		return "…"
	}
	return string(r[:width-1]) + "…"
}
