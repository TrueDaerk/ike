package editor

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/x/ansi"
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
	stripe := m.scrollbarStripe(track, total)
	trackStyle := lipgloss.NewStyle().Foreground(m.theme().ScrollbarTrack)
	thumbStyle := lipgloss.NewStyle().Foreground(m.theme().ScrollbarThumb)
	w := m.width
	for y := 0; y < len(rows) && y < track; y++ {
		var cell string
		if sev, hit := stripe[y]; hit {
			cell = lipgloss.NewStyle().Foreground(m.diagColor(sev)).Bold(true).Render("■")
		} else if y >= start && y < start+length {
			cell = thumbStyle.Render("┃")
		} else {
			cell = trackStyle.Render("│")
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
