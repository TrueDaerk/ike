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

// TestServerDelegation covers the ServerLanguage seam (#1063): a language may
// delegate its documents to another language's server while keeping its own ID
// as the wire languageId.
func TestServerDelegation(t *testing.T) {
	Register(Language{ID: "delehost", Server: &ServerSpec{Language: "delehost", Command: "fake"}})
	Register(Language{ID: "deleaux", Filenames: []string{"Delefile"}, ServerLanguage: "delehost"})
	Register(Language{ID: "deleorphan", ServerLanguage: "no-such-language"})
	Register(Language{ID: "deleplain"})

	host, _ := ByID("delehost")
	aux, _ := ByID("deleaux")
	orphan, _ := ByID("deleorphan")
	plain, _ := ByID("deleplain")

	if got := host.ServerLang(); got != "delehost" {
		t.Errorf("host ServerLang = %q", got)
	}
	if got := aux.ServerLang(); got != "delehost" {
		t.Errorf("aux ServerLang = %q, want delehost", got)
	}
	if !host.HasServer() {
		t.Error("host HasServer = false")
	}
	if !aux.HasServer() {
		t.Error("aux HasServer = false, want true via delegate")
	}
	if orphan.HasServer() {
		t.Error("orphan HasServer = true, want false (delegate unknown)")
	}
	if plain.HasServer() {
		t.Error("plain HasServer = true, want false")
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

func TestIndentAfter(t *testing.T) {
	Register(Language{
		ID:          "indented",
		Extensions:  []string{"ind"},
		IndentAfter: []string{":", "{"},
	})
	Register(Language{ID: "noindent", Extensions: []string{"noi"}})

	if suf, ok := IndentAfter("x.ind"); !ok || len(suf) != 2 || suf[0] != ":" || suf[1] != "{" {
		t.Fatalf("IndentAfter(x.ind) = %v %v", suf, ok)
	}
	// A language without rules and an unknown path both report !ok.
	if _, ok := IndentAfter("x.noi"); ok {
		t.Fatal("IndentAfter(x.noi) should be !ok (no rules declared)")
	}
	if _, ok := IndentAfter("x.unknown-ext"); ok {
		t.Fatal("IndentAfter on unknown language should be !ok")
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
