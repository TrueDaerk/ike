package ui_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// inputsweep_test.go is the guard for the #2002 sweep: every single-line text
// input in the tree edits through the shared helpers in internal/ui
// (EditKey/PasteText/CursorView), so paste, cursor and word motions, the
// word/line kills and the macOS opt/cmd chords behave the same everywhere.
//
// The check is deliberately crude — it greps for the two shapes a hand-rolled
// input always has, printable insertion straight off a key message — because
// the failure it prevents is a new field quietly re-implementing a subset of
// EditKey. Adding a genuinely non-input use means adding it to allowed below
// with the reason, which is the review prompt this test exists to force.

// handRolled matches printable-text insertion taken directly from a key
// message: "s += key.Text" (append-only field) and the "key.Text != """
// guard that fronts a hand-written insertion branch.
var handRolled = regexp.MustCompile(`\+= *(msg|key|k|m)\.Text\b|(msg|key|k)\.Text != ""`)

// allowed lists the files that legitimately read a key's printable text
// without being a single-line input, with the reason each is exempt.
var allowed = map[string]string{
	// The document buffer, not a one-line field: insertion goes through the
	// editor's own multi-line edit path (undo grouping, auto-indent, pairs).
	"internal/editor/keys_insert.go": "editor buffer insertion",
	// The terminal encodes keys for the pty; the shell owns the line editing.
	"internal/terminal/model.go": "pty key encoding",
	// Both of these front a preselected-prefill branch: they only decide that
	// a typed rune replaces the selection, then hand the actual insertion to
	// ui.EditKey below.
	"internal/explorer/fileops.go":     "preselect replace-on-type, EditKey does the insert",
	"internal/editor/replace_panel.go": "preselect replace-on-type, EditKey does the insert",
}

func TestNoHandRolledTextInputs(t *testing.T) {
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
		if _, ok := allowed[rel]; ok {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		for i, line := range strings.Split(string(src), "\n") {
			if handRolled.MatchString(line) {
				offenders = append(offenders, rel+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(offenders) > 0 {
		t.Errorf("hand-rolled text input(s) outside internal/ui — route the field through "+
			"ui.EditKey/ui.PasteText instead, or add the file to allowed with a reason "+
			"(wiki/architecture/ui/text-input.md):\n  %s", strings.Join(offenders, "\n  "))
	}
}

// TestAllowlistIsCurrent keeps allowed honest: an entry whose file no longer
// matches the pattern is stale and must go, else the exemption silently
// covers whatever that file grows next.
func TestAllowlistIsCurrent(t *testing.T) {
	root := filepath.Join("..", "..")
	for rel := range allowed {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("allowed entry %s: %v", rel, err)
			continue
		}
		if !handRolled.Match(src) {
			t.Errorf("allowed entry %s no longer matches the hand-rolled pattern — drop it", rel)
		}
	}
}
