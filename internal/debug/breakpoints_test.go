package debug

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestToggleAndLines(t *testing.T) {
	b := NewBreakpoints()
	if !b.Toggle("a.py", 5) || !b.Has("a.py", 5) {
		t.Fatal("first toggle must set")
	}
	b.Toggle("a.py", 2)
	if got := b.Lines("a.py"); !reflect.DeepEqual(got, []int{2, 5}) {
		t.Fatalf("Lines = %v, want sorted [2 5]", got)
	}
	if b.Toggle("a.py", 5) || b.Has("a.py", 5) {
		t.Fatal("second toggle must clear")
	}
	if b.Count() != 1 {
		t.Fatalf("Count = %d", b.Count())
	}
	b.Toggle("a.py", 2)
	if len(b.All()) != 0 {
		t.Fatal("clearing the last line must drop the file entry")
	}
}

func TestAdjustEditInsert(t *testing.T) {
	b := NewBreakpoints()
	for _, l := range []int{2, 5, 9} {
		b.Toggle("a.py", l)
	}
	// Enter at the end of line 4: one line inserted, cursor lands on 5.
	b.AdjustEdit("a.py", 5, 1)
	if got := b.Lines("a.py"); !reflect.DeepEqual(got, []int{2, 6, 10}) {
		t.Fatalf("after insert: %v, want [2 6 10]", got)
	}
	// Open a line above line 2 (O): cursor on the new line 2, delta +1 —
	// the breakpoint that was on 2 moves down with its text.
	b2 := NewBreakpoints()
	b2.Toggle("a.py", 2)
	b2.AdjustEdit("a.py", 2, 1)
	if got := b2.Lines("a.py"); !reflect.DeepEqual(got, []int{3}) {
		t.Fatalf("after open-above: %v, want [3]", got)
	}
}

func TestAdjustEditDelete(t *testing.T) {
	b := NewBreakpoints()
	for _, l := range []int{2, 5, 9} {
		b.Toggle("a.py", l)
	}
	// dd on line 3 (cursor stays on 3): lines below pull up.
	b.AdjustEdit("a.py", 3, -1)
	if got := b.Lines("a.py"); !reflect.DeepEqual(got, []int{2, 4, 8}) {
		t.Fatalf("after delete: %v, want [2 4 8]", got)
	}
	// Deleting several lines collapses in-range breakpoints onto the cursor
	// row, deduplicated.
	b2 := NewBreakpoints()
	for _, l := range []int{4, 5, 6} {
		b2.Toggle("a.py", l)
	}
	b2.AdjustEdit("a.py", 3, -3)
	if got := b2.Lines("a.py"); !reflect.DeepEqual(got, []int{3}) {
		t.Fatalf("after range delete: %v, want [3]", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	b := NewBreakpoints()
	b.Toggle("pkg/a.py", 7)
	b.Toggle("b.go", 1)
	if err := b.Save(); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if !got.Has("pkg/a.py", 7) || !got.Has("b.go", 1) || got.Count() != 2 {
		t.Fatalf("round trip lost data: %v", got.All())
	}
}

func TestEnableDisable(t *testing.T) {
	b := NewBreakpoints()
	b.Toggle("a.py", 3)
	b.Toggle("a.py", 7)
	if !b.Enabled("a.py", 3) {
		t.Fatal("a fresh breakpoint must be enabled")
	}
	b.SetEnabled("a.py", 3, false)
	if b.Enabled("a.py", 3) || !b.Has("a.py", 3) {
		t.Fatal("disable must keep the breakpoint but mark it off")
	}
	if got := b.EnabledLines("a.py"); !reflect.DeepEqual(got, []int{7}) {
		t.Fatalf("EnabledLines = %v, want [7]", got)
	}
	if got := b.DisabledLines("a.py"); !reflect.DeepEqual(got, []int{3}) {
		t.Fatalf("DisabledLines = %v, want [3]", got)
	}
	b.SetEnabled("a.py", 3, true)
	if !b.Enabled("a.py", 3) || len(b.DisabledLines("a.py")) != 0 {
		t.Fatal("re-enable must clear the disabled flag")
	}
	// Disabling a line without a breakpoint is a no-op.
	b.SetEnabled("a.py", 99, false)
	if len(b.DisabledLines("a.py")) != 0 {
		t.Fatal("SetEnabled on a bare line must not invent state")
	}
	// Toggling a disabled breakpoint off drops the flag with it.
	b.SetEnabled("a.py", 7, false)
	b.Toggle("a.py", 7)
	b.Toggle("a.py", 7)
	if !b.Enabled("a.py", 7) {
		t.Fatal("a re-added breakpoint must start enabled")
	}
}

func TestRemoveAndRemoveAll(t *testing.T) {
	b := NewBreakpoints()
	b.Toggle("a.py", 1)
	b.Toggle("b.go", 2)
	if !b.Remove("a.py", 1) || b.Has("a.py", 1) {
		t.Fatal("Remove must delete an existing breakpoint")
	}
	if b.Remove("a.py", 1) {
		t.Fatal("Remove on a bare line must report false")
	}
	b.Toggle("b.go", 9)
	b.SetEnabled("b.go", 9, false)
	if n := b.RemoveAll(); n != 2 {
		t.Fatalf("RemoveAll = %d, want 2", n)
	}
	if b.Count() != 0 || len(b.DisabledLines("b.go")) != 0 {
		t.Fatal("RemoveAll must empty the store including disabled flags")
	}
}

func TestAdjustEditShiftsDisabledFlag(t *testing.T) {
	b := NewBreakpoints()
	b.Toggle("a.py", 2)
	b.Toggle("a.py", 5)
	b.SetEnabled("a.py", 5, false)
	b.AdjustEdit("a.py", 4, 1) // insert one line above 5
	if got := b.Lines("a.py"); !reflect.DeepEqual(got, []int{2, 6}) {
		t.Fatalf("Lines = %v, want [2 6]", got)
	}
	if b.Enabled("a.py", 6) || !b.Enabled("a.py", 2) {
		t.Fatal("the disabled flag must travel with its shifted line")
	}
}

func TestSaveLoadKeepsDisabled(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	b := NewBreakpoints()
	b.Toggle("a.py", 3)
	b.Toggle("a.py", 8)
	b.SetEnabled("a.py", 8, false)
	if err := b.Save(); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if !got.Enabled("a.py", 3) || got.Enabled("a.py", 8) || !got.Has("a.py", 8) {
		t.Fatalf("round trip lost the disabled state: %v / %v", got.All(), got.DisabledLines("a.py"))
	}
}

func TestLoadLegacyFormat(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	legacy := []byte(`{"a.py": [2, 5]}`)
	if err := os.WriteFile(filepath.Join(dir, "breakpoints.json"), legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if !got.Has("a.py", 2) || !got.Has("a.py", 5) || !got.Enabled("a.py", 2) {
		t.Fatalf("legacy layout must load enabled: %v", got.All())
	}
}

func TestLoadTolerant(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	if Load().Count() != 0 {
		t.Fatal("missing file must load empty")
	}
	if err := os.WriteFile(filepath.Join(dir, "breakpoints.json"), []byte("{oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	if Load().Count() != 0 {
		t.Fatal("malformed file must load empty")
	}
}
