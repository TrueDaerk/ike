package undotree

import (
	"strings"
	"testing"
	"time"

	"ike/internal/editor/history"
)

// stubSource is a Source over a fixed map of state contents.
type stubSource struct {
	current string
	states  map[int]string
	calls   int
}

func (s *stubSource) Text() string { return s.current }

func (s *stubSource) HistoryContentAt(seq int) (string, bool) {
	s.calls++
	c, ok := s.states[seq]
	return c, ok
}

func previewFixture() *stubSource {
	return &stubSource{
		current: "alpha\nbeta\nX",
		states: map[int]string{
			0: "alpha\nbeta",
			1: "alpha\nbeta\nc",
			2: "alpha\nbeta\nc\nd",
			3: "alpha\nbeta\nX",
		},
	}
}

func TestPreviewShowsDiffOfSelectedNode(t *testing.T) {
	m := New()
	m.SetSize(100, 40)
	src := previewFixture()
	m.SetSource(src)
	m.Open(nodesFixture())

	// The selection starts on the current state (seq 3) — nothing to apply.
	v := m.View()
	if !strings.Contains(v, "diff: current → state 3") {
		t.Fatalf("preview header missing for the current state:\n%s", v)
	}
	if !strings.Contains(v, "identical to the current buffer") {
		t.Errorf("current state should preview as identical:\n%s", v)
	}

	// Moving to the abandoned branch (seq 2) shows what jumping there does.
	m.Update(key("j"))
	v = m.View()
	if !strings.Contains(v, "diff: current → state 2") {
		t.Fatalf("preview should follow the selection:\n%s", v)
	}
	if !strings.Contains(v, "-X") || !strings.Contains(v, "+c") || !strings.Contains(v, "+d") {
		t.Errorf("preview should show -X and +c/+d:\n%s", v)
	}
}

func TestPreviewCachesPerSeqAndInvalidatesOnJump(t *testing.T) {
	m := New()
	m.SetSize(100, 40)
	src := previewFixture()
	m.SetSource(src)
	m.Open(nodesFixture())

	m.View()
	m.View()
	if src.calls != 1 {
		t.Fatalf("repeated renders of one selection reconstructed %d times, want 1", src.calls)
	}
	m.SetNodes(nodesFixture()) // a jump happened: the diffs are stale
	m.View()
	if src.calls != 2 {
		t.Errorf("SetNodes must drop the cache, calls = %d, want 2", src.calls)
	}
}

func TestPreviewReportsPrunedState(t *testing.T) {
	m := New()
	m.SetSize(100, 40)
	m.SetSource(&stubSource{current: "a", states: map[int]string{0: "a"}})
	m.Open(nodesFixture())
	if v := m.View(); !strings.Contains(v, "no longer available") {
		t.Errorf("a pruned state should say so:\n%s", v)
	}
}

func TestPreviewTruncatesToTheWindow(t *testing.T) {
	long := make([]string, 60)
	for i := range long {
		long[i] = "line"
	}
	m := New()
	m.SetSize(100, 40)
	m.SetSource(&stubSource{
		current: "",
		states:  map[int]string{3: strings.Join(long, "\n")},
	})
	m.Open(nodesFixture())
	v := m.View()
	if !strings.Contains(v, "more lines") {
		t.Errorf("a long diff should report the truncated remainder:\n%s", v)
	}
}

func TestNoSourceRendersNoPreview(t *testing.T) {
	m := New()
	m.SetSize(100, 40)
	m.Open(nodesFixture())
	if strings.Contains(m.View(), "diff: current") {
		t.Error("without a source there is no preview")
	}
}

func TestRelativeTimestampsPerRow(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	nodes := []history.NodeInfo{
		{Seq: 0, Parent: -1},
		{Seq: 1, Parent: 0, At: now.Add(-3 * time.Hour), Preview: `+"b"`},
		{Seq: 2, Parent: 1, At: now.Add(-5 * time.Minute), Preview: `+"c"`},
		{Seq: 3, Parent: 2, At: now.Add(-2 * time.Second), Preview: `+"d"`, Current: true},
	}
	m := New()
	m.now = func() time.Time { return now }
	m.SetSize(100, 40)
	m.Open(nodes)
	v := m.View()
	for _, want := range []string{"just now", "5m ago", "3h ago"} {
		if !strings.Contains(v, want) {
			t.Errorf("view is missing the relative age %q:\n%s", want, v)
		}
	}
}

func TestRelAgeUnits(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		at   time.Time
		want string
	}{
		{time.Time{}, ""},
		{now, "just now"},
		{now.Add(-90 * time.Second), "1m ago"},
		{now.Add(-2 * time.Hour), "2h ago"},
		{now.Add(-50 * time.Hour), "2d ago"},
	}
	for _, c := range cases {
		if got := relAge(c.at, now); got != c.want {
			t.Errorf("relAge(%v) = %q, want %q", c.at, got, c.want)
		}
	}
}
