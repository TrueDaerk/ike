package settings

import (
	"fmt"
	"path/filepath"
	"strings"

	"ike/internal/pathcomplete"
)

// pathsuggest.go is the settings-side glue for the shared path completion
// engine (#541): a tiny state holder the inline path inputs (the toolchain
// custom-path input, Path-type entries) embed. The hosting input refreshes it
// on every edit, applies tab through complete, and renders lines under the
// input while candidates exist.

// maxSuggestLines caps the rendered suggestion rows; the remainder collapses
// into a "+N more" tail so an ambiguous prefix cannot flood the page.
const maxSuggestLines = 8

// pathSuggest holds the live candidate list for one inline path input.
type pathSuggest struct {
	candidates []string
}

// refresh recomputes the candidates for the current input.
func (s *pathSuggest) refresh(input string) {
	s.candidates = pathcomplete.Complete(input).Candidates
}

// complete applies a tab press: it returns the input extended to the longest
// unambiguous prefix and refreshes the candidates for the new input.
func (s *pathSuggest) complete(input string) string {
	out := pathcomplete.Complete(input).Completed
	s.refresh(out)
	return out
}

// clear drops the candidates (input closed or committed).
func (s *pathSuggest) clear() { s.candidates = nil }

// lines returns the rows to render under the input, indented to align with
// inline detail rows. Every candidate shares the typed directory, so only the
// final path component is shown (a directory keeps its trailing separator) —
// long absolute prefixes would otherwise truncate away the distinguishing
// part. Empty when there is nothing to suggest.
func (s *pathSuggest) lines() []string {
	if len(s.candidates) == 0 {
		return nil
	}
	n := len(s.candidates)
	shown := n
	if shown > maxSuggestLines {
		shown = maxSuggestLines
	}
	out := make([]string, 0, shown+1)
	for _, c := range s.candidates[:shown] {
		out = append(out, "     "+lastComponent(c))
	}
	if n > shown {
		out = append(out, fmt.Sprintf("     … +%d more", n-shown))
	}
	return out
}

// lastComponent is the candidate's final path element, keeping a directory's
// trailing separator.
func lastComponent(c string) string {
	sep := string(filepath.Separator)
	if i := strings.LastIndexByte(strings.TrimSuffix(c, sep), filepath.Separator); i >= 0 {
		return c[i+1:]
	}
	return c
}
