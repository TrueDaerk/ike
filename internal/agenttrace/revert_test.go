package agenttrace

import (
	"errors"
	"testing"
)

func TestRevert(t *testing.T) {
	hunk := DiffHunk{OldStart: 2, OldLines: 3, NewStart: 2, NewLines: 3, Lines: []string{" b", "-c", "+C", " d"}}
	edit := ChangeDiff{Source: DiffStrings, Hunks: []DiffHunk{hunk}}
	cases := []struct {
		name string
		d    ChangeDiff
		now  string
		want string
		err  error
	}{
		{"hunk", edit, "a\nb\nC\nd\ne\n", "a\nb\nc\nd\ne\n", nil},
		{"later edits survive", edit, "A\nb\nC\nd\nE", "A\nb\nc\nd\nE", nil},
		{"conflict", edit, "a\nb\nX\nd\n", "", ErrConflict},
		{"already reverted", edit, "a\nb\nc\nd\n", "", ErrReverted},
		{"ambiguous", edit, "b\nC\nd\nb\nC\nd", "", ErrAmbiguous},
		{"two hunks", ChangeDiff{Source: DiffPatch, Hunks: []DiffHunk{
			{Lines: []string{" a", "+x"}}, {Lines: []string{" y", "-z", "+Z"}},
		}}, "a\nx\nm\ny\nZ\n", "a\nm\ny\nz\n", nil},
		{"bare deletion", ChangeDiff{Source: DiffStrings, Hunks: []DiffHunk{{Lines: []string{"-gone"}}}}, "a\n", "", ErrConflict},
		{"whole contents", ChangeDiff{Source: DiffSession, Before: "old\n", HasBefore: true, After: "new\n", HasAfter: true}, "new", "old", nil},
		{"whole contents moved on", ChangeDiff{Source: DiffSession, Before: "old\n", HasBefore: true, After: "new\n", HasAfter: true}, "newer", "", ErrConflict},
		{"create", ChangeDiff{Source: DiffCreate, HasBefore: true, After: "x\n", HasAfter: true, Hunks: []DiffHunk{{Lines: []string{"+x"}}}}, "x\n", "", ErrCreated},
		{"after only", ChangeDiff{Source: DiffAfterOnly, After: "x\n", HasAfter: true}, "x\n", "", ErrNoSnapshot},
		{"nothing", ChangeDiff{}, "x\n", "", ErrNoSnapshot},
	}
	for _, c := range cases {
		got, err := Revert(c.d, c.now)
		if !errors.Is(err, c.err) || got != c.want {
			t.Errorf("%s: Revert = %q, %v; want %q, %v", c.name, got, err, c.want, c.err)
		}
	}
}

// TestApply covers the forward splice the complexity agent's diff takes
// (#3009): exact-once before lines, last hunk first, a miss names its hunk.
func TestApply(t *testing.T) {
	one := []DiffHunk{{Lines: []string{" b", "-c", "+C", " d"}}}
	two := []DiffHunk{{Lines: []string{" a", "+x"}}, {Lines: []string{" y", "-z", "+Z"}}}
	cases := []struct {
		name   string
		hunks  []DiffHunk
		now    string
		want   string
		err    error
		failAt int // 1-based hunk a *HunkError names, 0 for none
	}{
		{"hunk", one, "a\nb\nc\nd\ne\n", "a\nb\nC\nd\ne\n", nil, 0},
		{"two hunks, last first", two, "a\nm\ny\nz\n", "a\nx\nm\ny\nZ\n", nil, 0},
		{"no match", one, "a\nb\nX\nd\n", "", ErrNoMatch, 1},
		{"second hunk misses", two, "a\nm\ny\nq\n", "", ErrNoMatch, 2},
		{"already applied", one, "a\nb\nC\nd\n", "", ErrApplied, 1},
		{"ambiguous", one, "b\nc\nd\nb\nc\nd", "", ErrAmbiguous, 1},
		{"bare insertion", []DiffHunk{{Lines: []string{"+new"}}}, "a\n", "", ErrNoMatch, 1},
		{"no hunks", nil, "a\n", "", ErrNoSnapshot, 0},
	}
	for _, c := range cases {
		got, err := Apply(c.hunks, c.now)
		if !errors.Is(err, c.err) || got != c.want {
			t.Errorf("%s: Apply = %q, %v; want %q, %v", c.name, got, err, c.want, c.err)
			continue
		}
		var he *HunkError
		if c.failAt > 0 && (!errors.As(err, &he) || he.Index+1 != c.failAt || he.Total != len(c.hunks)) {
			t.Errorf("%s: error %v must name hunk %d of %d", c.name, err, c.failAt, len(c.hunks))
		}
	}
}
