// Package unidiff parses the unified diff format (#1630) for the .diff/.patch
// language: line classification (added/removed/context, @@ hunk headers, file
// headers, git extension headers), fold ranges on hunk and file boundaries,
// and word-level emphasis between paired removed/added lines using the same
// rune-level Myers refinement the diff views use (internal/diff).
//
// The format has no Tree-sitter grammar here on purpose: unified diff is line
// oriented and stateful (a line reading "--- x" is a removed line inside a
// hunk body but a file header outside one), which a pure-Go pass over the @@
// counts classifies exactly — the hunk header's line counts say how many body
// lines follow, so content is never mistaken for structure.
//
// Like every lang.Spans/lang.Folds producer it runs on each highlight pass
// and stays a single O(lines) sweep; word-level refinement is bounded per
// line pair by diff's maxRefineRunes cap.
package unidiff

import (
	"strconv"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"ike/internal/diff"
	"ike/internal/lang"
)

// Capture names the theme resolves (derived from existing captures in
// highlight's theme layer, override-able via theme.captures.diff.*).
const (
	CapturePlus   = "diff.plus"   // added lines (+)
	CaptureMinus  = "diff.minus"  // removed lines (-)
	CaptureDelta  = "diff.delta"  // @@ hunk headers
	CaptureHeader = "diff.header" // file headers: diff --git, ---, +++
	CaptureMeta   = "diff.meta"   // git extension headers, \ No newline marker

	// PlusEmph / MinusEmph mark the word-level changed range inside a paired
	// added/removed line. The dotted-prefix fallback resolves their foreground
	// to the line's own capture; the editor layers the diff viewer's
	// changed-range background underneath (styleAt in internal/editor).
	PlusEmph  = "diff.plus.emph"
	MinusEmph = "diff.minus.emph"
)

// wordOff gates word-level emphasis (editor.diff_word_highlight, default on).
// An atomic because parses run on background goroutines while config reloads
// flip the toggle on the UI loop — the same shape as highlight's rainbowOff.
var wordOff atomic.Bool

// SetWordHighlight enables/disables word-level emphasis; applied on the next
// parse.
func SetWordHighlight(on bool) { wordOff.Store(!on) }

// WordHighlightEnabled reports whether word-level emphasis is active.
func WordHighlightEnabled() bool { return !wordOff.Load() }

// kind classifies one line of a unified diff.
type kind uint8

const (
	kindText    kind = iota // prose outside the diff structure (commit message …)
	kindHeader              // file headers: "diff ", "--- ", "+++ "
	kindMeta                // git extension headers, "\ No newline at end of file"
	kindHunk                // "@@ -l,c +l,c @@" hunk headers
	kindMinus               // removed line inside a hunk body
	kindPlus                // added line inside a hunk body
	kindContext             // unchanged line inside a hunk body
)

// metaPrefixes are the git extension headers (#1630) that appear between a
// "diff --git" line and the first hunk. Matched only outside hunk bodies, so
// content never collides.
var metaPrefixes = []string{
	"index ",
	"old mode",
	"new mode",
	"new file mode",
	"deleted file mode",
	"similarity index",
	"dissimilarity index",
	"rename from",
	"rename to",
	"copy from",
	"copy to",
	"Binary files ",
	"GIT binary patch",
}

// doc is one parsed buffer: the per-line classification plus the fold ranges
// it implies.
type doc struct {
	kinds []kind
	folds []lang.FoldRange // pre-order: each file section before its hunks
}

// parse classifies every line in a single sweep. Hunk bodies are consumed by
// the @@ header's line counts — the exact rule of the format — so a body line
// starting with "--- " stays a removed line, never a file header.
func parse(lines []string) doc {
	d := doc{kinds: make([]kind, len(lines))}
	oldLeft, newLeft := 0, 0 // body lines still owed to the open hunk
	hunkStart, fileStart := -1, -1
	var hunks []lang.FoldRange
	closeHunk := func(end int) {
		if hunkStart >= 0 && end > hunkStart {
			hunks = append(hunks, lang.FoldRange{HeaderLine: hunkStart, EndLine: end})
		}
		hunkStart = -1
	}
	closeFile := func(end int) {
		if fileStart >= 0 && end > fileStart {
			d.folds = append(d.folds, lang.FoldRange{HeaderLine: fileStart, EndLine: end})
			// The file's hunks follow their section header: pre-order.
			d.folds = append(d.folds, hunks...)
		} else {
			d.folds = append(d.folds, hunks...)
		}
		hunks = hunks[:0]
		fileStart = -1
	}
	for i, ln := range lines {
		if oldLeft > 0 || newLeft > 0 {
			switch {
			case strings.HasPrefix(ln, "-"):
				d.kinds[i] = kindMinus
				oldLeft--
			case strings.HasPrefix(ln, "+"):
				d.kinds[i] = kindPlus
				newLeft--
			case strings.HasPrefix(ln, `\`):
				// "\ No newline at end of file" — annotates the previous
				// line, counts toward neither side.
				d.kinds[i] = kindMeta
			case ln == "" || strings.HasPrefix(ln, " "):
				// Some tools strip the trailing space off empty context lines.
				d.kinds[i] = kindContext
				oldLeft--
				newLeft--
			default:
				// Malformed body (truncated patch): abandon the hunk and let
				// the ordinary classification below read this line.
				oldLeft, newLeft = 0, 0
				closeHunk(i - 1)
				goto outside
			}
			if oldLeft <= 0 && newLeft <= 0 {
				closeHunk(i)
			}
			continue
		}
	outside:
		switch {
		case strings.HasPrefix(ln, "@@ -"):
			o, n, ok := parseHunkHeader(ln)
			if !ok {
				d.kinds[i] = kindText
				continue
			}
			d.kinds[i] = kindHunk
			oldLeft, newLeft = o, n
			if oldLeft > 0 || newLeft > 0 {
				hunkStart = i
			}
		case strings.HasPrefix(ln, "diff "):
			closeFile(i - 1)
			fileStart = i
			d.kinds[i] = kindHeader
		case strings.HasPrefix(ln, "--- ") || strings.HasPrefix(ln, "+++ "):
			d.kinds[i] = kindHeader
		case strings.HasPrefix(ln, `\`):
			// Trailing "\ No newline" lands after the counts ran out; keep it
			// inside the hunk's fold so collapsing hides it too.
			d.kinds[i] = kindMeta
			if n := len(hunks); n > 0 && hunks[n-1].EndLine == i-1 {
				hunks[n-1].EndLine = i
			}
		case hasMetaPrefix(ln):
			d.kinds[i] = kindMeta
		default:
			d.kinds[i] = kindText
		}
	}
	closeHunk(len(lines) - 1)
	closeFile(len(lines) - 1)
	return d
}

// hasMetaPrefix reports whether a non-body line is a git extension header.
func hasMetaPrefix(ln string) bool {
	for _, p := range metaPrefixes {
		if strings.HasPrefix(ln, p) {
			return true
		}
	}
	return false
}

// parseHunkHeader reads "@@ -start[,count] +start[,count] @@ …" and returns
// both counts (an omitted count means 1, per the format).
func parseHunkHeader(ln string) (oldN, newN int, ok bool) {
	rest, ok := cutRange(ln[len("@@ -"):], &oldN)
	if !ok {
		return 0, 0, false
	}
	rest, ok = strings.CutPrefix(rest, " +")
	if !ok {
		return 0, 0, false
	}
	rest, ok = cutRange(rest, &newN)
	if !ok || !strings.HasPrefix(rest, " @@") {
		return 0, 0, false
	}
	return oldN, newN, true
}

// cutRange consumes "start[,count]" from s, stores the count and returns the
// remainder.
func cutRange(s string, count *int) (string, bool) {
	i := digits(s, 0)
	if i == 0 {
		return s, false
	}
	*count = 1
	if i < len(s) && s[i] == ',' {
		j := digits(s, i+1)
		if j == i+1 {
			return s, false
		}
		n, err := strconv.Atoi(s[i+1 : j])
		if err != nil {
			return s, false
		}
		*count = n
		i = j
	}
	return s[i:], true
}

// digits returns the index just past the run of ASCII digits starting at i.
func digits(s string, i int) int {
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i
}

// Spans is the lang.Language.Spans hook: whole-line coloring per line kind,
// with word-level emphasis spans for paired -/+ lines prepended so they win
// where both cover a cell (CaptureAt is first-covering-wins).
func Spans(lines []string) []lang.Span {
	d := parse(lines)
	var out []lang.Span
	if WordHighlightEnabled() {
		out = wordSpans(lines, d.kinds)
	}
	for i, k := range d.kinds {
		var capture string
		switch k {
		case kindHeader:
			capture = CaptureHeader
		case kindMeta:
			capture = CaptureMeta
		case kindHunk:
			capture = CaptureDelta
		case kindMinus:
			capture = CaptureMinus
		case kindPlus:
			capture = CapturePlus
		default:
			continue
		}
		n := utf8.RuneCountInString(lines[i])
		if n == 0 {
			continue
		}
		out = append(out, lang.Span{Line: i, EndCol: n, Capture: capture})
	}
	return out
}

// Folds is the lang.Language.Folds hook: every hunk folds behind its @@
// header, every file section behind its "diff" header, in pre-order.
func Folds(lines []string) []lang.FoldRange {
	return parse(lines).folds
}

// wordSpans pairs each run of consecutive removed lines with the added run
// that immediately follows it — the classic word-diff pairing — and refines
// each i-th pair rune-level. Offsets shift by one column for the -/+ marker.
func wordSpans(lines []string, kinds []kind) []lang.Span {
	var out []lang.Span
	var minus, plus []int
	flush := func() {
		n := min(len(minus), len(plus))
		for j := 0; j < n; j++ {
			ls, rs := diff.Refine(lines[minus[j]][1:], lines[plus[j]][1:])
			for _, s := range ls {
				out = append(out, lang.Span{Line: minus[j], StartCol: s.Start + 1, EndCol: s.End + 1, Capture: MinusEmph})
			}
			for _, s := range rs {
				out = append(out, lang.Span{Line: plus[j], StartCol: s.Start + 1, EndCol: s.End + 1, Capture: PlusEmph})
			}
		}
		minus, plus = minus[:0], plus[:0]
	}
	for i, k := range kinds {
		switch k {
		case kindMinus:
			if len(plus) > 0 {
				flush()
			}
			minus = append(minus, i)
		case kindPlus:
			if len(minus) == 0 {
				continue
			}
			plus = append(plus, i)
		case kindMeta:
			// "\ No newline" between a -run and its +run keeps the pairing.
		default:
			flush()
		}
	}
	flush()
	return out
}
