package debug

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// watches.go is the watch-expression store (#2174): the debugger's list of
// user expressions, persisted per project in .ike/watches.json next to the
// breakpoints (IKE_CONFIG_DIR override like every other state store). The
// list is adapter-independent — it outlives sessions and restarts, and is
// re-evaluated on every stop.

// Watches is the per-project watch-expression list, in user order. Not safe
// for concurrent use; it lives on the root model and is only touched inside
// Update, like Breakpoints.
type Watches struct {
	exprs []string
}

// NewWatches returns an empty list.
func NewWatches() *Watches { return &Watches{} }

// WatchesFile returns the path of the persisted list.
func WatchesFile() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "watches.json")
	}
	return filepath.Join(".ike", "watches.json")
}

// persistedWatches is the on-disk shape — an object, not a bare array, so a
// later field (per-expression format hints, say) does not break the file.
type persistedWatches struct {
	Expressions []string `json:"expressions"`
}

// LoadWatches reads the persisted list; a missing or malformed file loads
// empty — watch expressions are convenience state, never a startup error.
// Blank entries are dropped on the way in.
func LoadWatches() *Watches {
	w := NewWatches()
	data, err := os.ReadFile(WatchesFile())
	if err != nil {
		return w
	}
	var p persistedWatches
	if json.Unmarshal(data, &p) != nil {
		return w
	}
	for _, e := range p.Expressions {
		if e = strings.TrimSpace(e); e != "" {
			w.exprs = append(w.exprs, e)
		}
	}
	return w
}

// Save persists the list; an error is the caller's to surface (never fatal).
func (w *Watches) Save() error {
	data, err := json.MarshalIndent(persistedWatches{Expressions: w.exprs}, "", "  ")
	if err != nil {
		return err
	}
	path := WatchesFile()
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0o644)
}

// List returns the expressions in order (a copy — the caller may keep it).
func (w *Watches) List() []string {
	return append([]string(nil), w.exprs...)
}

// Len reports how many expressions are watched.
func (w *Watches) Len() int { return len(w.exprs) }

// Add appends expr, reporting whether it changed the list; a blank
// expression is ignored (an emptied inline editor is a cancel, not a watch).
func (w *Watches) Add(expr string) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false
	}
	w.exprs = append(w.exprs, expr)
	return true
}

// Replace rewrites index i, reporting whether it changed the list. A blank
// expression removes the row instead — the panel's "commit an emptied
// expression removes the watch" rule.
func (w *Watches) Replace(i int, expr string) bool {
	if i < 0 || i >= len(w.exprs) {
		return false
	}
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return w.Remove(i)
	}
	if w.exprs[i] == expr {
		return false
	}
	w.exprs[i] = expr
	return true
}

// Remove deletes index i, reporting whether it changed the list.
func (w *Watches) Remove(i int) bool {
	if i < 0 || i >= len(w.exprs) {
		return false
	}
	w.exprs = append(w.exprs[:i], w.exprs[i+1:]...)
	return true
}
