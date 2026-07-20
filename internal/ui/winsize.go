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
// adjustment. The chords are ctrl+shift+arrows: CSI-parameter-encoded, so
// they are delivered in every mainstream terminal (the ctrl+shift collapse
// only affects character keys).
func ResizeDelta(key string) (ddw, ddh int, ok bool) {
	switch key {
	case "ctrl+shift+left":
		return -4, 0, true
	case "ctrl+shift+right":
		return 4, 0, true
	case "ctrl+shift+up":
		return 0, -1, true
	case "ctrl+shift+down":
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
	if s.path == "" {
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
