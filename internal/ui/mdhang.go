package ui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// listMarkerRe matches a rendered list-item line: leading indent, the marker
// glamour emits for the item (bullet, "12.", or a task checkbox), a single
// separating space, and the item's text. It is matched against the ANSI-
// stripped line, so the indent is plain spaces.
var listMarkerRe = regexp.MustCompile(`^( *)(•|\d{1,9}\.|\[[ xX✓✗]\]) (\S.*)$`)

// HangingIndent gives wrapped list items in glamour output a hanging indent:
// the continuation lines of an item are re-aligned under the item's text
// instead of under its bullet or number, and the item's own leading indent is
// kept.
//
// Why post-processing and not a style tweak: glamour has no notion of a
// hanging indent. A list item's text is rendered as an inline paragraph after
// a marker prefix (ansi.ItemElement), and the only indentation knobs its style
// config offers — List.Indent, List.LevelIndent, List.Margin — are block
// level: ansi.MarginWriter applies them to *every* line of the block alike, so
// raising them shifts the marker along with the continuation lines and never
// separates the two. Glamour also exposes no hook to replace its list element,
// so aligning the wrap has to happen on the rendered text. Since glamour has
// already word-wrapped at `wrap` cells, this re-joins each item's lines and
// re-wraps them at the narrower width the hanging indent leaves.
//
// rendered must be glamour output — the per-line padding it emits is what
// tells this the width the text was really wrapped at — and wrap the value
// passed to glamour.WithWordWrap. ANSI styling is preserved; lines that are
// not list items are returned untouched.
func HangingIndent(rendered string, wrap int) string {
	if wrap < 1 || rendered == "" {
		return rendered
	}
	lines := strings.Split(rendered, "\n")
	// Glamour pads every block line out to the width it actually wrapped at,
	// which is narrower than `wrap` — it reserves the document margin on the
	// right as well as the left. Reading the widest padded line back gives
	// that effective width without hard-coding the margin, and the word-wrap
	// invariant below only holds against it.
	eff := 0
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > eff {
			eff = w
		}
	}
	if eff < 1 || eff > wrap {
		eff = wrap
	}
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); {
		line := trimTrailingCells(lines[i])
		plain := ansi.Strip(line)
		m := listMarkerRe.FindStringSubmatch(plain)
		if m == nil {
			out = append(out, lines[i])
			i++
			continue
		}
		indent := len(m[1])
		// The marker plus the single space that follows it: the column the
		// item's text starts at, and so the hanging indent for its wrapped
		// remainder. For ordered lists that is past the number, not under it.
		hang := indent + ansi.StringWidth(m[2]) + 1

		texts := []string{ansi.TruncateLeft(line, hang, "")}
		prevW := ansi.StringWidth(plain)
		j := i + 1
		for ; j < len(lines); j++ {
			next := trimTrailingCells(lines[j])
			np := ansi.Strip(next)
			if strings.TrimSpace(np) == "" {
				break
			}
			if lead := len(np) - len(strings.TrimLeft(np, " ")); lead != indent {
				break
			}
			if listMarkerRe.MatchString(np) {
				break
			}
			// The word-wrap invariant: a line glamour wrapped off the previous
			// one starts with a word that could not have fit on it. Anything
			// that would have fit is a separate block (a code line at the same
			// indent, say) and must not be folded into this item.
			word := strings.Fields(np)[0]
			if prevW+1+ansi.StringWidth(word) <= eff {
				break
			}
			texts = append(texts, ansi.TruncateLeft(next, indent, ""))
			prevW = ansi.StringWidth(np)
		}
		if len(texts) == 1 {
			// The item fit on one line; leave glamour's output alone.
			out = append(out, lines[i])
			i++
			continue
		}
		avail := eff - hang
		if avail < 1 {
			// Pathologically narrow pane: keep the text as glamour wrapped it
			// rather than emit one cell per line.
			out = append(out, lines[i:j]...)
			i = j
			continue
		}
		wrapped := strings.Split(ansi.Wrap(strings.Join(texts, " "), avail, ""), "\n")
		out = append(out, ansi.Truncate(line, hang, "")+wrapped[0])
		pad := strings.Repeat(" ", hang)
		for _, w := range wrapped[1:] {
			out = append(out, pad+w)
		}
		i = j
	}
	return strings.Join(out, "\n")
}

// trimTrailingCells drops trailing spaces from a styled line. Glamour pads
// every line out to the wrap width with styled spaces; that padding would
// otherwise be folded into the item text when lines are re-joined.
func trimTrailingCells(s string) string {
	p := ansi.Strip(s)
	t := strings.TrimRight(p, " ")
	if len(t) == len(p) {
		return s
	}
	return ansi.Truncate(s, ansi.StringWidth(t), "")
}
