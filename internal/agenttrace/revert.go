package agenttrace

import (
	"errors"
	"strconv"
	"strings"
)

// revert.go undoes one reconstructed change against the file as it is now
// (#2877): V on a change the change feed cannot revert (no link, or no
// pre-change snapshot) falls back to the transcript's diff. Every hunk is
// turned around — its after lines are looked up in the current content and
// replaced by its before lines — so later, unrelated edits to the file
// survive. A hunk whose after lines are gone (or appear more than once) is a
// conflict: nothing is reverted. Apply (#3009) is the same splice run
// forward, for the complexity agent's diff.

// ErrNoSnapshot means the diff does not know what the file held before.
var ErrNoSnapshot = errors.New("no snapshot to revert to")

// ErrCreated means the change created the file: there is no earlier version.
var ErrCreated = errors.New("the change created the file — there is no previous version to restore")

// ErrConflict means the file no longer holds the change's content.
var ErrConflict = errors.New("the file no longer contains the change — it was edited since")

// ErrAmbiguous means the change's content occurs more than once in the file.
var ErrAmbiguous = errors.New("the change's content occurs more than once in the file — cannot tell which to revert")

// ErrReverted means the file already holds the content from before the change.
var ErrReverted = errors.New("the file already holds the content from before the change")

// Revertible reports why d cannot be reverted at all, nil when it may be.
func Revertible(d ChangeDiff) error {
	switch d.Source {
	case DiffCreate:
		return ErrCreated
	case DiffNone, DiffAfterOnly:
		return ErrNoSnapshot
	}
	if len(d.Hunks) == 0 && !(d.HasBefore && d.HasAfter) {
		return ErrNoSnapshot
	}
	return nil
}

// Revert returns now with d undone. now is the current content in the
// buffer's form (a trailing newline or not, it is kept as found).
func Revert(d ChangeDiff, now string) (string, error) {
	if err := Revertible(d); err != nil {
		return "", err
	}
	if len(d.Hunks) == 0 {
		// Whole contents only: the file must still be exactly the after.
		switch strings.TrimSuffix(now, "\n") {
		case strings.TrimSuffix(d.After, "\n"):
			return keepNewline(d.Before, now), nil
		case strings.TrimSuffix(d.Before, "\n"):
			return "", ErrReverted
		}
		return "", ErrConflict
	}
	out, err := spliceHunks(now, d.Hunks, false)
	if err != nil {
		var he *HunkError
		if errors.As(err, &he) {
			err = he.Err // V names no hunk: the change is one unit there
		}
		if err == ErrApplied {
			err = ErrReverted
		}
		return "", err
	}
	return out, nil
}

// ErrApplied means the content already holds the hunk's target side.
var ErrApplied = errors.New("the file already holds the change")

// HunkError is a hunk that did not apply: Index is 0-based, Total the
// hunk count, Err the reason (ErrConflict, ErrAmbiguous, ErrApplied).
type HunkError struct {
	Index, Total int
	Err          error
}

func (e *HunkError) Error() string {
	return "hunk " + strconv.Itoa(e.Index+1) + " of " + strconv.Itoa(e.Total) + ": " + e.Err.Error()
}

func (e *HunkError) Unwrap() error { return e.Err }

// Apply returns now with hunks applied forward (#3009) — the complexity
// agent's diff: each hunk's before lines (context and removed) must occur
// exactly once in now and are replaced by its after lines (context and
// added). Any hunk that does not match fails the whole apply with a
// *HunkError naming it; nothing is applied then.
func Apply(hunks []DiffHunk, now string) (string, error) {
	if len(hunks) == 0 {
		return "", ErrNoSnapshot
	}
	return spliceHunks(now, hunks, true)
}

// spliceHunks replaces, for every hunk, one side by the other in text:
// forward finds the before lines and puts the after lines, backward (the
// revert) the other way round. A miss whose replacement is already present
// reports ErrApplied.
func spliceHunks(text string, hunks []DiffHunk, forward bool) (string, error) {
	lines := strings.Split(text, "\n")
	// Last hunk first: an earlier hunk's replacement never shifts the
	// positions a later one was found at.
	for i := len(hunks) - 1; i >= 0; i-- {
		from, to := hunkSides(hunks[i])
		if !forward {
			from, to = to, from
		}
		fail := func(err error) (string, error) {
			return "", &HunkError{Index: i, Total: len(hunks), Err: err}
		}
		if len(from) == 0 {
			// A pure insertion / deletion without context: nothing marks
			// where the lines go.
			return fail(ErrConflict)
		}
		at, err := findBlock(lines, from)
		if err == ErrConflict && len(to) > 0 {
			if _, again := findBlock(lines, to); again == nil {
				err = ErrApplied
			}
		}
		if err != nil {
			return fail(err)
		}
		next := make([]string, 0, len(lines)-len(from)+len(to))
		next = append(next, lines[:at]...)
		next = append(next, to...)
		next = append(next, lines[at+len(from):]...)
		lines = next
	}
	return strings.Join(lines, "\n"), nil
}

// hunkSides splits a hunk into its before lines (context and removed) and
// its after lines (context and added).
func hunkSides(h DiffHunk) (before, after []string) {
	for _, l := range h.Lines {
		if l == "" {
			continue
		}
		switch l[0] {
		case ' ':
			before = append(before, l[1:])
			after = append(after, l[1:])
		case '-':
			before = append(before, l[1:])
		case '+':
			after = append(after, l[1:])
		}
	}
	return before, after
}

// findBlock returns the one index where block starts in lines.
func findBlock(lines, block []string) (int, error) {
	at := -1
	for i := 0; i+len(block) <= len(lines); i++ {
		if equalLines(lines[i:i+len(block)], block) {
			if at >= 0 {
				return 0, ErrAmbiguous
			}
			at = i
		}
	}
	if at < 0 {
		return 0, ErrConflict
	}
	return at, nil
}

func equalLines(a, b []string) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// keepNewline gives text the trailing-newline form of like.
func keepNewline(text, like string) string {
	text = strings.TrimSuffix(text, "\n")
	if strings.HasSuffix(like, "\n") {
		text += "\n"
	}
	return text
}
