package pathcomplete

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tree builds a fixture directory: Development/, Downloads/, design.txt,
// notes.txt, .hidden/.
func tree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"Development", "Downloads", ".hidden"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"design.txt", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(root, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func sep() string { return string(filepath.Separator) }

func TestCompleteExtendsToCommonPrefix(t *testing.T) {
	root := tree(t)
	r := Complete(filepath.Join(root, "D"))
	// Development / Downloads share only "D": the input is already the
	// common prefix, so Completed stays put and both candidates are offered.
	if got, want := r.Completed, filepath.Join(root, "D"); got != want {
		t.Fatalf("Completed = %q, want %q (ambiguous D)", got, want)
	}
	if len(r.Candidates) != 2 {
		t.Fatalf("Candidates = %v, want Development/ and Downloads/", r.Candidates)
	}
	for _, c := range r.Candidates {
		if !strings.HasSuffix(c, sep()) {
			t.Fatalf("directory candidate %q lacks trailing separator", c)
		}
	}
}

func TestCompleteSingleDirGetsSeparator(t *testing.T) {
	root := tree(t)
	r := Complete(filepath.Join(root, "Dev"))
	if want := filepath.Join(root, "Development") + sep(); r.Completed != want {
		t.Fatalf("Completed = %q, want %q", r.Completed, want)
	}
	if len(r.Candidates) != 1 {
		t.Fatalf("Candidates = %v", r.Candidates)
	}
}

func TestCompleteCaseInsensitiveFallback(t *testing.T) {
	root := tree(t)
	r := Complete(filepath.Join(root, "dev"))
	if want := filepath.Join(root, "Development") + sep(); r.Completed != want {
		t.Fatalf("Completed = %q, want %q", r.Completed, want)
	}
}

func TestCompleteExactCaseBeatsFolded(t *testing.T) {
	root := tree(t)
	// "de" matches design.txt exactly; Development only case-folded.
	r := Complete(filepath.Join(root, "de"))
	if len(r.Candidates) != 1 || !strings.HasSuffix(r.Candidates[0], "design.txt") {
		t.Fatalf("Candidates = %v, want design.txt only", r.Candidates)
	}
}

func TestCompleteHiddenNeedsExplicitDot(t *testing.T) {
	root := tree(t)
	if r := Complete(root + sep()); len(r.Candidates) != 4 {
		t.Fatalf("Candidates = %v, hidden entry should be excluded", r.Candidates)
	}
	r := Complete(root + sep() + ".")
	if len(r.Candidates) != 1 || !strings.HasSuffix(r.Candidates[0], ".hidden"+sep()) {
		t.Fatalf("Candidates = %v, want .hidden/", r.Candidates)
	}
}

func TestDirsOnly(t *testing.T) {
	root := tree(t)
	r := Dirs(root + sep())
	if len(r.Candidates) != 2 {
		t.Fatalf("Candidates = %v, want the two directories", r.Candidates)
	}
	for _, c := range r.Candidates {
		if !strings.HasSuffix(c, sep()) {
			t.Fatalf("candidate %q lacks trailing separator", c)
		}
	}
}

func TestCompleteNoMatchKeepsInput(t *testing.T) {
	root := tree(t)
	in := filepath.Join(root, "zzz")
	r := Complete(in)
	if r.Completed != in || r.Candidates != nil {
		t.Fatalf("no-match should keep input: %+v", r)
	}
}

func TestCompleteEmptyAndBadDir(t *testing.T) {
	if r := Complete(""); r.Completed != "" || r.Candidates != nil {
		t.Fatalf("empty input: %+v", r)
	}
	in := filepath.Join(string(filepath.Separator), "no-such-dir-ike-test", "x")
	if r := Complete(in); r.Completed != in || r.Candidates != nil {
		t.Fatalf("unreadable dir should keep input: %+v", r)
	}
}

func TestCompletePreservesTildeNotation(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir")
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Skip("home unreadable")
	}
	var name string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			name = e.Name()
			break
		}
	}
	if name == "" {
		t.Skip("home has no visible entries")
	}
	r := Complete("~" + sep() + name[:1])
	if len(r.Candidates) == 0 {
		t.Fatalf("expected candidates under ~%s for prefix %q", sep(), name[:1])
	}
	for _, c := range r.Candidates {
		if !strings.HasPrefix(c, "~"+sep()) {
			t.Fatalf("candidate %q lost the ~ notation", c)
		}
	}
	if !strings.HasPrefix(r.Completed, "~"+sep()) {
		t.Fatalf("Completed %q lost the ~ notation", r.Completed)
	}
}

func TestExpand(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir")
	}
	if got := Expand("~"); got != home {
		t.Fatalf("Expand(~) = %q, want %q", got, home)
	}
	if got, want := Expand("~/x"), filepath.Join(home, "x"); got != want {
		t.Fatalf("Expand(~/x) = %q, want %q", got, want)
	}
	if got := Expand("~user/x"); got != "~user/x" {
		t.Fatalf("Expand(~user/x) = %q, want unchanged", got)
	}
	if got := Expand("/abs"); got != "/abs" {
		t.Fatalf("Expand(/abs) = %q, want unchanged", got)
	}
}

func TestCommonPrefixMixedCase(t *testing.T) {
	// F/f and B/b fold together and 'a' matches both; the divergence is r/z.
	if got := commonPrefix([]string{"Foobar", "fooBaz"}); !strings.EqualFold(got, "fooba") {
		t.Fatalf("commonPrefix = %q, want case-fold fooba", got)
	}
	if got := commonPrefix([]string{"abc"}); got != "abc" {
		t.Fatalf("commonPrefix single = %q", got)
	}
}
