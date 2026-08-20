package perfhud

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// namedTickMsg and lowerTickMsg cover both spellings the codebase uses for
// timer deadlines: exported ones (BackupTickMsg) and package-private ones
// (followTickMsg).
type namedTickMsg struct{}
type lowertickMsg struct{}

func TestClassifyBucketsMessages(t *testing.T) {
	cases := []struct {
		msg  tea.Msg
		want Category
	}{
		{tea.KeyPressMsg{}, CatKey},
		{tea.KeyReleaseMsg{}, CatKey},
		{tea.PasteMsg{Content: "hello"}, CatKey},
		{tea.PasteStartMsg{}, CatKey},
		{tea.PasteEndMsg{}, CatKey},
		{tea.MouseClickMsg{}, CatMouse},
		{tea.MouseMotionMsg{}, CatMouse},
		{tea.MouseWheelMsg{}, CatMouse},
		{tea.MouseReleaseMsg{}, CatMouse},
		{tea.WindowSizeMsg{}, CatResize},
		{namedTickMsg{}, CatTick},
		{lowertickMsg{}, CatTick},
		{tea.FocusMsg{}, CatOther},
		{fakeResultMsg{}, CatOther},
	}
	for _, c := range cases {
		name := typeName(c.msg)
		if got := classify(c.msg, name); got != c.want {
			t.Errorf("classify(%s) = %s, want %s", name, got, c.want)
		}
	}
}

func TestTypeNameIsTheConcreteType(t *testing.T) {
	if got := typeName(tea.KeyPressMsg{}); got != "tea.KeyPressMsg" {
		t.Fatalf("typeName = %q, want tea.KeyPressMsg", got)
	}
	if got := typeName(nil); got != "<nil>" {
		t.Fatalf("typeName(nil) = %q, want <nil>", got)
	}
}

func TestCategoryStrings(t *testing.T) {
	want := []string{"key", "mouse", "resize", "tick", "other"}
	for i, w := range want {
		if got := Category(i).String(); got != w {
			t.Errorf("Category(%d) = %q, want %q", i, got, w)
		}
	}
	if got := Category(CatCount).String(); got != "?" {
		t.Errorf("out-of-range category = %q, want ?", got)
	}
}

// TestClassificationIsCachedPerType pins the reason the HUD can afford a
// per-type breakdown: the type switch runs once per message *type*, and the
// counters keep aggregating correctly across the cached path.
func TestClassificationIsCachedPerType(t *testing.T) {
	c, _ := testCollector(t)
	for i := 0; i < 5; i++ {
		c.Count(namedTickMsg{})
	}
	if got := len(c.catOf); got != 1 {
		t.Fatalf("classification cache holds %d entries, want 1", got)
	}
	if got := c.counts[CatTick]; got != 5 {
		t.Fatalf("tick count = %d, want 5", got)
	}
}
