package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/lang"
	"ike/internal/scratch"
)

// materialize_test.go covers the two #2056 follow-ups to "Treat Buffer as …":
// opening the matching playground from a typed file-less buffer, and
// materializing such a buffer to a real file so the path-keyed subsystems
// (LSP above all) apply to it.

// dataLangs registers the minimal data languages these tests type buffers as
// — id and extension only, like bufferLangLangs, deliberately not the
// language plugins whose ServerSpec would open the missing-server prompt.
func dataLangs() {
	lang.Register(lang.Language{ID: "json", Extensions: []string{"json"}})
	lang.Register(lang.Language{ID: "yaml", Extensions: []string{"yaml", "yml"}})
	lang.Register(lang.Language{ID: "dockerfile", Filenames: []string{"Dockerfile"}})
}

// typedBuffer is a file-less buffer holding content and treated as langID.
func typedBuffer(t *testing.T, langID, content string) Model {
	t.Helper()
	dataLangs()
	m := filelessModel(t, content, 0, 0)
	out, _ := m.Update(SetBufferLangMsg{ID: langID})
	m = out.(Model)
	if got := m.activeEditor().LangID(); got != langID {
		t.Fatalf("the buffer must be treated as %s, got %q", langID, got)
	}
	return m
}

// scratchDir is where the model under test materializes to. The model helpers
// already sandbox IKE_CONFIG_DIR into a temp dir, so this only has to ask the
// store where that put it — no materialized buffer ever lands in the
// developer's ~/.ike.
func scratchDir(t *testing.T) string {
	t.Helper()
	dir, err := scratch.Dir()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestMaterializeBindsBufferToScratchFile is the acceptance case: the command
// creates a file with the language's extension, the buffer points at it, and
// its text is what the file holds — which is what makes the LSP `didOpen`
// wiring bindUntitled performs meaningful.
func TestMaterializeBindsBufferToScratchFile(t *testing.T) {
	m := typedBuffer(t, "json", `{"a": 1}`)
	dir := scratchDir(t)

	out, cmd := m.Update(MaterializeBufferMsg{})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("materialize must return the bind wiring (watcher, hooks, reparse)")
	}

	ed := m.activeEditor()
	if !ed.HasFile() {
		t.Fatal("the buffer must point at the materialized file")
	}
	if ext := filepath.Ext(ed.Path()); ext != ".json" {
		t.Fatalf("materialized under %q, want a .json file", ed.Path())
	}
	if got := filepath.Dir(ed.Path()); got != dir {
		t.Fatalf("materialized into %q, want the scratch store %q", got, dir)
	}
	body, err := os.ReadFile(ed.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(body)) != `{"a": 1}` {
		t.Fatalf("the file holds %q, want the buffer text", string(body))
	}
	// The path classifies the buffer from here on, so the override is gone
	// while the language it named stays.
	if ed.LangOverride() != "" {
		t.Errorf("a materialized buffer must be classified by its file name, override %q survived", ed.LangOverride())
	}
	if ed.LangID() != "json" {
		t.Errorf("the materialized file must still resolve as json, got %q", ed.LangID())
	}
}

// TestMaterializeSavesUnderRealNameAfterwards guards the documented promise:
// materializing is not a one-way door — the buffer is an ordinary file buffer
// afterwards, so "Save As" moves it into the project.
func TestMaterializeSavesUnderRealNameAfterwards(t *testing.T) {
	m := typedBuffer(t, "json", `{"a": 1}`)
	out, _ := m.Update(MaterializeBufferMsg{})
	m = out.(Model)

	target := filepath.Join(t.TempDir(), "real.json")
	if err := m.activeEditor().SaveTo(target); err != nil {
		t.Fatalf("saving a materialized buffer under a real name: %v", err)
	}
	if got := m.activeEditor().Path(); got != target {
		t.Fatalf("the buffer points at %q, want %q", got, target)
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(body)) != `{"a": 1}` {
		t.Fatalf("the saved file holds %q, want the buffer text", string(body))
	}
}

// TestMaterializeRefusals covers the three states the command does not apply
// in: no type chosen, a type with no extension, and a buffer that already has
// a file. None of them may leave a file behind.
func TestMaterializeRefusals(t *testing.T) {
	t.Run("untyped buffer", func(t *testing.T) {
		m := filelessModel(t, "some pasted text", 0, 0)
		dir := scratchDir(t)
		out, _ := m.Update(MaterializeBufferMsg{})
		m = out.(Model)
		if m.activeEditor().HasFile() {
			t.Fatalf("an untyped buffer must not be materialized, got %q", m.activeEditor().Path())
		}
		assertNoScratch(t, dir)
	})
	t.Run("language without an extension", func(t *testing.T) {
		m := typedBuffer(t, "dockerfile", "FROM alpine")
		dir := scratchDir(t)
		out, _ := m.Update(MaterializeBufferMsg{})
		m = out.(Model)
		if m.activeEditor().HasFile() {
			t.Fatalf("a base-name language has no extension to materialize under, got %q", m.activeEditor().Path())
		}
		assertNoScratch(t, dir)
	})
	t.Run("buffer with a file", func(t *testing.T) {
		m := intentionModel(t, "x.json", `{"a": 1}`, 0, 0)
		dir := scratchDir(t)
		path := m.activeEditor().Path()
		out, _ := m.Update(MaterializeBufferMsg{})
		m = out.(Model)
		if got := m.activeEditor().Path(); got != path {
			t.Fatalf("the buffer moved from %q to %q", path, got)
		}
		assertNoScratch(t, dir)
	})
}

// assertNoScratch fails when the refused command allocated a scratch file
// anyway — a leftover empty file the scratch panel would then list.
func assertNoScratch(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("a refused materialize left %d file(s) in %s", len(entries), dir)
	}
}

// TestMaterializeUsesScratchStore pins the documented location: the file the
// command creates is one the scratch panel lists (and can delete), not a
// second temp store nobody cleans up.
func TestMaterializeUsesScratchStore(t *testing.T) {
	m := typedBuffer(t, "yaml", "a: 1")
	out, _ := m.Update(MaterializeBufferMsg{})
	m = out.(Model)

	path := m.activeEditor().Path()
	list, err := scratch.List()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range list {
		if p == path {
			found = true
		}
	}
	if !found {
		t.Fatalf("materialized %q is not in the scratch listing %v", path, list)
	}
}

// TestTypedBufferOffersPlaygroundAndMaterialize is the intention half of
// #2056: both follow-ups are reachable through alt+enter, which is where the
// type was chosen in the first place.
func TestTypedBufferOffersPlaygroundAndMaterialize(t *testing.T) {
	for _, tc := range []struct {
		langID, content, want string
	}{
		{"json", `{"a": 1}`, "Open in jq Playground"},
		{"yaml", "a: 1", "Open in yq Playground"},
	} {
		t.Run(tc.langID, func(t *testing.T) {
			m := typedBuffer(t, tc.langID, tc.content)
			titles := offeredTitles(t, m)
			if !hasTitle(titles, tc.want) {
				t.Errorf("%q is missing from %v", tc.want, titles)
			}
			if !hasTitle(titles, "Materialize to File") {
				t.Errorf("the materialize entry is missing from %v", titles)
			}
		})
	}
}

// TestTypedBufferOpensPlaygroundOverItsOwnText is the playground half: the
// command opens over the file-less buffer and queries its content — no save,
// no copy into a scratch first.
func TestTypedBufferOpensPlaygroundOverItsOwnText(t *testing.T) {
	noDebounce(t)
	m := typedBuffer(t, "json", `{"foo":[{"bar":1},{"bar":4}]}`)
	tm, cmd := m.Update(OpenPlaygroundMsg{})
	m = drainCmd(tm.(Model), cmd)
	if !m.playOpen() {
		t.Fatal("json.jqPlayground must open over a typed file-less buffer")
	}
	m = setProgram(m, ".foo[] | select(.bar > 3)")
	if m.play.result.Err != "" {
		t.Fatalf("the program reported %q", m.play.result.Err)
	}
	if len(m.play.result.Outputs) != 1 {
		t.Fatalf("got %d outputs, want 1: %v", len(m.play.result.Outputs), m.play.result.Outputs)
	}
}
