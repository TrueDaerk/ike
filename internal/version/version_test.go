package version

import (
	"runtime"
	"strings"
	"testing"
)

// restore puts the build-stamp vars back after a test rewrites them; they are
// package-level because -ldflags can only reach package-level strings.
func restore(t *testing.T) {
	t.Helper()
	commit, dirty := Commit, Dirty
	t.Cleanup(func() { Commit, Dirty = commit, dirty })
}

func TestShortIsJustTheNumber(t *testing.T) {
	if strings.ContainsAny(Short(), " ()") {
		t.Fatalf("Short() = %q, want a bare version number", Short())
	}
	if strings.Count(Short(), ".") != 2 {
		t.Fatalf("Short() = %q, want major.minor.patch", Short())
	}
}

// TestFullWithoutBuildStamp covers a plain `go build`: no commit is stamped, so
// the banner must not render an empty "()" pair.
func TestFullWithoutBuildStamp(t *testing.T) {
	restore(t)
	Commit, Dirty = "", ""

	got := Full()
	if strings.Contains(got, "(") || strings.Contains(got, ")") {
		t.Fatalf("Full() = %q, want no parenthesised stamp without a commit", got)
	}
	for _, want := range []string{"ike", Version, runtime.Version(), runtime.GOOS, runtime.GOARCH} {
		if !strings.Contains(got, want) {
			t.Fatalf("Full() = %q, missing %q", got, want)
		}
	}
}

func TestFullWithCommit(t *testing.T) {
	restore(t)
	Commit, Dirty = "a1b2c3d", ""

	if got := Full(); !strings.Contains(got, "(a1b2c3d)") {
		t.Fatalf("Full() = %q, want the commit in parentheses", got)
	}
}

// TestFullVariantMarksReducedBuild: the complexity-only build (#2939) ends
// its banner with the variant; the full build's banner carries none.
func TestFullVariantMarksReducedBuild(t *testing.T) {
	restore(t)
	Commit, Dirty = "a1b2c3d", ""

	got := FullVariant("complexity-only")
	if !strings.HasSuffix(got, " (complexity-only)") || !strings.HasPrefix(got, Full()) {
		t.Fatalf("FullVariant() = %q, want Full() + \" (complexity-only)\"", got)
	}
	if FullVariant("") != Full() {
		t.Fatalf("FullVariant(\"\") = %q, want Full() = %q", FullVariant(""), Full())
	}
}

// TestFullMarksDirty guards the case that matters when someone reports a bug
// from a local build: the banner has to say the tree was not clean.
func TestFullMarksDirty(t *testing.T) {
	restore(t)
	Commit, Dirty = "a1b2c3d", "true"

	if got := Full(); !strings.Contains(got, "(a1b2c3d, dirty)") {
		t.Fatalf("Full() = %q, want the dirty marker", got)
	}
}

// TestDirtyWithoutCommitStaysQuiet: the Makefile always sets both or neither,
// but a hand-rolled -ldflags line might set only Dirty. Rendering ", dirty"
// with nothing to attach it to would be noise.
func TestDirtyWithoutCommitStaysQuiet(t *testing.T) {
	restore(t)
	Commit, Dirty = "", "true"

	if got := Full(); strings.Contains(got, "dirty") {
		t.Fatalf("Full() = %q, want no stamp when there is no commit", got)
	}
}
