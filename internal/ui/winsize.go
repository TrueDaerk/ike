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
func ResizeDelta(key string) (ddw, ddh int, ok bool) {
	switch key {
	case "ctrl+shift+left", "shift+super+left", "shift+meta+left", "alt+shift+left":
		return -4, 0, true
	case "ctrl+shift+right", "shift+super+right", "shift+meta+right", "alt+shift+right":
		return 4, 0, true
	case "ctrl+shift+up", "shift+super+up", "shift+meta+up", "alt+shift+up":
		return 0, -1, true
	case "ctrl+shift+down", "shift+super+down", "shift+meta+down", "alt+shift+down":
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
