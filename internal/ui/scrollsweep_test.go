package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// scrollsweep_test.go is the guard for the #2462 sweep, modelled on
// inputsweep_test.go: every list-shaped surface in the tree keeps its scroll
// window through the shared helpers here — ui.ClampWindow for a
// cursor-and-window clamp, ui.ScrollToShow for the window alone — so the
// window follows the cursor identically everywhere and, since #2462, is
// pulled back up when the list shrinks under it (no trailing blank rows).
//
// The check greps for the shape a hand-written clamp always has: the pair of
// "pull the window up to the cursor" / "push the window down to the cursor"
// lines that fifteen panes each carried their own copy of. It deliberately
// does not look at function names — a pane may well keep a clampScroll()
// wrapper, as long as its body delegates here.
//
// Adding a genuinely different scroller means adding it to allowedScroll with
// the reason, which is the review prompt this test exists to force.

// handRolledScroll matches the two lines of a hand-written window follow:
// "if m.top > m.cursor {" (pull up) and "m.top = m.cursor - h + 1" (push
// down), in whatever the surface calls its window offset and selection.
var handRolledScroll = regexp.MustCompile(
	`(?i)[\w.]*top *> *[\w.]*cursor|[\w.]*top *= *[\w.]*cursor[\w.]* *- *\w+ *\+ *1`)

// allowedScroll lists the files whose scroller is legitimately not the shared
// list window, with the reason each is exempt.
var allowedScroll = map[string]string{
	// Extracted into a shared hierarchy package by #2465, which adopts
	// ui.ScrollToShow there; kept out of the #2462 sweep to avoid the clash.
	"internal/callhier/callhier.go": "moving to the shared hierarchy package (#2465)",
	"internal/typehier/typehier.go": "moving to the shared hierarchy package (#2465)",
	// The explorer's scratch section scrolls independently of the tree above
	// it and has its own documented stability contract (scroll_stability_test.go):
	// the window is offset-anchored, not cursor-anchored, and a cursor of -1
	// (no scratch selected) must leave the offset alone.
	"internal/explorer/scratches.go": "offset-anchored section viewport with its own stability contract",
	// The editor viewport scrolls wrapped display lines around a buffer
	// position, not list rows around a selection index.
	"internal/editor/viewport/wrap.go": "wrapped-line viewport, not a row list",
	// zt/zz/zb park the editor viewport on the cursor line deliberately, with
	// their own scrolloff arithmetic; there is no list selection involved.
	"internal/editor/vimops.go": "vim scroll-position commands over buffer lines",
}

func TestNoHandRolledScrollClamps(t *testing.T) {
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
		if _, ok := allowedScroll[rel]; ok {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		for i, line := range strings.Split(string(src), "\n") {
			if handRolledScroll.MatchString(line) {
				offenders = append(offenders, rel+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(offenders) > 0 {
		t.Errorf("hand-rolled scroll clamp(s) outside internal/ui — call ui.ClampWindow "+
			"(cursor + window) or ui.ScrollToShow (window only) instead, or add the file to "+
			"allowedScroll with a reason (wiki/architecture/list-navigation.md):\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

// TestScrollAllowlistIsCurrent keeps allowedScroll honest: an entry whose file
// no longer matches the pattern is stale and must go, else the exemption
// silently covers whatever that file grows next.
func TestScrollAllowlistIsCurrent(t *testing.T) {
	root := filepath.Join("..", "..")
	for rel := range allowedScroll {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("allowedScroll entry %s: %v", rel, err)
			continue
		}
		if !handRolledScroll.Match(src) {
			t.Errorf("allowedScroll entry %s no longer matches the hand-rolled pattern — drop it", rel)
		}
	}
}
