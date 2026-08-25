package scratch

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ike/internal/lang"
)

// sandbox points the store at a fresh IKE_CONFIG_DIR and returns the expected
// scratch dir.
func sandbox(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	return filepath.Join(dir, "scratches")
}

func TestDirHonorsConfigDirOverride(t *testing.T) {
	want := sandbox(t)
	got, err := Dir()
	if err != nil || got != want {
		t.Fatalf("Dir() = %q, %v; want %q", got, err, want)
	}
}

func TestCreateAllocatesSequentially(t *testing.T) {
	dir := sandbox(t)

	first, err := Create("py")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "scratch-1.py"); first != want {
		t.Fatalf("first = %q, want %q", first, want)
	}
	if _, err := os.Stat(first); err != nil {
		t.Fatalf("scratch must exist on disk: %v", err)
	}
	// The counter skips existing names, dot-optional extension.
	second, err := Create(".py")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "scratch-2.py"); second != want {
		t.Fatalf("second = %q, want %q", second, want)
	}
	// A different extension restarts at the first free N for that name.
	other, err := Create("")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "scratch-1.txt"); other != want {
		t.Fatalf("empty ext must mean txt: %q, want %q", other, want)
	}
}

// TestCreateSeedsLanguageTemplate covers #1223: a scratch of a language that
// registers a file template opens with that template rendered, so e.g. a PHP
// scratch is runnable as created; languages without one stay empty.
func TestCreateSeedsLanguageTemplate(t *testing.T) {
	sandbox(t)
	lang.Register(lang.Language{ID: "scrtpl", Extensions: []string{"scrtpl"}, Template: "<?tpl ${NAME}\n"})
	lang.Register(lang.Language{ID: "scrbare", Extensions: []string{"scrbare"}})

	path, err := Create("scrtpl")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "<?tpl scratch-1\n"; string(data) != want {
		t.Fatalf("seeded content = %q, want %q", data, want)
	}

	bare, err := Create("scrbare")
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(bare); err != nil || len(data) != 0 {
		t.Fatalf("template-less language must create an empty scratch: %q, %v", data, err)
	}
}

func TestListNewestFirstAndMissingDir(t *testing.T) {
	sandbox(t)

	// Missing dir: empty list, no error.
	if got, err := List(); err != nil || len(got) != 0 {
		t.Fatalf("List() on missing dir = %v, %v", got, err)
	}

	a, _ := Create("txt")
	b, _ := Create("txt")
	// Make the first strictly older so mod-time ordering is deterministic.
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(a, old, old); err != nil {
		t.Fatal(err)
	}
	got, err := List()
	if err != nil || len(got) != 2 {
		t.Fatalf("List() = %v, %v", got, err)
	}
	if got[0] != b || got[1] != a {
		t.Fatalf("want newest first [%q %q], got %v", b, a, got)
	}
}

// TestEntriesCarryModTimes covers the panel's data source (#1932): the same
// newest-first order as List, with the mod time each row renders.
func TestEntriesCarryModTimes(t *testing.T) {
	sandbox(t)

	if got, err := Entries(); err != nil || len(got) != 0 {
		t.Fatalf("Entries() on missing dir = %v, %v", got, err)
	}

	a, _ := Create("txt")
	b, _ := Create("txt")
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(a, old, old); err != nil {
		t.Fatal(err)
	}
	got, err := Entries()
	if err != nil || len(got) != 2 {
		t.Fatalf("Entries() = %v, %v", got, err)
	}
	if got[0].Path != b || got[1].Path != a {
		t.Fatalf("want newest first [%q %q], got %v", b, a, got)
	}
	if !got[1].ModTime.Equal(old) {
		t.Fatalf("mod time = %v, want %v", got[1].ModTime, old)
	}
}

// TestDeleteRemovesScratch covers the panel's delete action (#1932).
func TestDeleteRemovesScratch(t *testing.T) {
	sandbox(t)

	path, err := Create("txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := Delete(path); err != nil {
		t.Fatalf("Delete(%q) = %v", path, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("scratch must be gone: %v", err)
	}
	if got, err := List(); err != nil || len(got) != 0 {
		t.Fatalf("List() after delete = %v, %v", got, err)
	}
	// A second delete of the same path is an error, not a silent success.
	if err := Delete(path); err == nil {
		t.Fatal("deleting a missing scratch must fail")
	}
}

// TestDeleteRefusesOutsideDir is the guard rail: Delete only ever removes a
// file lying directly in the scratch dir (#1932).
func TestDeleteRefusesOutsideDir(t *testing.T) {
	dir := sandbox(t)
	if _, err := Create("txt"); err != nil { // materializes the dir
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "victim.txt")
	if err := os.WriteFile(outside, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(dir, "sub")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(nested, "deep.txt")
	if err := os.WriteFile(deep, []byte("keep me too"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		outside,
		filepath.Join(dir, "..", filepath.Base(outside)),
		deep,
		nested, // a directory inside the scratch dir is not a scratch file
		"",
	} {
		if err := Delete(path); err == nil {
			t.Fatalf("Delete(%q) must be refused", path)
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("file outside the scratch dir must survive: %v", err)
	}
	if _, err := os.Stat(deep); err != nil {
		t.Fatalf("nested file must survive: %v", err)
	}
}

// TestRenameRenamesInPlace covers #1963: Rename gives a scratch a new base
// name inside the store and returns the new absolute path.
func TestRenameRenamesInPlace(t *testing.T) {
	dir := sandbox(t)
	path, err := Create("txt")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Rename(path, "notes.md")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "notes.md"); got != want {
		t.Fatalf("Rename = %q want %q", got, want)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("renamed scratch must exist: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("old name must be gone, stat err = %v", err)
	}
	// Renaming onto the same name is a no-op, not an "already exists" error.
	if same, err := Rename(got, "notes.md"); err != nil || same != got {
		t.Fatalf("same-name rename = %q, %v", same, err)
	}
}

// TestFirstLineSkipsLeadingBlankLines covers the picker's title source
// (#2057): FirstLine returns the first non-empty, trimmed line, skipping
// leading blank ones.
func TestFirstLineSkipsLeadingBlankLines(t *testing.T) {
	sandbox(t)
	path, err := Create("txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("\n  \n  hello world  \nsecond line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := FirstLine(path); got != "hello world" {
		t.Fatalf("FirstLine = %q, want %q", got, "hello world")
	}
}

// TestFirstLineEmptyOrMissing covers the picker's placeholder trigger: an
// empty file, an all-blank file and a vanished path all yield "".
func TestFirstLineEmptyOrMissing(t *testing.T) {
	dir := sandbox(t)
	empty, err := Create("txt")
	if err != nil {
		t.Fatal(err)
	}
	if got := FirstLine(empty); got != "" {
		t.Fatalf("FirstLine(empty) = %q, want \"\"", got)
	}
	blank, err := Create("txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blank, []byte("\n\n   \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := FirstLine(blank); got != "" {
		t.Fatalf("FirstLine(blank) = %q, want \"\"", got)
	}
	if got := FirstLine(filepath.Join(dir, "missing.txt")); got != "" {
		t.Fatalf("FirstLine(missing) = %q, want \"\"", got)
	}
}

// TestRenameGuards is the boundary rail: Rename refuses paths outside the
// store, pathy or traversal names, and existing targets.
func TestRenameGuards(t *testing.T) {
	dir := sandbox(t)
	path, err := Create("txt")
	if err != nil {
		t.Fatal(err)
	}
	other, err := Create("txt")
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "victim.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Rename(outside, "x.txt"); err == nil {
		t.Fatal("a path outside the store must be refused")
	}
	for _, name := range []string{"", ".", "..", "sub/esc.txt", "../esc.txt"} {
		if _, err := Rename(path, name); err == nil {
			t.Fatalf("Rename(_, %q) must be refused", name)
		}
	}
	if _, err := Rename(path, filepath.Base(other)); err == nil {
		t.Fatal("an existing target must be refused")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the scratch must survive every refusal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.Base(other))); err != nil {
		t.Fatalf("the target must survive: %v", err)
	}
}

// TestCreateWithContentSeedsGeneratedFile covers the content-accepting
// creation path (#2134): the generator's document lands in the file instead of
// the language template, and allocation still counts up.
func TestCreateWithContentSeedsGeneratedFile(t *testing.T) {
	dir := sandbox(t)
	lang.Register(lang.Language{ID: "gentpl", Extensions: []string{"gtp"}, Template: "TEMPLATE\n"})

	body := []byte("id,name\n1,ada\n")
	first, err := CreateWithContent("gtp", body)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "scratch-1.gtp"); first != want {
		t.Fatalf("first = %q, want %q", first, want)
	}
	got, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("content = %q, want the generated document (not the template)", got)
	}

	// The next allocation counts up, and a plain Create still gets the
	// template — the content path must disturb neither.
	second, err := Create("gtp")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "scratch-2.gtp"); second != want {
		t.Fatalf("second = %q, want %q", second, want)
	}
	if data, err := os.ReadFile(second); err != nil || string(data) != "TEMPLATE\n" {
		t.Fatalf("template scratch = %q, %v; want the language template", data, err)
	}
}

// TestCreateWithContentEmptyStaysEmpty pins that an empty (non-nil) content
// means "empty file", not "fall back to the template".
func TestCreateWithContentEmptyStaysEmpty(t *testing.T) {
	sandbox(t)
	lang.Register(lang.Language{ID: "emptpl", Extensions: []string{"etp"}, Template: "TEMPLATE\n"})

	path, err := CreateWithContent("etp", []byte{})
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || len(data) != 0 {
		t.Fatalf("content = %q, %v; want an empty file", data, err)
	}
}
