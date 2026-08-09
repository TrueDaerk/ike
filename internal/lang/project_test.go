package lang

import (
	"errors"
	"testing"
)

// scaffoldToolchain is a Toolchain offering project scaffolding (#1718).
type scaffoldToolchain struct {
	opts      []ProjectOption
	gotRoot   string
	gotOption string
	err       error
}

func (*scaffoldToolchain) Detect(string) (map[string]any, bool) { return nil, false }
func (s *scaffoldToolchain) ProjectOptions() []ProjectOption    { return s.opts }
func (s *scaffoldToolchain) ScaffoldProject(root, option string) error {
	s.gotRoot, s.gotOption = root, option
	return s.err
}

// plainToolchain detects only — no scaffolding.
type plainToolchain struct{}

func (plainToolchain) Detect(string) (map[string]any, bool) { return nil, false }

// TestProjectLanguagesFiltersScaffolders guards the wizard's type list: only
// languages whose Toolchain implements ProjectScaffolder appear.
func TestProjectLanguagesFiltersScaffolders(t *testing.T) {
	sc := &scaffoldToolchain{opts: []ProjectOption{{ID: "basic", Label: "Basic", Available: true}}}
	Register(Language{ID: "scaffproj", Toolchain: sc})
	Register(Language{ID: "noscaff", Toolchain: plainToolchain{}})
	Register(Language{ID: "notool"})

	found := map[string]bool{}
	for _, l := range ProjectLanguages() {
		found[l.ID] = true
	}
	if !found["scaffproj"] {
		t.Fatal("ProjectLanguages must include the scaffolding language")
	}
	if found["noscaff"] || found["notool"] {
		t.Fatalf("ProjectLanguages must exclude non-scaffolders, got %v", found)
	}
}

// TestProjectOptionsAndScaffoldDispatch guards the registry entry points.
func TestProjectOptionsAndScaffoldDispatch(t *testing.T) {
	sc := &scaffoldToolchain{opts: []ProjectOption{{ID: "basic", Label: "Basic", Available: true}}}
	Register(Language{ID: "scaffproj2", Toolchain: sc})

	opts := ProjectOptions("scaffproj2")
	if len(opts) != 1 || opts[0].ID != "basic" {
		t.Fatalf("ProjectOptions = %+v", opts)
	}
	if ProjectOptions("noscaff") != nil || ProjectOptions("no-such-lang") != nil {
		t.Fatal("ProjectOptions must be nil without a scaffolder")
	}

	if err := ScaffoldProject("scaffproj2", "/tmp/x", "basic"); err != nil {
		t.Fatalf("ScaffoldProject: %v", err)
	}
	if sc.gotRoot != "/tmp/x" || sc.gotOption != "basic" {
		t.Fatalf("scaffold got (%q, %q)", sc.gotRoot, sc.gotOption)
	}

	sc.err = errors.New("boom")
	if err := ScaffoldProject("scaffproj2", "/tmp/x", "basic"); err == nil || err.Error() != "boom" {
		t.Fatalf("scaffold error not propagated: %v", err)
	}
}

// TestScaffoldProjectRejectsNonScaffolders keeps the error paths explicit.
func TestScaffoldProjectRejectsNonScaffolders(t *testing.T) {
	Register(Language{ID: "noscaff2", Toolchain: plainToolchain{}})
	if err := ScaffoldProject("noscaff2", "/tmp/x", "basic"); err == nil {
		t.Fatal("a language without a scaffolder must refuse")
	}
	if err := ScaffoldProject("no-such-lang", "/tmp/x", "basic"); err == nil {
		t.Fatal("an unknown language must refuse")
	}
}
