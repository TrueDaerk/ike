package palette

import "testing"

// TestDirModeRanksAndEmitsMoveTarget guards the directory picker (#175): the
// root is offered, queries fuzzy-filter, and activation carries the picked
// directory in a MoveTargetMsg.
func TestDirModeRanksAndEmitsMoveTarget(t *testing.T) {
	d := &DirMode{walk: func(string) []string {
		return []string{"./", "internal/app", "internal/editor", "docs"}
	}}
	items := d.Results("", Context{Root: "."})
	if len(items) != 4 {
		t.Fatalf("empty query must list all dirs, got %d", len(items))
	}
	items = d.Results("edit", Context{Root: "."})
	if len(items) != 1 || items[0].Title != "internal/editor" {
		t.Fatalf("fuzzy filter wrong: %+v", items)
	}
	msg, ok := items[0].Msg.(MoveTargetMsg)
	if !ok || msg.Dir != "internal/editor" {
		t.Fatalf("activation must carry MoveTargetMsg with the dir, got %#v", items[0].Msg)
	}
}

// TestWalkProjectDirsIncludesRootFirst: the real walk offers "./" so a nested
// file can move to the project root.
func TestWalkProjectDirsIncludesRootFirst(t *testing.T) {
	dirs := walkProjectDirs(t.TempDir())
	if len(dirs) == 0 || dirs[0] != "./" {
		t.Fatalf("walk must list the root first, got %v", dirs)
	}
}
