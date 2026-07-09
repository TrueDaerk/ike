package backup

import (
	"sort"
	"time"
)

// Debouncer tracks a per-key deadline so a dirty buffer is snapshotted only after
// it has been quiet for the debounce interval. It holds no timer: the caller
// supplies "now" (real clock in the app, a fake clock in tests), Marks a key on
// every edit, and asks which keys are Due. Only dirty buffers are ever Marked, so
// a clean editor produces no work — zero writes when nothing is dirty.
type Debouncer struct {
	d        time.Duration
	deadline map[string]time.Time
}

// NewDebouncer returns a Debouncer with the given quiet interval.
func NewDebouncer(d time.Duration) *Debouncer {
	return &Debouncer{d: d, deadline: map[string]time.Time{}}
}

// Mark (re)arms key's deadline to now + interval, so a burst of edits collapses
// into one pending snapshot that fires after the edits stop.
func (b *Debouncer) Mark(key string, now time.Time) {
	b.deadline[key] = now.Add(b.d)
}

// Cancel drops any pending deadline for key (its buffer was saved or closed).
func (b *Debouncer) Cancel(key string) { delete(b.deadline, key) }

// Due returns the keys whose deadline has passed at now, in sorted order, and
// clears them. Callers snapshot each returned key's current text.
func (b *Debouncer) Due(now time.Time) []string {
	var due []string
	for k, t := range b.deadline {
		if !now.Before(t) {
			due = append(due, k)
		}
	}
	sort.Strings(due)
	for _, k := range due {
		delete(b.deadline, k)
	}
	return due
}

// Pending reports how many keys are waiting.
func (b *Debouncer) Pending() int { return len(b.deadline) }

// Next returns the earliest pending deadline, so the caller can schedule a wake
// exactly when the next snapshot is due. ok is false when nothing is pending.
func (b *Debouncer) Next() (time.Time, bool) {
	var earliest time.Time
	found := false
	for _, t := range b.deadline {
		if !found || t.Before(earliest) {
			earliest, found = t, true
		}
	}
	return earliest, found
}
