package diff

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// reviewModel starts review mode over three files and lands the first one,
// returning the model and the contents the fake owner serves per index.
func reviewModel(t *testing.T) (*Model, map[int][2]string) {
	t.Helper()
	m := New("diff", "", "", "", nil)
	m.SetSize(80, 12)
	files := []ReviewFile{
		{Path: "a/one.go", Abs: "/repo/a/one.go", Code: "M"},
		{Path: "b/two.go", Abs: "/repo/b/two.go", Code: "AM"},
		{Path: "c/three.go", Abs: "/repo/c/three.go", Code: "D"},
	}
	contents := map[int][2]string{
		0: {"a\nb\nc\nd\ne\nf\ng\nh\n", "a\nB\nc\nd\ne\nf\ng\nH\n"}, // two hunks
		1: {"x\ny\n", "x\nY\n"},                                     // one hunk
		2: {"gone\n", ""},                                           // one hunk (deleted)
	}
	cmd := m.StartReview(files, 0)
	load := loadMsg(t, cmd)
	if load.Index != 0 || load.Key != "diff" {
		t.Fatalf("start request = %+v, want index 0 on key diff", load)
	}
	m.ShowReviewFile(0, contents[0][0], contents[0][1])
	return &m, contents
}

// loadMsg runs cmd and asserts it yields a ReviewLoadMsg.
func loadMsg(t *testing.T, cmd tea.Cmd) ReviewLoadMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a review load request, got no command")
	}
	msg, ok := cmd().(ReviewLoadMsg)
	if !ok {
		t.Fatalf("command yielded %T, want ReviewLoadMsg", msg)
	}
	return msg
}

func TestReviewLandsWithPathLabelsAndPosition(t *testing.T) {
	m, _ := reviewModel(t)
	l, r := m.SideLabels()
	if l != "a/one.go @ HEAD" || r != "a/one.go (working tree)" {
		t.Fatalf("side labels = %q / %q", l, r)
	}
	if m.RightPath() != "/repo/a/one.go" {
		t.Fatalf("right path = %q, want the working-tree file for enter", m.RightPath())
	}
	if lr, rr := m.Revs(); lr != "HEAD" || rr != "" {
		t.Fatalf("revs = %q/%q", lr, rr)
	}
	if !m.Editable() {
		t.Fatal("a working-tree side must stay editable")
	}
	if m.CurrentHunk() != 0 {
		t.Fatalf("a forward landing starts on hunk 1, got cur=%d", m.CurrentHunk())
	}
	v := plainView(m)
	if !strings.Contains(v, "file 1/3 · hunk 1/2") {
		t.Fatalf("footer lacks the file/hunk position:\n%s", v)
	}
}

func TestReviewHunkStepCrossesFileBoundaries(t *testing.T) {
	m, contents := reviewModel(t)
	// Hunk 1 → hunk 2 stays in the file.
	if cmd := m.StepHunk(1); cmd != nil {
		t.Fatal("a step inside the file must not request another file")
	}
	if m.CurrentHunk() != 1 {
		t.Fatalf("cur = %d, want 1", m.CurrentHunk())
	}
	// Past the last hunk: the next file, landing on its first hunk.
	load := loadMsg(t, m.StepHunk(1))
	if load.Index != 1 {
		t.Fatalf("forward boundary requested index %d, want 1", load.Index)
	}
	if m.ReviewPending() != 1 {
		t.Fatalf("pending = %d", m.ReviewPending())
	}
	m.ShowReviewFile(1, contents[1][0], contents[1][1])
	if m.ReviewIndex() != 1 || m.CurrentHunk() != 0 {
		t.Fatalf("after forward landing: file=%d cur=%d", m.ReviewIndex(), m.CurrentHunk())
	}
	// Back past the first hunk: the previous file, on its last hunk.
	load = loadMsg(t, m.StepHunk(-1))
	if load.Index != 0 {
		t.Fatalf("backward boundary requested index %d, want 0", load.Index)
	}
	m.ShowReviewFile(0, contents[0][0], contents[0][1])
	if m.ReviewIndex() != 0 || m.CurrentHunk() != 1 {
		t.Fatalf("after backward landing: file=%d cur=%d, want file 0 on its last hunk", m.ReviewIndex(), m.CurrentHunk())
	}
	// The list ends stop: backward from the first hunk of the first file.
	if cmd := m.StepHunk(-1); cmd != nil {
		t.Fatal("stepping back from the first hunk of the first file must stop")
	}
	if m.CurrentHunk() != 0 {
		t.Fatalf("cur = %d, want clamped at 0", m.CurrentHunk())
	}
}

func TestReviewNKeysCrossBoundariesToo(t *testing.T) {
	m, _ := reviewModel(t)
	m.SetFocused(true)
	m.Update(key("n"))
	cmd := m.Update(key("n"))
	if load := loadMsg(t, cmd); load.Index != 1 {
		t.Fatalf("n past the last hunk requested %d, want 1", load.Index)
	}
}

func TestSingleFileDiffStillClampsAtTheEnds(t *testing.T) {
	m := testModel(t, "a\nb\nc\nd\ne\nf\ng\nh\n", "a\nB\nc\nd\ne\nf\ng\nH\n")
	m.StepHunk(1)
	m.StepHunk(1)
	if cmd := m.StepHunk(1); cmd != nil {
		t.Fatal("a single-file diff must not emit a load request")
	}
	if m.CurrentHunk() != 1 {
		t.Fatalf("cur = %d, want the last hunk", m.CurrentHunk())
	}
	if m.Reviewing() {
		t.Fatal("a plain diff is not in review mode")
	}
	if strings.Contains(plainView(m), "file ") {
		t.Fatal("a plain diff's footer carries no file position")
	}
}

func TestReviewFileStepsAndEnds(t *testing.T) {
	m, contents := reviewModel(t)
	load := loadMsg(t, m.StepReviewFile(1))
	if load.Index != 1 {
		t.Fatalf("next file = %d", load.Index)
	}
	m.ShowReviewFile(1, contents[1][0], contents[1][1])
	load = loadMsg(t, m.StepReviewFile(1))
	if load.Index != 2 {
		t.Fatalf("next file = %d", load.Index)
	}
	m.ShowReviewFile(2, contents[2][0], contents[2][1])
	if cmd := m.StepReviewFile(1); cmd != nil {
		t.Fatal("the last file has no next")
	}
	load = loadMsg(t, m.StepReviewFile(-1))
	if load.Index != 1 {
		t.Fatalf("prev file = %d", load.Index)
	}
	m.ShowReviewFile(1, contents[1][0], contents[1][1])
	if m.CurrentHunk() != 0 {
		t.Fatalf("a backward file step lands on the last hunk (the only one here), got %d", m.CurrentHunk())
	}
}

func TestReviewSkipsBinaryAndOversizedFiles(t *testing.T) {
	m, contents := reviewModel(t)
	m.ReviewFiles()[1].Skip = "binary"
	// Stepping forward from file 0 passes the skipped file.
	load := loadMsg(t, m.StepReviewFile(1))
	if load.Index != 2 {
		t.Fatalf("skip: requested %d, want 2", load.Index)
	}
	m.ShowReviewFile(2, contents[2][0], contents[2][1])
	// And back again.
	load = loadMsg(t, m.StepReviewFile(-1))
	if load.Index != 0 {
		t.Fatalf("skip back: requested %d, want 0", load.Index)
	}
	m.ShowReviewFile(0, contents[0][0], contents[0][1])
	// A file the owner only finds unreadable while loading: the step goes on.
	m.ReviewFiles()[2].Skip = ""
	load = loadMsg(t, m.StepReviewFile(1))
	if load.Index != 2 {
		t.Fatalf("requested %d, want 2 (file 1 is still skipped)", load.Index)
	}
	if cmd := m.ReviewSkip(2, "too large"); cmd != nil {
		t.Fatal("no file follows the last one; the placeholder must show instead")
	}
	if m.ReviewIndex() != 2 || m.HunkCount() != 0 {
		t.Fatalf("placeholder: file=%d hunks=%d", m.ReviewIndex(), m.HunkCount())
	}
	if v := plainView(m); !strings.Contains(v, "c/three.go: too large — skipped") {
		t.Fatalf("placeholder footer missing:\n%s", v)
	}
	// Found unreadable mid-list: the step continues to the next file.
	m.ShowReviewFile(0, contents[0][0], contents[0][1])
	m.ReviewFiles()[1].Skip = ""
	m.ReviewFiles()[2].Skip = ""
	load = loadMsg(t, m.StepReviewFile(1))
	if load.Index != 1 {
		t.Fatalf("requested %d, want 1", load.Index)
	}
	load = loadMsg(t, m.ReviewSkip(1, "binary"))
	if load.Index != 2 {
		t.Fatalf("skip continued to %d, want 2", load.Index)
	}
}

func TestReviewRefreshKeepsPositionWhenAFileDropsOut(t *testing.T) {
	m, contents := reviewModel(t)
	m.ShowReviewFile(1, contents[1][0], contents[1][1])
	files := m.ReviewFiles()
	// The first file became clean: the current one keeps its identity and
	// is re-requested in place (HEAD may have moved).
	cmd := m.UpdateReviewFiles([]ReviewFile{files[1], files[2]})
	if load := loadMsg(t, cmd); load.Index != 0 {
		t.Fatalf("refresh re-requested %d, want 0 (the current file's new index)", load.Index)
	}
	if m.ReviewIndex() != 0 {
		t.Fatalf("index after refresh = %d", m.ReviewIndex())
	}
	cur, _ := m.ReviewCurrent()
	if cur.Path != "b/two.go" {
		t.Fatalf("current after refresh = %q", cur.Path)
	}
	m.StepHunk(1) // land on its hunk so a position exists to keep
	hunk := m.CurrentHunk()
	m.ShowReviewFile(0, contents[1][0], contents[1][1])
	if m.CurrentHunk() != hunk {
		t.Fatalf("in-place reload moved the current hunk: %d → %d", hunk, m.CurrentHunk())
	}
	if v := plainView(m); !strings.Contains(v, "file 1/2") {
		t.Fatalf("footer after refresh:\n%s", v)
	}
	// The current file itself drops out: its successor loads.
	cmd = m.UpdateReviewFiles([]ReviewFile{files[2]})
	if load := loadMsg(t, cmd); load.Index != 0 {
		t.Fatalf("successor request = %d, want 0", load.Index)
	}
	m.ShowReviewFile(0, contents[2][0], contents[2][1])
	if cur, _ := m.ReviewCurrent(); cur.Path != "c/three.go" {
		t.Fatalf("current = %q, want the successor", cur.Path)
	}
	// Everything clean: the diff stays, the footer says so.
	if cmd := m.UpdateReviewFiles(nil); cmd != nil {
		t.Fatal("an empty list requests nothing")
	}
	if v := plainView(m); !strings.Contains(v, "no changes left to review") {
		t.Fatalf("empty-list notice missing:\n%s", v)
	}
}

func TestReviewReloadRightKeepsLeft(t *testing.T) {
	m, contents := reviewModel(t)
	m.ReloadRight("a\nb\nc\nd\ne\nf\ng\nh\n")
	if m.HunkCount() != 0 {
		t.Fatalf("right side reverted to HEAD → no hunks, got %d", m.HunkCount())
	}
	m.ReloadRight(contents[0][1])
	if m.HunkCount() != 2 {
		t.Fatalf("hunks = %d, want 2", m.HunkCount())
	}
}

func TestReviewFilePickerJumps(t *testing.T) {
	m, _ := reviewModel(t)
	m.SetFocused(true)
	m.Update(key("f"))
	if !m.PickingFile() {
		t.Fatal("f opens the picker in review mode")
	}
	v := plainView(m)
	if !strings.Contains(v, "file: ") || !strings.Contains(v, "1/3  M a/one.go") {
		t.Fatalf("picker row should show the current file:\n%s", v)
	}
	for _, r := range "thr" {
		m.Update(key(string(r)))
	}
	if v := plainView(m); !strings.Contains(v, "1/1  D c/three.go") {
		t.Fatalf("typing filters the list:\n%s", v)
	}
	cmd := m.Update(key("enter"))
	if m.PickingFile() {
		t.Fatal("enter closes the picker")
	}
	if load := loadMsg(t, cmd); load.Index != 2 {
		t.Fatalf("picker jumped to %d, want 2", load.Index)
	}
	// esc abandons; a single-file diff ignores f.
	m.Update(key("f"))
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.PickingFile() {
		t.Fatal("esc closes the picker")
	}
	s := testModel(t, "a\n", "b\n")
	s.SetFocused(true)
	s.Update(key("f"))
	if s.PickingFile() {
		t.Fatal("a single-file diff has no picker")
	}
}

func TestReviewPickerArrowsBrowseAllFiles(t *testing.T) {
	m, _ := reviewModel(t)
	m.SetFocused(true)
	m.Update(key("f"))
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if v := plainView(m); !strings.Contains(v, "2/3  AM b/two.go") {
		t.Fatalf("down moves to the next file:\n%s", v)
	}
	cmd := m.Update(key("enter"))
	if load := loadMsg(t, cmd); load.Index != 1 {
		t.Fatalf("enter after down jumped to %d, want 1", load.Index)
	}
}

func TestReviewEmptyStartShowsNotice(t *testing.T) {
	m := New("diff", "", "", "", nil)
	m.SetSize(80, 12)
	if cmd := m.StartReview(nil, 0); cmd != nil {
		t.Fatal("nothing to load")
	}
	if !m.Reviewing() {
		t.Fatal("review mode is on even over an empty list")
	}
	if v := plainView(&m); !strings.Contains(v, "no changes to review") {
		t.Fatalf("notice missing:\n%s", v)
	}
}
