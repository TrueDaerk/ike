package editor

import (
	"strings"

	"ike/internal/diff"
	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
)

// vcs_revert.go implements vcs.revertHunk (#555), JetBrains' "Rollback Lines"
// for the terminal: the contiguous change under the caret — the same region
// the gutter diff markers (#464) show — is restored to its HEAD content. The
// replacement goes through mutate(), so it is one change in the undo tree and
// plain editor undo brings the hunk back.

// RevertHunkUnderCursor restores the changed region under the cursor to its
// HEAD content and reports whether a hunk was found there. The buffer turns
// dirty like any edit; the caller refreshes the gutter marks.
func (m *Model) RevertHunkUnderCursor(head string) bool {
	h, ok := hunkAt(head, m.buf.String(), m.cursor.Line)
	if !ok {
		return false
	}
	m.mutate(func(rec *history.Recorder) buffer.Position {
		return applyHunkRevert(m.buf, rec, h)
	})
	return true
}

// revertHunk is one contiguous change against HEAD, resolved to buffer terms:
// the 0-based buffer-line range to replace (rightStart > rightEnd marks a
// pure deletion, restored by inserting above anchor), and the HEAD lines that
// belong in its place.
type revertHunk struct {
	rightStart, rightEnd int      // buffer lines the hunk occupies, inclusive
	anchor               int      // pure deletion: insert before this line (LineCount = append)
	headLines            []string // HEAD content of the hunk; empty for pure additions
}

// pureDeletion reports whether the hunk removes HEAD lines without owning any
// buffer line of its own.
func (h revertHunk) pureDeletion() bool { return h.rightEnd < h.rightStart }

// hunkAt diffs head against buf and returns the hunk whose gutter-marked
// lines (added/changed lines plus deletion anchors, folded at EOF exactly
// like vcs.LineMarks) contain the 0-based buffer line.
func hunkAt(head, buf string, line int) (revertHunk, bool) {
	if head == buf {
		return revertHunk{}, false
	}
	res := diff.Compute(head, buf)

	// hunkOfRow maps a row index to its hunk index (-1 between hunks).
	hunkOfRow := func(i int) int {
		for hi, h := range res.Hunks {
			if i >= h.Start && i < h.End {
				return hi
			}
		}
		return -1
	}

	// Replay the LineMarks walk, remembering which hunk marked each line so
	// the caret hits exactly what the gutter shows.
	markHunk := map[int]int{}
	lastRight := -1
	for i, row := range res.Rows {
		switch row.Kind {
		case diff.RowAdded, diff.RowChanged:
			markHunk[row.RightNo-1] = hunkOfRow(i)
			lastRight = row.RightNo - 1
		case diff.RowRemoved:
			if at := lastRight + 1; !hasMark(markHunk, at) {
				markHunk[at] = hunkOfRow(i)
			}
		case diff.RowSame:
			lastRight = row.RightNo - 1
		}
	}
	// Fold a deletion marker past the last real buffer line (a removal at
	// EOF) back onto the last line, mirroring vcs.LineMarks.
	lastReal := strings.Count(buf, "\n")
	if buf == "" || !strings.HasSuffix(buf, "\n") {
		lastReal++
	}
	lastReal--
	for at, hi := range markHunk {
		if at > lastReal {
			delete(markHunk, at)
			if !hasMark(markHunk, lastReal) {
				markHunk[lastReal] = hi
			}
		}
	}

	hi, ok := markHunk[line]
	if !ok || hi < 0 {
		return revertHunk{}, false
	}
	return buildHunk(res, hi), true
}

// hasMark reports whether the map holds an entry for line (0 is a valid hunk
// index, so presence needs the two-value lookup).
func hasMark(markHunk map[int]int, line int) bool {
	_, ok := markHunk[line]
	return ok
}

// buildHunk resolves hunk hi of res into buffer terms.
func buildHunk(res diff.Result, hi int) revertHunk {
	hk := res.Hunks[hi]
	h := revertHunk{rightStart: -1, rightEnd: -2}
	for i := hk.Start; i < hk.End; i++ {
		row := res.Rows[i]
		if row.RightNo > 0 {
			if h.rightStart < 0 {
				h.rightStart = row.RightNo - 1
			}
			h.rightEnd = row.RightNo - 1
		}
		if row.LeftNo > 0 {
			h.headLines = append(h.headLines, row.Left)
		}
	}
	if h.pureDeletion() {
		// Insert before the buffer line that follows the last unchanged line
		// above the hunk (0 when the hunk removes the head of the file).
		h.anchor = 0
		for i := hk.Start - 1; i >= 0; i-- {
			if res.Rows[i].RightNo > 0 {
				h.anchor = res.Rows[i].RightNo
				break
			}
		}
	}
	return h
}

// applyHunkRevert applies h against b through rec and returns the cursor
// position for after the revert.
func applyHunkRevert(b *buffer.Buffer, rec *history.Recorder, h revertHunk) buffer.Position {
	text := strings.Join(h.headLines, "\n")
	switch {
	case h.pureDeletion():
		if h.anchor < b.LineCount() {
			rec.Apply(buffer.Insert(buffer.Position{Line: h.anchor, Col: 0}, text+"\n"))
			return buffer.Position{Line: h.anchor, Col: 0}
		}
		// Removal at EOF: re-append below the last line.
		rec.Apply(buffer.Insert(b.EndOfBuffer(), "\n"+text))
		return buffer.Position{Line: b.LineCount() - len(h.headLines), Col: 0}
	case len(h.headLines) == 0:
		// Pure addition: drop the lines, one bounding newline included.
		r := buffer.Range{
			Start: buffer.Position{Line: h.rightStart, Col: 0},
			End:   buffer.Position{Line: h.rightEnd + 1, Col: 0},
		}
		if h.rightEnd+1 >= b.LineCount() {
			// Added lines at EOF: eat the newline before them instead.
			r.End = buffer.Position{Line: h.rightEnd, Col: b.RuneLen(h.rightEnd)}
			r.Start = buffer.Position{Line: h.rightStart, Col: 0}
			if h.rightStart > 0 {
				r.Start = buffer.Position{Line: h.rightStart - 1, Col: b.RuneLen(h.rightStart - 1)}
			}
		}
		rec.Apply(buffer.Delete(r))
		return buffer.Position{Line: min(h.rightStart, b.LineCount()-1), Col: 0}
	default:
		rec.Apply(buffer.Edit{
			Range: buffer.Range{
				Start: buffer.Position{Line: h.rightStart, Col: 0},
				End:   buffer.Position{Line: h.rightEnd, Col: b.RuneLen(h.rightEnd)},
			},
			Text: text,
		})
		return buffer.Position{Line: h.rightStart, Col: 0}
	}
}
