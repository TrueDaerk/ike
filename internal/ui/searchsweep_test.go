package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// searchsweep_test.go is the guard for the #2461 sweep, modelled on
// inputsweep_test.go: every in-pane "/" search in the tree keeps its state
// and its prompt behaviour in ui.LineSearch, so typing narrows live, enter
// keeps the matches for n / N, esc drops them, the current match survives an
// edit on the nearest surviving position, and the prompt row reads the same
// everywhere.
//
// The check greps for the two shapes a hand-rolled search line always has:
// the state tuple (an "open prompt" flag, a rune caret beside the query, the
// wrap marker) and the walkers (searchKey / stepMatch / recomputeMatches and
// their siblings). A file that shows either shape without mentioning
// ui.LineSearch is an offender. Adding a genuinely different search means
// adding it to allowedSearch with the reason, which is the review prompt this
// test exists to force.

// handRolledSearch matches a search-line state field or walker declaration.
var handRolledSearch = regexp.MustCompile(
	`^\s+(searching|sEditing)\s+bool\b|^\s+(qpos|qcur|sCur)\s+int\b|^\s+sInput\s+string\b|^\s+wrapped\s+bool\b|` +
		`func \(\w+ \*?\w+\) (searchKey|stepMatch|recomputeMatches|searchStep|applySearch)\(`)

// sharedSearch is the mark of an adopter: the file holds (or embeds) the
// shared state, so its walkers are wrappers around it.
var sharedSearch = regexp.MustCompile(`\bui\.LineSearch\b`)

// allowedSearch lists the files whose search is legitimately not the shared
// in-pane search, with the reason each is exempt.
var allowedSearch = map[string]string{
	// The editor's own "/" is vim's: a regex-capable command line with its
	// own history bucket, direction, smartcase flag and n/N repeat over the
	// buffer cursor (internal/editor/search) — the thing the in-pane search
	// imitates, not an instance of it.
	"internal/editor/editor.go": "vim search command line over the buffer cursor",
	// Copy mode's search (#2162) is vim's "/" and "?" over a buffer cursor:
	// directional, accepted-then-repeated, with the query line separate from
	// the last accepted pattern — a different state machine, kept with the
	// mode it belongs to.
	"internal/terminal/copymode.go": "copy-mode / and ? — directional accept-then-repeat search over a buffer cursor",
	// A filter over a settings tree, not a jump-to-match search: the matches
	// are the rows left visible, and the step walks the cursor over them.
	"internal/settings/panel.go": "settings filter — steps the cursor over the filtered rows",
	// The TODO index filters its locations list; stepping walks the list.
	"internal/todoindex/todoindex.go": "todo index filter — steps the locations list",
	// The DOM inspector's matches are HTML nodes chosen by a CSS selector,
	// and a step emits an editor jump; no line query is involved.
	"internal/domview/domview.go": "CSS-selector node matches with an editor jump per step",
}

func TestNoHandRolledLineSearches(t *testing.T) {
	root := filepath.Join("..", "..")
	var offenders []string
	err := filepath.Walk(filepath.Join(root, "internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "internal/ui/") {
			return nil // the shared helpers themselves
		}
		if _, ok := allowedSearch[rel]; ok {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if sharedSearch.Match(src) {
			return nil // an adopter: its walkers wrap the shared state
		}
		for i, line := range strings.Split(string(src), "\n") {
			if handRolledSearch.MatchString(line) {
				offenders = append(offenders, rel+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(offenders) > 0 {
		t.Errorf("hand-rolled in-pane search(es) outside internal/ui — hold the state in "+
			"ui.LineSearch instead, or add the file to allowedSearch with a reason "+
			"(wiki/architecture/search.md § In-pane search):\n  %s", strings.Join(offenders, "\n  "))
	}
}

// TestSearchAllowlistIsCurrent keeps allowedSearch honest: an entry whose
// file no longer matches the pattern (or has since adopted the shared state)
// is stale and must go, else the exemption silently covers whatever that file
// grows next.
func TestSearchAllowlistIsCurrent(t *testing.T) {
	root := filepath.Join("..", "..")
	for rel := range allowedSearch {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("allowedSearch entry %s: %v", rel, err)
			continue
		}
		hit := false
		for _, line := range strings.Split(string(src), "\n") {
			if handRolledSearch.MatchString(line) {
				hit = true
				break
			}
		}
		if !hit || sharedSearch.Match(src) {
			t.Errorf("allowedSearch entry %s no longer matches the hand-rolled pattern — drop it", rel)
		}
	}
}
