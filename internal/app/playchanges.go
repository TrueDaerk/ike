package app

import (
	"strings"

	"ike/internal/vcs"
)

// playchanges.go marks the result lines a run changed (#2787). After an edit
// to the program the result re-renders as a whole, and on a large result the
// eye cannot find what the edit did; so every run diffs its text line by line
// against the result it replaces and the gutter carries a bar on each added or
// changed line (a thin top bar on the line after a deletion) in the Info tone.
// The marks hold until the next result replaces them — a failed run keeps the
// previous result and with it its marks — and ctrl+l, which clears the result,
// clears them too. The first run, and the first after a clear, has nothing to
// compare against and marks nothing.
//
// The diff runs off the event loop in the same goroutine as the evaluation,
// and only while both results stay within playDiffMaxLines: past that the
// marks are skipped, so a huge result costs no diff at all.

// playDiffMaxLines is the per-side line budget of the change diff.
const playDiffMaxLines = 5000

// playChangeMarks diffs the previous result text against the new one and
// returns the new result's per-line marks, nil when nothing changed or either
// side is over the budget.
func playChangeMarks(prev, next string) map[int]vcs.LineMark {
	if prev == next || overLineBudget(prev) || overLineBudget(next) {
		return nil
	}
	return vcs.LineMarks(prev, next)
}

// overLineBudget reports whether s holds more than playDiffMaxLines lines,
// without counting past the budget.
func overLineBudget(s string) bool {
	n := 0
	for {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			return false
		}
		if n++; n >= playDiffMaxLines {
			return true
		}
		s = s[i+1:]
	}
}
