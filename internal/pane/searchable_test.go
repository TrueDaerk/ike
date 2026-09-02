package pane

import (
	"os"
	"path/filepath"
	"testing"
)

// searchable_test.go covers #2409: every pane kind that has a search of its
// own answers OpenSearch, and the ones that have none report false so the root
// model can say so instead of swallowing the chord.

// tempFile writes name under a fresh temp dir and returns its path.
func tempFile(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestOpenSearchPerKind is the table over pane kinds: OpenSearch reaches the
// pane's own search (opened == true) or the kind has none (nil capability).
func TestOpenSearchPerKind(t *testing.T) {
	md := tempFile(t, "notes.md", "# Title\n\nbody\n")
	csv := tempFile(t, "rows.csv", "a,b\n1,2\n")

	cases := []struct {
		name string
		make func(r *Registry) *Instance
		want bool // OpenSearch reports the search opened
	}{
		{"explorer", func(r *Registry) *Instance { return r.Get(r.AddExplorer()) }, true},
		{"problems", func(r *Registry) *Instance { return r.Get(r.AddProblems()) }, true},
		{"usages", func(r *Registry) *Instance { return r.Get(r.AddUsages()) }, true},
		{"http", func(r *Registry) *Instance { return r.Get(r.AddHTTP()) }, true},
		{"issues", func(r *Registry) *Instance { return r.Get(r.AddIssues()) }, true},
		{"dom", func(r *Registry) *Instance { return r.Get(r.AddDOM()) }, true},
		{"diff", func(r *Registry) *Instance { return r.Get(r.AddDiff("", "")) }, true},
		{"archive", func(r *Registry) *Instance { return r.Get(r.AddArchiveView(md)) }, true},
		{"markdown", func(r *Registry) *Instance { return r.Get(r.AddMarkdownPreview(md)) }, true},
		// The data viewer only filters once a table is selected; a viewer over
		// a file that never loaded reports "no search right now" rather than
		// opening an input over nothing.
		{"data", func(r *Registry) *Instance { return r.Get(r.AddDataView(csv)) }, false},
		// Kinds with no search of their own.
		{"structure", func(r *Registry) *Instance { return r.Get(r.AddStructure()) }, false},
		{"vcs", func(r *Registry) *Instance { return r.Get(r.AddVCS()) }, false},
		{"breakpoints", func(r *Registry) *Instance { return r.Get(r.AddBreakpoints()) }, false},
		{"tests", func(r *Registry) *Instance { return r.Get(r.AddTests()) }, false},
		{"image", func(r *Registry) *Instance { return r.Get(r.AddImagePreview(md)) }, false},
		// A plain editor pane keeps editor.find on the chord, so the pane
		// capability is deliberately absent.
		{"editor", func(r *Registry) *Instance { return r.Get(r.AddEditor()) }, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inst := tc.make(newReg())
			if got := inst.OpenSearch(); got != tc.want {
				t.Fatalf("OpenSearch() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestOpenSearchOpensTheExplorerSpeedSearch checks the capability really
// reaches the pane's own state, not just a truthful return value.
func TestOpenSearchOpensTheExplorerSpeedSearch(t *testing.T) {
	r := newReg()
	inst := r.Get(r.AddExplorer())
	if inst.Explorer().Searching() {
		t.Fatal("the speed search starts closed")
	}
	if !inst.OpenSearch() {
		t.Fatal("OpenSearch must open the explorer speed search")
	}
	if !inst.Explorer().Searching() {
		t.Fatal("the speed search did not open")
	}
}

// TestOpenSearchFollowsTheActiveContentTab: a pane hosting a viewer in a tab
// (#1778) searches what it shows, not what its own kind is.
func TestOpenSearchFollowsTheActiveContentTab(t *testing.T) {
	md := tempFile(t, "notes.md", "# Title\n\nbody\n")
	r := newReg()
	host := r.Get(r.AddEditor())
	if host.OpenSearch() {
		t.Fatal("a plain editor tab has no pane search")
	}
	src := r.Get(r.AddMarkdownPreview(md))
	if !host.AddContentTab(src) {
		t.Fatal("the markdown preview must be adoptable as a content tab")
	}
	if !host.OpenSearch() {
		t.Fatal("the host must delegate to the active content tab")
	}
}
