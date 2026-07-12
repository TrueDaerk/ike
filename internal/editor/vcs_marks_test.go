package editor

import (
	"testing"

	"ike/internal/vcs"
)

// Gutter diff markers (Roadmap 0320, #464): the editor stores path-matched
// marks, ignores foreign paths, and clears on a nil map.
func TestGitMarksStoredAndCleared(t *testing.T) {
	m, _ := loaded(t, "a\nb\n")
	m, _ = m.Update(vcs.MarksMsg{Path: m.path, Marks: map[int]vcs.LineMark{1: vcs.LineChanged}})
	if m.gitMarks[1] != vcs.LineChanged {
		t.Fatalf("marks not stored: %v", m.gitMarks)
	}
	m, _ = m.Update(vcs.MarksMsg{Path: "/other.go", Marks: map[int]vcs.LineMark{0: vcs.LineAdded}})
	if m.gitMarks[1] != vcs.LineChanged || len(m.gitMarks) != 1 {
		t.Fatalf("foreign-path marks applied: %v", m.gitMarks)
	}
	m, _ = m.Update(vcs.MarksMsg{Path: m.path})
	if m.gitMarks != nil {
		t.Fatalf("nil marks must clear: %v", m.gitMarks)
	}
}

func TestGitMarkColors(t *testing.T) {
	m, _ := loaded(t, "x\n")
	pal := m.theme()
	for mk, want := range map[vcs.LineMark]any{
		vcs.LineAdded:   pal.VCSAdded,
		vcs.LineChanged: pal.VCSModified,
		vcs.LineDeleted: pal.VCSDeleted,
	} {
		if got := m.gitMarkColor(mk); got != want {
			t.Errorf("mark %v color = %v, want %v", mk, got, want)
		}
	}
}
