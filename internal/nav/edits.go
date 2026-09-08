package nav

// edits.go is the edit-location ring (#2545): the positions the caret was at
// when the buffer last changed, newest first, deduped by line proximity and
// capped. nav.lastEdit walks it back (JetBrains "Last Edit Location") and
// nav.recentLocations lists it beside the jump history. Like History it is
// pure data; the root model records into it from the editor emitter.

// Location is a ring entry: a caret Position plus the project Root it belongs
// to, so a cross-project entry can switch the project before the open.
type Location struct {
	Position
	Root string
}

// maxEdits bounds the ring; the oldest edit locations fall off.
const maxEdits = 50

// editProximity is the dedupe window: an edit within this many lines of an
// existing entry in the same file replaces that entry instead of adding one,
// so typing across a few adjacent lines reads as one place.
const editProximity = 3

// EditRing holds the edit locations, oldest first. The zero value is ready.
type EditRing struct {
	entries []Location
	// walk is the index (from the newest, 0-based) of the entry the last
	// Step landed on; walking says whether a walk is in progress at all. A
	// fresh edit ends the walk, so the next nav.lastEdit starts over from
	// the newest entry.
	walk    int
	walking bool
}

// Record remembers loc as the newest edit location. Pathless buffers are
// skipped, an entry of the same file within editProximity lines is replaced
// (moved to the newest slot), and the ring is capped at maxEdits. Recording
// ends any walk in progress.
func (r *EditRing) Record(loc Location) {
	if loc.Path == "" {
		return
	}
	r.walking = false
	kept := r.entries[:0]
	for _, e := range r.entries {
		if !e.closeTo(loc) {
			kept = append(kept, e)
		}
	}
	r.entries = append(kept, loc)
	if len(r.entries) > maxEdits {
		r.entries = r.entries[len(r.entries)-maxEdits:]
	}
}

// closeTo reports whether two locations are the same file within the
// proximity window.
func (l Location) closeTo(o Location) bool {
	if l.Path != o.Path {
		return false
	}
	d := l.Line - o.Line
	return d >= -editProximity && d <= editProximity
}

// Recent returns the edit locations newest first (a copy).
func (r *EditRing) Recent() []Location {
	out := make([]Location, 0, len(r.entries))
	for i := len(r.entries) - 1; i >= 0; i-- {
		out = append(out, r.entries[i])
	}
	return out
}

// Len reports the number of remembered edit locations.
func (r *EditRing) Len() int { return len(r.entries) }

// Step yields the next target of nav.lastEdit: the newest entry, or — when
// the caret still sits where the previous Step landed — the one before it,
// so repeating the command walks back through the ring. Entries on the
// caret's own line are skipped (invoking the command at the edit site goes
// to the previous edit). ok is false when the ring is exhausted; the walk
// position is kept so a further repeat stays exhausted until an edit or a
// move elsewhere restarts it.
func (r *EditRing) Step(current Position) (Location, bool) {
	start := 0
	if r.walking && r.walk < len(r.entries) && r.at(r.walk).near(current) {
		start = r.walk + 1
	}
	for i := start; i < len(r.entries); i++ {
		loc := r.at(i)
		if loc.near(current) {
			continue
		}
		r.walking, r.walk = true, i
		return loc, true
	}
	return Location{}, false
}

// at returns the i-th newest entry.
func (r *EditRing) at(i int) Location { return r.entries[len(r.entries)-1-i] }

// Recent lists the jump history's positions newest first: the back stack
// (most recent departure first) followed by the forward tail. It feeds the
// recent-locations picker (#2545) beside the edit ring.
func (h *History) Recent() []Position {
	out := make([]Position, 0, len(h.back)+len(h.forward))
	for i := len(h.back) - 1; i >= 0; i-- {
		out = append(out, h.back[i])
	}
	for i := len(h.forward) - 1; i >= 0; i-- {
		out = append(out, h.forward[i])
	}
	return out
}
