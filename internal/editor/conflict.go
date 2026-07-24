package editor

// conflict.go implements merge-conflict resolution in the editor (#1149):
// detection of git conflict blocks (`<<<<<<<` / `=======` / `>>>>>>>`, with
// an optional diff3 `|||||||` base section), tinted rendering of the ours /
// base / theirs sections, per-block accept actions (ours / theirs / both —
// each a single undo unit), and next/previous navigation. Detection is cached
// per document version (the testmarks #1150 pattern): the scan runs at most
// once per edit, never per frame, and each rescan bumps a conflicts epoch so
// the scrollbar's stripe memo (#1131/#1097) invalidates exactly when the
// blocks can have moved.

import (
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
)

// conflictBlock is one detected conflict, all fields 0-based buffer lines:
// start is the `<<<<<<<` marker, sep the `=======`, end the `>>>>>>>`; base is
// the diff3 `|||||||` marker or -1 when the block has no base section. The
// ours section spans (start, base|sep), the base section (base, sep), the
// theirs section (sep, end) — all exclusive of the marker lines.
type conflictBlock struct {
	start int
	base  int
	sep   int
	end   int
}

// oursEnd is the exclusive end of the ours section: the base marker when a
// diff3 section is present, else the separator.
func (c conflictBlock) oursEnd() int {
	if c.base >= 0 {
		return c.base
	}
	return c.sep
}

// contains reports whether the 0-based line lies inside the block, marker
// lines included.
func (c conflictBlock) contains(line int) bool { return line >= c.start && line <= c.end }

// conflictRole classifies a buffer line for rendering (#1149).
type conflictRole int

const (
	conflictNone   conflictRole = iota
	conflictMarker              // a `<<<<<<<` / `|||||||` / `=======` / `>>>>>>>` line
	conflictOurs                // ours section (between start and base/sep)
	conflictBase                // diff3 base section (between ||||||| and =======)
	conflictTheirs              // theirs section (between ======= and end)
)

// conflictStore caches the detected blocks per document version, pointer-held
// so the many value copies of a Model sharing one view use one cache (the
// testMarkStore pattern, #1150). epoch bumps on every rescan; the scrollbar
// stripe memo keys on it beside the diagnostics and git-marks epochs.
type conflictStore struct {
	version int
	path    string
	blocks  []conflictBlock
	epoch   int
}

func newConflictStore() *conflictStore { return &conflictStore{version: -1} }

// conflictMarkerAt reports whether line opens with the 7-rune marker followed
// by nothing or a space — so an 8th repeated rune (a markdown `========`
// underline) never matches.
func conflictMarkerAt(line, marker string) bool {
	if !strings.HasPrefix(line, marker) {
		return false
	}
	return len(line) == len(marker) || line[len(marker)] == ' '
}

// scanConflicts detects the well-formed conflict blocks in lines: a
// `<<<<<<<` opener, an optional `|||||||` base marker, the `=======`
// separator, and the `>>>>>>>` closer, in that order. A second opener before
// the block completes abandons the half block and restarts there (the outer
// markers cannot nest); a truncated block at EOF yields nothing.
func scanConflicts(lines []string) []conflictBlock {
	var out []conflictBlock
	cur := -1 // index of the open `<<<<<<<` line, -1 when idle
	base, sep := -1, -1
	for i, l := range lines {
		switch {
		case conflictMarkerAt(l, "<<<<<<<"):
			cur, base, sep = i, -1, -1
		case cur < 0:
			continue
		case sep < 0 && base < 0 && conflictMarkerAt(l, "|||||||"):
			base = i
		case sep < 0 && conflictMarkerAt(l, "======="):
			sep = i
		case sep >= 0 && conflictMarkerAt(l, ">>>>>>>"):
			out = append(out, conflictBlock{start: cur, base: base, sep: sep, end: i})
			cur, base, sep = -1, -1, -1
		}
	}
	return out
}

// conflicts returns the current blocks, rescanning only when the document
// version or path moved since the last scan (never per frame). Each rescan
// bumps the store's epoch so downstream memos invalidate.
func (m Model) conflicts() []conflictBlock {
	if m.conflictCache == nil {
		return nil
	}
	if m.conflictCache.version == m.docVersion && m.conflictCache.path == m.path {
		return m.conflictCache.blocks
	}
	m.conflictCache.version = m.docVersion
	m.conflictCache.path = m.path
	m.conflictCache.blocks = scanConflicts(m.buf.Lines())
	m.conflictCache.epoch++
	return m.conflictCache.blocks
}

// conflictsEpoch returns the epoch of the current block set, refreshing the
// cache first so the epoch always describes this document version.
func (m Model) conflictsEpoch() int {
	if m.conflictCache == nil {
		return 0
	}
	m.conflicts()
	return m.conflictCache.epoch
}

// conflictAt returns the block containing the 0-based line, marker lines
// included.
func (m Model) conflictAt(line int) (conflictBlock, bool) {
	for _, c := range m.conflicts() {
		if c.contains(line) {
			return c, true
		}
	}
	return conflictBlock{}, false
}

// ConflictAtCursor reports whether the cursor sits inside a conflict block —
// the app's cheap query for showing the accept entries in the editor context
// menu (#1020).
func (m Model) ConflictAtCursor() bool {
	_, ok := m.conflictAt(m.cursor.Line)
	return ok
}

// conflictRoleOf classifies a line for rendering: marker lines dim bold, the
// ours/theirs sections tint, the diff3 base section dims (view.go).
func (m Model) conflictRoleOf(line int) conflictRole {
	c, ok := m.conflictAt(line)
	if !ok {
		return conflictNone
	}
	switch {
	case line == c.start || line == c.sep || line == c.end || line == c.base:
		return conflictMarker
	case line < c.oursEnd():
		return conflictOurs
	case c.base >= 0 && line < c.sep:
		return conflictBase
	default:
		return conflictTheirs
	}
}

// acceptConflict resolves the block containing the cursor: the whole block —
// marker lines included — is replaced by the kept side(s), ours first when
// both are kept, as ONE undo unit (the mutate/Recorder path every operator
// uses). The cursor lands on the block's start line. Returns the ex-line
// notice when the cursor is not inside a conflict.
func (m *Model) acceptConflict(keepOurs, keepTheirs bool) tea.Cmd {
	c, ok := m.conflictAt(m.cursor.Line)
	if !ok {
		return notice("no merge conflict at cursor")
	}
	var kept []string
	if keepOurs {
		for l := c.start + 1; l < c.oursEnd(); l++ {
			kept = append(kept, m.buf.Line(l))
		}
	}
	if keepTheirs {
		for l := c.sep + 1; l < c.end; l++ {
			kept = append(kept, m.buf.Line(l))
		}
	}
	text := strings.Join(kept, "\n")
	last := m.buf.LineCount() - 1
	m.mutate(func(rec *history.Recorder) buffer.Position {
		var r buffer.Range
		switch {
		case c.end < last:
			// Whole-line replacement through the following line break.
			r = buffer.Range{Start: buffer.Position{Line: c.start}, End: buffer.Position{Line: c.end + 1}}
			if text != "" {
				text += "\n"
			}
		case len(kept) == 0 && c.start > 0:
			// Block at EOF, nothing kept: also consume the preceding newline
			// so no empty line is left behind.
			prev := len([]rune(m.buf.Line(c.start - 1)))
			r = buffer.Range{
				Start: buffer.Position{Line: c.start - 1, Col: prev},
				End:   buffer.Position{Line: c.end, Col: len([]rune(m.buf.Line(c.end)))},
			}
		default:
			r = buffer.Range{
				Start: buffer.Position{Line: c.start},
				End:   buffer.Position{Line: c.end, Col: len([]rune(m.buf.Line(c.end)))},
			}
		}
		rec.Apply(buffer.Edit{Range: r, Text: text})
		return buffer.Position{Line: c.start}
	})
	return nil
}

// conflictJump moves the cursor to the next (forward) or previous conflict
// block's start line, wrapping around the file — the diagnosticJump walk
// (#369) over conflict starts.
func (m *Model) conflictJump(forward bool) tea.Cmd {
	blocks := m.conflicts()
	if len(blocks) == 0 {
		return notice("no merge conflicts in this file")
	}
	starts := make([]int, len(blocks))
	for i, c := range blocks {
		starts[i] = c.start
	}
	sort.Ints(starts)
	// Strictly past the cursor line in the walk direction, so a press while
	// standing on a block's start moves on; falling off either end wraps.
	pick, found := starts[0], false
	if forward {
		for _, s := range starts {
			if s > m.cursor.Line {
				pick, found = s, true
				break
			}
		}
	} else {
		pick = starts[len(starts)-1]
		for i := len(starts) - 1; i >= 0; i-- {
			if starts[i] < m.cursor.Line {
				pick, found = starts[i], true
				break
			}
		}
	}
	wrapped := ""
	if !found {
		wrapped = " (wrapped)"
	}
	m.SetCursor(pick, 0)
	n := 0
	for i, s := range starts {
		if s == pick {
			n = i + 1
			break
		}
	}
	return notice("merge conflict " + strconv.Itoa(n) + "/" + strconv.Itoa(len(starts)) + wrapped)
}
