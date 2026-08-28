// Package changefeed is the session-scoped record of files changed by
// something other than IKE (#2000): a coding agent writing across the tree, a
// `git checkout`, a formatter run in a terminal pane. The watcher already
// reports those events and already suppresses IKE's own saves; this package
// turns the stream into a reviewable list — newest first, one row per file,
// each carrying the pre-change content so the change can be diffed and undone.
//
// It is pure data: no I/O, no clock, no config. The caller supplies the time,
// the pre-change text, and the noise predicate (internal/app passes the
// watcher's own ignore rule), so ordering, coalescing and the memory caps stay
// testable without a filesystem.
package changefeed

import "time"

// Kind classifies what happened to the file, mirroring the watcher's file
// event kinds.
type Kind uint8

const (
	// Changed is an external write to an existing file.
	Changed Kind = iota
	// Created is a file that appeared externally.
	Created
	// Removed is a file deleted externally.
	Removed
)

// Icon returns the one-cell marker the feed list renders per kind, borrowing
// git's status vocabulary.
func (k Kind) Icon() string {
	switch k {
	case Created:
		return "+"
	case Removed:
		return "-"
	default:
		return "~"
	}
}

// Label names the kind for the feed list and prompts.
func (k Kind) Label() string {
	switch k {
	case Created:
		return "created"
	case Removed:
		return "removed"
	default:
		return "changed"
	}
}

// Origin records where an entry's pre-change content came from — which decides
// how much the revert action can promise.
type Origin uint8

const (
	// NoBefore means no pre-change content was available: a newly created
	// file had none, and a file IKE never opened or saved leaves no trace to
	// reconstruct one from.
	NoBefore Origin = iota
	// FromBuffer means the content is what the open, unmodified buffer held
	// when the external write landed — the exact bytes that were replaced.
	FromBuffer
	// FromSnapshot means the content is the newest local-history snapshot,
	// i.e. what IKE last wrote to that file. Anything the external process
	// changed since is included in the diff.
	FromSnapshot
	// Dropped means content was captured but released again to stay inside
	// the feed's memory budget. The entry still lists the change; it just
	// cannot diff or revert it anymore.
	Dropped
)

// Label names the origin for the panel's detail line.
func (o Origin) Label() string {
	switch o {
	case FromBuffer:
		return "open buffer"
	case FromSnapshot:
		return "local history"
	case Dropped:
		return "released (memory cap)"
	default:
		return "unavailable"
	}
}

// Entry is one file's external-change record. Count coalesces repeated writes
// to the same file — an agent rewriting a file five times is one row, not five.
type Entry struct {
	Path   string
	Time   time.Time // when the newest external event for Path landed
	Kind   Kind
	Count  int    // external events coalesced into this row (>= 1)
	Before string // pre-change content, empty unless Origin says otherwise
	Origin Origin
	// Source names the process the change is attributed to ("claude", a
	// formatter, a task) — best effort, empty whenever the caller cannot
	// tell. The feed never guesses one: an unattributed entry stays ungrouped
	// rather than having somebody else's write pinned on whatever process
	// happened to be running.
	Source string
}

// HasBefore reports whether the entry can still produce a diff and a revert.
func (e Entry) HasBefore() bool { return e.Origin == FromBuffer || e.Origin == FromSnapshot }

// Defaults for the feed's caps; Feed fields override them when positive.
const (
	// DefaultLimit bounds how many files the feed lists. Past it the oldest
	// rows drop out — the feed is a review queue, not an audit log.
	DefaultLimit = 200
	// DefaultMaxBytes bounds the pre-change content the feed retains across
	// all entries. Past it the oldest entries release their content (their
	// rows stay), so a batch touching large files cannot grow the session's
	// memory without bound.
	DefaultMaxBytes = 4 << 20
)

// Feed is the session-scoped list, newest first. The zero value is usable and
// selects the default caps.
type Feed struct {
	// Limit bounds the entry count; <=0 selects DefaultLimit.
	Limit int
	// MaxBytes bounds retained pre-change content; <=0 selects
	// DefaultMaxBytes.
	MaxBytes int
	// Disabled stops recording altogether (the feed_limit = 0 setting).
	Disabled bool
	// Ignore rejects noisy paths before they ever reach the list. nil admits
	// everything; internal/app wires the watcher's own ignore rule here so the
	// feed hides exactly what the watcher would never have walked into.
	Ignore func(path string) bool

	entries []Entry // newest first
	bytes   int     // retained Before bytes across entries
}

// New returns an empty feed with the default caps.
func New() *Feed { return &Feed{Limit: DefaultLimit, MaxBytes: DefaultMaxBytes} }

// SetLimit re-caps the entry count and trims the tail to it right away. Zero
// (or less) disables the feed and clears it — the setting is the user's off
// switch, so turning it off must not leave a stale list behind.
func (f *Feed) SetLimit(n int) {
	if f == nil {
		return
	}
	if n <= 0 {
		f.Disabled = true
		f.Clear()
		return
	}
	f.Disabled, f.Limit = false, n
	f.trim()
}

// limit is the effective entry cap; zero means recording is off.
func (f *Feed) limit() int {
	if f.Disabled {
		return 0
	}
	if f.Limit <= 0 {
		return DefaultLimit
	}
	return f.Limit
}

// maxBytes is the effective content budget.
func (f *Feed) maxBytes() int {
	if f.MaxBytes <= 0 {
		return DefaultMaxBytes
	}
	return f.MaxBytes
}

// Add records one external change and reports whether it landed. An entry for
// the same path is coalesced in place and moves to the front: its timestamp
// and kind advance, its count grows, and it keeps the pre-change content it
// was first recorded with — reverting an agent's fifth rewrite should restore
// what the file held before the first one, not before the fifth.
func (f *Feed) Add(e Entry) bool {
	if f == nil || e.Path == "" || f.limit() == 0 {
		return false
	}
	if f.Ignore != nil && f.Ignore(e.Path) {
		return false
	}
	if e.Count < 1 {
		e.Count = 1
	}
	if !e.HasBefore() {
		e.Before = "" // an origin without content must not retain any
	}
	for i, old := range f.entries {
		if old.Path != e.Path {
			continue
		}
		merged := old
		merged.Time = e.Time
		merged.Kind = mergeKinds(old.Kind, e.Kind)
		merged.Count = old.Count + e.Count
		if merged.Source == "" {
			// The first event landed with nothing to attribute it to; a later
			// one could tell. Adopt that source — some attribution beats none.
			// An existing one is never overwritten: a row is one file, and the
			// process that first touched it is the one the group is about.
			merged.Source = e.Source
		}
		if !old.HasBefore() && e.HasBefore() {
			// The first event caught the file with nothing to compare
			// against (never opened, never saved); a later one found a
			// snapshot. Adopt it — some pre-change content beats none.
			merged.Before, merged.Origin = e.Before, e.Origin
			f.bytes += len(e.Before)
		}
		f.entries = append(f.entries[:i], f.entries[i+1:]...)
		f.entries = append([]Entry{merged}, f.entries...)
		f.enforceBytes()
		return true
	}
	f.bytes += len(e.Before)
	f.entries = append([]Entry{e}, f.entries...)
	f.trim()
	f.enforceBytes()
	return true
}

// mergeKinds folds a new kind onto the recorded one, matching the watcher's
// coalescing: a removal wins, and a file created this session stays "created"
// however often it is rewritten afterwards.
func mergeKinds(old, next Kind) Kind {
	switch {
	case next == Removed || old == Removed:
		return Removed
	case old == Created && next == Changed:
		return Created
	}
	return next
}

// trim drops the oldest entries past the count cap.
func (f *Feed) trim() {
	for len(f.entries) > f.limit() {
		last := f.entries[len(f.entries)-1]
		f.bytes -= len(last.Before)
		f.entries = f.entries[:len(f.entries)-1]
	}
}

// enforceBytes releases pre-change content from the oldest entries until the
// retained total is back inside the budget. The rows survive — a listed change
// the user can no longer diff is still worth knowing about — and their origin
// records why the content is gone.
func (f *Feed) enforceBytes() {
	budget := f.maxBytes()
	for i := len(f.entries) - 1; i >= 0 && f.bytes > budget; i-- {
		if !f.entries[i].HasBefore() {
			continue
		}
		f.bytes -= len(f.entries[i].Before)
		f.entries[i].Before, f.entries[i].Origin = "", Dropped
	}
}

// Entries returns the feed newest-first as a copy, so a caller iterating the
// list cannot be surprised by a watcher event landing mid-render.
func (f *Feed) Entries() []Entry {
	if f == nil {
		return nil
	}
	out := make([]Entry, len(f.entries))
	copy(out, f.entries)
	return out
}

// Group is one titled section of the feed: the entries attributed to a single
// originating process, newest first. An empty Source is the unknown bucket —
// what nothing could be attributed to, which stays ungrouped.
type Group struct {
	Source  string
	Entries []Entry
}

// Attributed reports whether the group names a process, i.e. whether it is a
// real group rather than the unknown bucket.
func (g Group) Attributed() bool { return g.Source != "" }

// Groups splits the feed by originating process, newest first inside each
// group. Attributed groups lead, ordered by their newest entry, and the
// unknown bucket comes last: attribution is the exception, so the sections
// that can be acted on as a unit come first and everything the feed could not
// place stays together at the end, in plain feed order.
func (f *Feed) Groups() []Group {
	if f == nil || len(f.entries) == 0 {
		return nil
	}
	var out []Group
	at := map[string]int{} // source -> index in out
	var unknown []Entry
	for _, e := range f.entries {
		if e.Source == "" {
			unknown = append(unknown, e)
			continue
		}
		if i, ok := at[e.Source]; ok {
			out[i].Entries = append(out[i].Entries, e)
			continue
		}
		at[e.Source] = len(out)
		out = append(out, Group{Source: e.Source, Entries: []Entry{e}})
	}
	if len(unknown) > 0 {
		out = append(out, Group{Entries: unknown})
	}
	return out
}

// Attributed reports whether any entry carries a source, i.e. whether grouping
// has anything to show. A feed nothing could be attributed to renders as the
// plain list it has always been.
func (f *Feed) Attributed() bool {
	if f == nil {
		return false
	}
	for _, e := range f.entries {
		if e.Source != "" {
			return true
		}
	}
	return false
}

// Len reports how many files the feed lists.
func (f *Feed) Len() int {
	if f == nil {
		return 0
	}
	return len(f.entries)
}

// Bytes reports the retained pre-change content in bytes.
func (f *Feed) Bytes() int {
	if f == nil {
		return 0
	}
	return f.bytes
}

// Get returns path's entry and whether the feed holds one.
func (f *Feed) Get(path string) (Entry, bool) {
	if f == nil {
		return Entry{}, false
	}
	for _, e := range f.entries {
		if e.Path == path {
			return e, true
		}
	}
	return Entry{}, false
}

// Remove drops path's entry (the panel's dismiss action) and reports whether
// there was one.
func (f *Feed) Remove(path string) bool {
	if f == nil {
		return false
	}
	for i, e := range f.entries {
		if e.Path != path {
			continue
		}
		f.bytes -= len(e.Before)
		f.entries = append(f.entries[:i], f.entries[i+1:]...)
		return true
	}
	return false
}

// Clear empties the feed.
func (f *Feed) Clear() {
	if f == nil {
		return
	}
	f.entries, f.bytes = nil, 0
}
