package editor

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/vcs"
)

// scrollbar.go — the editor pane's vertical scrollbar with a JetBrains-style
// diagnostics error stripe (#1022, part of #30). The bar overlays the pane's
// rightmost content column whenever the buffer has more lines than the
// viewport: a dim track, a heavier thumb whose position/size mirror the scroll
// offset and visible fraction (the explorer scrollbar's visual language), and
// severity-colored markers at each diagnostic line's proportional position.
// Mouse: a left press on the thumb starts a drag (routed by the app as
// dragEditScroll); a press on the track jumps the viewport proportionally.

// scrollbarGeometry resolves the bar's layout: the track length in rows, the
// total line count it maps, and the thumb's start/length on the track. ok is
// false when no scrollbar should render (unsized pane or no overflow).
func (m Model) scrollbarGeometry() (track, total, thumbStart, thumbLen int, ok bool) {
	track = m.view.Height()
	total = m.buf.LineCount()
	if track <= 0 || total <= track || m.width < 2 {
		return 0, 0, 0, 0, false
	}
	thumbStart, thumbLen = scrollThumb(track, total, track, m.view.Top)
	return track, total, thumbStart, thumbLen, true
}

// scrollThumb sizes and positions a scrollbar thumb on a track of the given
// length for a window of visible rows over a total content size at offset.
// Same math as the explorer's scrollbar so the two bars feel identical.
func scrollThumb(track, total, visible, offset int) (start, length int) {
	if track <= 0 {
		return 0, 0
	}
	if total <= visible {
		return 0, track
	}
	length = track * visible / total
	if length < 1 {
		length = 1
	}
	if length > track {
		length = track
	}
	maxOff := total - visible
	start = (track - length) * offset / maxOff
	if start < 0 {
		start = 0
	}
	if start > track-length {
		start = track - length
	}
	return
}

// ScrollbarHit reports whether a content-local press lands on the scrollbar:
// the pane's rightmost column while the bar is visible, within the track. The
// app checks this before any content click so the bar outranks text at that x.
func (m Model) ScrollbarHit(x, y int) bool {
	track, _, _, _, ok := m.scrollbarGeometry()
	return ok && x == m.width-1 && y >= 0 && y < track
}

// ScrollbarPress handles a left press at track row y (content-local). On the
// thumb it records the grab offset and returns true — the app then tracks a
// dragEditScroll gesture feeding ScrollbarDrag. On the track above/below the
// thumb it jumps the viewport to the proportional position and returns false.
func (m *Model) ScrollbarPress(y int) (drag bool) {
	track, total, start, length, ok := m.scrollbarGeometry()
	if !ok {
		return false
	}
	if y >= start && y < start+length {
		m.sbGrab = y - start
		return true
	}
	// A press on a marked cell jumps to that mark's line (#1131), centred;
	// unmarked track cells keep the proportional jump (#1022).
	if _, _, lines := m.stripesFor(track, total); lines != nil {
		if ln, ok := lines[y]; ok {
			maxOff := total - track
			m.SetScroll(clampInt(ln-track/2, 0, maxOff), m.view.Left)
			return false
		}
	}
	if track > 1 {
		maxOff := total - track
		m.SetScroll(clampInt(y*maxOff/(track-1), 0, maxOff), m.view.Left)
	}
	return false
}

// ScrollbarDrag continues a thumb drag: the thumb's top follows the pointer
// minus the recorded grab offset, mapped back to a scroll offset.
func (m *Model) ScrollbarDrag(y int) {
	track, total, _, length, ok := m.scrollbarGeometry()
	if !ok || track-length <= 0 {
		return
	}
	maxOff := total - track
	top := clampInt((y-m.sbGrab)*maxOff/(track-length), 0, maxOff)
	m.SetScroll(top, m.view.Left)
}

// sbCache memoizes the scrollbar stripe across frames (#1097): the map was
// rebuilt from every diagnostic on every frame. Pointer-held so the
// value-receiver View can fill it; the single-threaded update loop is the
// only writer, and the diagnostics epoch plus geometry key invalidation.
type sbCache struct {
	epoch        int
	marksEpoch   int
	track, total int
	stripe       map[int]int
	git          map[int]vcs.LineMark
	lines        map[int]int
	valid        bool
}

// stripesFor returns the memoized mark maps for the given geometry: the
// diagnostics stripe (#1022), the git change marks (#1131, overview-ruler
// style) and, for click-to-jump, each marked row's representative buffer
// line. Keyed on both the diagnostics and the git-marks epoch.
func (m Model) stripesFor(track, total int) (stripe map[int]int, git map[int]vcs.LineMark, lines map[int]int) {
	if m.sbcache != nil && m.sbcache.valid &&
		m.sbcache.epoch == m.diagsEpoch && m.sbcache.marksEpoch == m.marksEpoch &&
		m.sbcache.track == track && m.sbcache.total == total {
		return m.sbcache.stripe, m.sbcache.git, m.sbcache.lines
	}
	stripe = m.scrollbarStripe(track, total)
	git, lines = m.scrollbarGitMarks(track, total)
	// Diagnostics own their cells for click-to-jump too (they win the cell
	// visually); record their lines over any git line on the same row.
	for _, d := range m.diags {
		ln := d.Range.Start.Line
		if ln < 0 || ln >= total {
			continue
		}
		y := ln * track / total
		if y > track-1 {
			y = track - 1
		}
		if lines == nil {
			lines = make(map[int]int)
		}
		if cur, ok := lines[y]; !ok || ln < cur {
			lines[y] = ln
		}
	}
	if m.sbcache != nil {
		*m.sbcache = sbCache{epoch: m.diagsEpoch, marksEpoch: m.marksEpoch,
			track: track, total: total, stripe: stripe, git: git, lines: lines, valid: true}
	}
	return stripe, git, lines
}

// scrollbarGitMarks maps track rows to the strongest git change mark whose
// line lands there (#1131) — deleted > changed > added when hunks share a
// cell — plus each row's first marked line for click-to-jump.
func (m Model) scrollbarGitMarks(track, total int) (map[int]vcs.LineMark, map[int]int) {
	if len(m.gitMarks) == 0 {
		return nil, nil
	}
	rank := func(mk vcs.LineMark) int {
		switch mk {
		case vcs.LineDeleted:
			return 3
		case vcs.LineChanged:
			return 2
		}
		return 1
	}
	git := make(map[int]vcs.LineMark)
	lines := make(map[int]int)
	for ln, mk := range m.gitMarks {
		if ln < 0 || ln >= total {
			continue
		}
		y := ln * track / total
		if y > track-1 {
			y = track - 1
		}
		if cur, seen := git[y]; !seen || rank(mk) > rank(cur) {
			git[y] = mk
		}
		if cur, seen := lines[y]; !seen || ln < cur {
			lines[y] = ln
		}
	}
	return git, lines
}

// scrollbarStripe maps track rows to the worst diagnostic severity whose line
// lands there (error stripe): row -> severity (1=error … 4=hint), lower wins.
func (m Model) scrollbarStripe(track, total int) map[int]int {
	if len(m.diags) == 0 {
		return nil
	}
	stripe := make(map[int]int)
	for _, d := range m.diags {
		ln := d.Range.Start.Line
		if ln < 0 || ln >= total {
			continue
		}
		y := ln * track / total
		if y > track-1 {
			y = track - 1
		}
		sev := d.Severity
		if sev < 1 || sev > 4 {
			sev = 1 // servers may omit severity; treat as error, like LSP does
		}
		if cur, seen := stripe[y]; !seen || sev < cur {
			stripe[y] = sev
		}
	}
	return stripe
}

// overlayScrollbar draws the bar over the rightmost cell of the rendered rows.
// Each affected row is clipped/padded to the pane width minus one, then the
// bar cell is appended: a diagnostic marker (worst severity wins the cell)
// over the thumb over the track.
func (m Model) overlayScrollbar(rows []string) []string {
	track, total, start, length, ok := m.scrollbarGeometry()
	if !ok {
		return rows
	}
	stripe, git, _ := m.stripesFor(track, total)
	// Cells are identical across rows: render each variant once per call
	// instead of a lipgloss style chain + Render per row (#1097).
	trackCell := lipgloss.NewStyle().Foreground(m.theme().ScrollbarTrack).Render("│")
	plainThumb := lipgloss.NewStyle().Background(m.theme().ScrollbarThumb).Render(" ")
	var sevCells [5]string
	gitCells := map[vcs.LineMark]string{}
	w := m.width
	for y := 0; y < len(rows) && y < track; y++ {
		onThumb := y >= start && y < start+length
		var cell string
		switch {
		case onThumb:
			// The thumb reads as a solid block (#1138): every thumb row gets
			// the ScrollbarThumb background, and a mark on it draws its glyph
			// in the mark's colour ON that background — the thumb's extent
			// stays identifiable even when the whole bar is covered in marks,
			// and the marks inside it keep their colour. Diagnostics over
			// git, as everywhere.
			if sev, hit := stripe[y]; hit {
				cell = lipgloss.NewStyle().Background(m.theme().ScrollbarThumb).
					Foreground(m.diagColor(sev)).Bold(true).Render("■")
			} else if mk, hit := git[y]; hit {
				cell = lipgloss.NewStyle().Background(m.theme().ScrollbarThumb).
					Foreground(m.gitMarkColor(mk)).Bold(true).Render("▎")
			} else {
				cell = plainThumb
			}
		default:
			if sev, hit := stripe[y]; hit {
				if sevCells[sev] == "" {
					sevCells[sev] = lipgloss.NewStyle().Foreground(m.diagColor(sev)).Bold(true).Render("■")
				}
				cell = sevCells[sev]
			} else if mk, hit := git[y]; hit {
				// Git change mark (#1131): the gutter's colour convention on
				// the ruler; diagnostics outrank it on a shared cell.
				if gitCells[mk] == "" {
					gitCells[mk] = lipgloss.NewStyle().Foreground(m.gitMarkColor(mk)).Bold(true).Render("▎")
				}
				cell = gitCells[mk]
			} else {
				cell = trackCell
			}
		}
		row := ansi.Truncate(rows[y], w-1, "")
		if pad := w - 1 - ansi.StringWidth(row); pad > 0 {
			row += strings.Repeat(" ", pad)
		}
		rows[y] = row + cell
	}
	return rows
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
