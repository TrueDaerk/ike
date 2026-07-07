package lang

import "testing"

func TestRegisterAndLookup(t *testing.T) {
	Register(Language{
		ID:         "fakelang",
		Extensions: []string{"fl", "FLX"},
		Filenames:  []string{"Fakefile"},
	})

	if l, ok := ByID("fakelang"); !ok || l.ID != "fakelang" {
		t.Fatalf("ByID = %+v, %v", l, ok)
	}
	// Extension lookup is case-insensitive and dot-optional.
	for _, path := range []string{"a.fl", "DIR/b.FL", "c.flx"} {
		if l, ok := ByPath(path); !ok || l.ID != "fakelang" {
			t.Errorf("ByPath(%q) = %+v, %v", path, l, ok)
		}
	}
	// Exact base name wins even with a different extension.
	if l, ok := ByPath("some/Fakefile"); !ok || l.ID != "fakelang" {
		t.Errorf("ByPath(Fakefile) = %+v, %v", l, ok)
	}
	if _, ok := ByPath("x.unknownext"); ok {
		t.Error("unknown extension should not resolve")
	}
}

func TestComments(t *testing.T) {
	Register(Language{
		ID:           "commented",
		Extensions:   []string{"cmt"},
		LineComment:  "//",
		BlockComment: [2]string{"/*", "*/"},
	})
	Register(Language{
		ID:          "lineonly",
		Extensions:  []string{"lo"},
		LineComment: "#",
	})
	Register(Language{ID: "bare", Extensions: []string{"bare"}})

	if line, block, ok := Comments("x.cmt"); !ok || line != "//" || block != [2]string{"/*", "*/"} {
		t.Fatalf("Comments(x.cmt) = %q %v %v", line, block, ok)
	}
	if line, _, ok := Comments("x.lo"); !ok || line != "#" {
		t.Fatalf("Comments(x.lo) = %q %v", line, ok)
	}
	// A language without any comment syntax and an unknown path both report !ok.
	if _, _, ok := Comments("x.bare"); ok {
		t.Fatal("Comments(x.bare) should be !ok (no syntax declared)")
	}
	if _, _, ok := Comments("x.unknown-ext"); ok {
		t.Fatal("Comments on unknown language should be !ok")
	}
}

func TestMergeSettings(t *testing.T) {
	base := map[string]any{
		"python": map[string]any{"defaultInterpreterPath": "/detected/python", "extra": 1},
		"only":   "base",
	}
	over := map[string]any{
		"python": map[string]any{"defaultInterpreterPath": "/user/python"}, // user wins
		"top":    "over",
	}
	got := MergeSettings(base, over)

	py := got["python"].(map[string]any)
	if py["defaultInterpreterPath"] != "/user/python" {
		t.Errorf("user setting should win: %v", py["defaultInterpreterPath"])
	}
	if py["extra"] != 1 {
		t.Errorf("detected sibling key should survive: %v", py["extra"])
	}
	if got["only"] != "base" || got["top"] != "over" {
		t.Errorf("top-level merge wrong: %v", got)
	}
}
