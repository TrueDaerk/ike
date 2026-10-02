package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// winsize.go implements resizable floating windows (#774): a persisted
// per-window-kind size adjustment (a width/height delta on top of the
// window's computed default) plus the shared resize-key mapping. Deltas —
// not absolute sizes — persist, so every window re-clamps naturally against
// the live terminal bounds when applied.

// ResizeDelta maps a pressed key (tea key String form) to a window size
// adjustment. Accepted chords, all modifier+shift+arrow:
//   - cmd+shift (spelled shift+super / shift+meta) — the macOS primary:
//     ctrl+arrows belong to Mission Control/Spaces there and never reach the
//     terminal (#774), while terminals like Ghostty deliver cmd chords;
//   - ctrl+shift — the primary everywhere else (CSI-parameter-encoded);
//   - alt+shift — spare secondary where Option is not a composition key.
//
// Both spellings of the cmd chords are accepted: the raw tea key String form
// (shift+super/shift+meta) most callers pass, and the keymap.Key canonical form
// (cmd+shift) callers that normalize first produce — the popup terminal parses
// the chord before it gets here (#1714).
func ResizeDelta(key string) (ddw, ddh int, ok bool) {
	switch key {
	case "ctrl+shift+left", "shift+super+left", "shift+meta+left", "cmd+shift+left", "alt+shift+left":
		return -4, 0, true
	case "ctrl+shift+right", "shift+super+right", "shift+meta+right", "cmd+shift+right", "alt+shift+right":
		return 4, 0, true
	case "ctrl+shift+up", "shift+super+up", "shift+meta+up", "cmd+shift+up", "alt+shift+up":
		return 0, -1, true
	case "ctrl+shift+down", "shift+super+down", "shift+meta+down", "cmd+shift+down", "alt+shift+down":
		return 0, 1, true
	}
	return 0, 0, false
}

// winDelta is one window kind's persisted adjustment.
type winDelta struct {
	W int `json:"w,omitempty"`
	H int `json:"h,omitempty"`
}

// WinSizes stores per-window-kind size deltas. The zero value (and nil) is
// inert: Get returns zero deltas and Adjust is a no-op without a path.
type WinSizes struct {
	path   string
	deltas map[string]winDelta
}

// LoadWinSizes reads the store at path, tolerating a missing or malformed
// file (fresh deltas). Failing to read must never disrupt the session.
func LoadWinSizes(path string) *WinSizes {
	w := &WinSizes{path: path, deltas: map[string]winDelta{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return w
	}
	var deltas map[string]winDelta
	if json.Unmarshal(data, &deltas) == nil && deltas != nil {
		w.deltas = deltas
	}
	return w
}

// Get returns the stored width/height delta for a window kind.
func (s *WinSizes) Get(kind string) (dw, dh int) {
	if s == nil {
		return 0, 0
	}
	d := s.deltas[kind]
	return d.W, d.H
}

// Has reports whether a window kind carries its own stored entry. It
// distinguishes "never resized here" from "resized back to the default size"
// (a zero delta), which is what a cross-store fallback needs (#1714).
func (s *WinSizes) Has(kind string) bool {
	if s == nil {
		return false
	}
	_, ok := s.deltas[kind]
	return ok
}

// Set replaces a window kind's delta outright and persists the store — used to
// mirror a store's delta into another one (the global popup-terminal fallback,
// #1714), where accumulating would drift the two apart.
func (s *WinSizes) Set(kind string, dw, dh int) {
	s.Put(kind, dw, dh)
	s.Flush()
}

// Put replaces a window kind's delta outright without persisting — the
// mid-drag step that rewrites a position offset (#2896); the drag's release
// calls Flush.
func (s *WinSizes) Put(kind string, dw, dh int) {
	if s == nil || kind == "" {
		return
	}
	if s.deltas == nil {
		s.deltas = map[string]winDelta{}
	}
	s.deltas[kind] = winDelta{W: dw, H: dh}
}

// OffsetKey is the store key holding a window kind's position offset from its
// centered origin (#1793, #2896): the offset rides in the same store as the
// size delta, under the kind's name plus ":pos".
func OffsetKey(kind string) string { return kind + ":pos" }

// Offset returns a window kind's stored position offset from center.
func (s *WinSizes) Offset(kind string) (dx, dy int) { return s.Get(OffsetKey(kind)) }

// SetOffset replaces a window kind's position offset without persisting (the
// mid-drag step, #2896); the drag's release calls Flush.
func (s *WinSizes) SetOffset(kind string, dx, dy int) {
	if kind == "" {
		return
	}
	s.Put(OffsetKey(kind), dx, dy)
}

// FloatOrigin places a w×h box in a tw×th terminal: centered, shifted by the
// position offset (ox, oy), clamped so the box always sits fully on screen
// (#2896). The clamp runs on every resolve, so a terminal resize re-clamps a
// stored offset on the very next frame and hit-test. A box larger than the
// terminal along an axis (content that ignores its width budget) has no room
// to move there: it stays centered, overhanging both sides evenly, exactly
// where the centered placement always put it.
func FloatOrigin(tw, th, w, h, ox, oy int) (x, y int) {
	return floatAxis(tw, w, ox), floatAxis(th, h, oy)
}

// floatAxis is FloatOrigin along one axis.
func floatAxis(t, w, o int) int {
	if w > t {
		return (t - w) / 2
	}
	return ClampDelta((t-w)/2, o, 0, t-w)
}

// AnchorOffset returns the position offset (along one axis, terminal extent t)
// that keeps a resized box's opposite edge in place (#2896): the box sat at x
// with extent w and now measures w2; s is the grabbed edge's grow direction —
// +1 (right/bottom) keeps the near edge x, −1 (left/top) keeps the far edge
// x+w, 0 (axis not grabbed) keeps x as well.
func AnchorOffset(t, x, w, w2, s int) int {
	nx := x
	if s < 0 {
		nx = x + w - w2
	}
	return nx - (t-w2)/2
}

// AnchoredStep bounds one edge-drag pointer delta d (along one axis, terminal
// extent t) so the grabbed edge never leaves the screen (#2896): a left/top
// edge grows at most to cell 0, a right/bottom edge at most to the last cell.
// The box sits at x with extent w; s is the grabbed edge's grow direction, and
// an axis not grabbed (s == 0) takes no step.
func AnchoredStep(t, x, w, s, d int) int {
	switch {
	case s < 0:
		return max(d, min(-x, 0))
	case s > 0:
		return min(d, max(t-(x+w), 0))
	}
	return 0
}

// Adjust adds a delta for a window kind and persists the store. Errors are
// swallowed: failing to persist must never disrupt the session.
func (s *WinSizes) Adjust(kind string, ddw, ddh int) {
	s.Nudge(kind, ddw, ddh)
	s.Flush()
}

// Nudge adds a delta without persisting — the mid-drag step of a mouse resize
// (#933), where writing the store once per motion event would be waste. The
// drag's release calls Flush.
func (s *WinSizes) Nudge(kind string, ddw, ddh int) {
	if s == nil || kind == "" {
		return
	}
	if s.deltas == nil {
		s.deltas = map[string]winDelta{}
	}
	d := s.deltas[kind]
	d.W += ddw
	d.H += ddh
	s.deltas[kind] = d
}

// Flush persists the store. Errors are swallowed: failing to persist must
// never disrupt the session.
func (s *WinSizes) Flush() {
	if s == nil || s.path == "" {
		return
	}
	data, err := json.Marshal(s.deltas)
	if err != nil {
		return
	}
	if dir := filepath.Dir(s.path); dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	_ = os.WriteFile(s.path, data, 0o644)
}

// ResizeZone hit-tests a point in box-local coordinates against the box's
// border ring for a mouse resize (#933). It returns the horizontal/vertical
// grow directions: an edge sets one axis (left/top −1, right/bottom +1), a
// corner both. Only the outermost cell counts — one cell further in is
// content, so border clicks never swallow content clicks (#761 precedent).
func ResizeZone(x, y, w, h int) (sx, sy int, ok bool) {
	if w < 3 || h < 3 || x < 0 || y < 0 || x >= w || y >= h {
		return 0, 0, false
	}
	switch x {
	case 0:
		sx = -1
	case w - 1:
		sx = 1
	}
	switch y {
	case 0:
		sy = -1
	case h - 1:
		sy = 1
	}
	return sx, sy, sx != 0 || sy != 0
}

// ClampDelta bounds base+delta into [min, max] and returns the result.
func ClampDelta(base, delta, min, max int) int {
	v := base + delta
	if v < min {
		v = min
	}
	if v > max {
		v = max
	}
	return v
}
