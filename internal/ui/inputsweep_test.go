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

// handRolledPaste matches the hand-rolled paste-insertion shape #2460 found in
// nbview.go and hexview.go before they were routed through Field.Paste:
// "x = string(r[:cur]) + text + string(r[cur:])", splicing a pasted block
// into a rune slice by hand instead of calling ui.PasteText/Field.Paste.
var handRolledPaste = regexp.MustCompile(`string\(r\[:\w+\]\)\s*\+\s*text\s*\+`)

// hintSlice matches a self-referential slice that drops the last element —
// "x = x[:len(x)-1]", the shape a hand-rolled backspace takes — but only when
// the sliced identifier's name hints at a text-input field (Input, Query,
// Filter, Text, Find, Repl, Program, Pattern). The two capture groups are
// compared in code below: a Go regexp cannot backreference a group, so the
// pattern only narrows candidates and handRolledBackspace.match confirms the
// identifier is the same on both sides. Unhinted identifiers (a stack pop, an
// undo list's ops slice, "parts", "steps") are deliberately not flagged —
// this guard is for text fields, not every self-shrinking slice in the tree.
var hintSlice = regexp.MustCompile(`(?i)([\w.]*(?:input|query|filter|text|find|repl|program|pattern)[\w.]*)\[:len\(([\w.]*)\)-1\]`)

// handRolledBackspace reports whether line hand-slices a hinted text field's
// last rune off the end and reassigns it to itself, in place of Field.Key
// (backspace already comes free through ui.EditKey/Field.Key).
func handRolledBackspace(line string) bool {
	for _, g := range hintSlice.FindAllStringSubmatch(line, -1) {
		if g[1] == g[2] {
			return true
		}
	}
	return false
}

// allowed lists the files that legitimately read a key's printable text, hand-
// splice a pasted block, or hand-slice a hinted field without being a
// single-line input, with the reason each is exempt.
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
	"internal/editor/keys_command.go":  "preselect replace-on-type, EditKey does the insert",
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
			if handRolled.MatchString(line) || handRolledPaste.MatchString(line) || handRolledBackspace(line) {
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
		matches := handRolled.Match(src) || handRolledPaste.Match(src)
		if !matches {
			for _, line := range strings.Split(string(src), "\n") {
				if handRolledBackspace(line) {
					matches = true
					break
				}
			}
		}
		if !matches {
			t.Errorf("allowed entry %s no longer matches any hand-rolled pattern — drop it", rel)
		}
	}
}
