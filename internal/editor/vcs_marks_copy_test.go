package editor

import (
	"testing"

	"ike/internal/vcs"
)

// The marks accessors of #2541: GitMarks hands out a copy the recompute can
// read off-loop, HasGitMarks says whether a clearing message has anything to
// clear.
func TestGitMarksAccessorsCopy(t *testing.T) {
	m, _ := loaded(t, "a\nb\n")
	if m.HasGitMarks() || m.GitMarks() != nil {
		t.Fatal("a fresh view shows no marks")
	}
	m, _ = m.Update(vcs.MarksMsg{Path: m.path, Marks: map[int]vcs.LineMark{1: vcs.LineChanged}})
	if !m.HasGitMarks() {
		t.Fatal("marks applied must report as showing")
	}
	got := m.GitMarks()
	got[0] = vcs.LineAdded // mutate the copy…
	if _, ok := m.gitMarks[0]; ok || len(m.gitMarks) != 1 {
		t.Fatalf("…the view must be untouched: %v", m.gitMarks)
	}
}
