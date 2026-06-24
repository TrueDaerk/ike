package keymap

import "time"

// TimeoutDuration is how long the resolver waits for the next step of a partial
// multi-step chord before discarding it (or resolving an exact match that is
// also a prefix of a longer chord). The caller schedules a timer on Pending.
const TimeoutDuration = 600 * time.Millisecond

// Status is the outcome of feeding a key to the resolver.
type Status int

const (
	// NoMatch: the key (and any partial state) maps to nothing; the caller should
	// let it fall through to the focused pane.
	NoMatch Status = iota
	// Pending: the sequence so far is a prefix of a longer chord; the resolver is
	// holding partial state and the caller should arm a TimeoutDuration timer.
	Pending
	// Resolved: a binding matched; Command/Binding carry it.
	Resolved
)

// Result is what Feed/Timeout report.
type Result struct {
	Status  Status
	Command string
	Binding Binding
}

// Resolver feeds keys against a BindingTable, tracking partial multi-step chord
// state. It is single-context-stack aware: each Feed carries the active focus
// context so pane-scoped bindings shadow Global ones. It holds no timer itself;
// on Pending the caller arms a TimeoutDuration timer and calls Timeout when it
// fires.
type Resolver struct {
	table   *BindingTable
	pending Chord
}

// NewResolver returns a resolver over table.
func NewResolver(table *BindingTable) *Resolver { return &Resolver{table: table} }

// Pending reports whether the resolver is mid-chord (awaiting more steps).
func (r *Resolver) Pending() bool { return r.pending.Len() > 0 }

// Reset discards any partial chord state.
func (r *Resolver) Reset() { r.pending = Chord{} }

// Feed advances the resolver with one key in the active context. A key that
// neither completes nor extends a chord while partial state is held restarts the
// sequence from that key alone, so an aborted prefix never strands a fresh chord.
func (r *Resolver) Feed(key Key, active Context) Result {
	key = NormalizeKey(key, GOOS)
	steps := append(append([]Key{}, r.pending.Steps...), key)
	cand := Chord{Steps: steps}

	// A prefix of a longer chord wins over an equal-length exact match so the
	// multi-step form (cmd+k cmd+c) is reachable; the exact match (cmd+k) is
	// recovered on Timeout.
	if r.table.IsPrefix(cand, active) {
		r.pending = cand
		return Result{Status: Pending}
	}
	if b, ok := r.table.Lookup(cand, active); ok {
		r.pending = Chord{}
		return Result{Status: Resolved, Command: b.Command, Binding: b}
	}
	// The accumulated sequence dead-ended; drop it and retry the key on its own.
	r.pending = Chord{}
	if len(steps) > 1 {
		return r.Feed(key, active)
	}
	return Result{Status: NoMatch}
}

// Timeout resolves a held partial chord when its timer fires: if the pending
// sequence is itself an exact binding it resolves, otherwise it is discarded.
func (r *Resolver) Timeout(active Context) Result {
	pending := r.pending
	r.pending = Chord{}
	if pending.Len() == 0 {
		return Result{Status: NoMatch}
	}
	if b, ok := r.table.Lookup(pending, active); ok {
		return Result{Status: Resolved, Command: b.Command, Binding: b}
	}
	return Result{Status: NoMatch}
}
