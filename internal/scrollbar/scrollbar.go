// Package scrollbar holds the shared vertical-scrollbar math used by every
// scrollable pane (#1367): thumb sizing/positioning plus the two mouse
// mappings — proportional track jumps and thumb drags. The editor (#1022)
// and explorer (#1036) established the geometry; extracting it here keeps
// every new bar (HTTP response viewer #1367, terminal scrollback #1368) on
// the exact same visual and interaction language instead of a re-derivation.
//
// All functions speak the same vocabulary: track is the bar's length in
// rows, total the content size in lines, visible the window size in lines,
// and offset the scroll position (0 = top).
package scrollbar

// Thumb sizes and positions a scrollbar thumb on a track of the given length
// for a window of visible rows over a total content size at offset.
func Thumb(track, total, visible, offset int) (start, length int) {
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

// Jump maps a left press on track row y (outside the thumb) onto the
// proportional scroll offset — the click-to-jump every bar implements.
// Returns the current offset unchanged when the geometry leaves no room.
func Jump(y, track, total, visible, offset int) int {
	maxOff := total - visible
	if maxOff <= 0 || track <= 1 {
		return offset
	}
	return clamp(y*maxOff/(track-1), 0, maxOff)
}

// Drag continues a thumb drag: the thumb's top follows pointer row y minus
// the grab offset recorded at press time, mapped back to a scroll offset.
// Returns the current offset unchanged when the thumb fills the track.
func Drag(y, grab, track, total, visible, offset int) int {
	maxOff := total - visible
	_, length := Thumb(track, total, visible, offset)
	if maxOff <= 0 || track-length <= 0 {
		return offset
	}
	return clamp((y-grab)*maxOff/(track-length), 0, maxOff)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
