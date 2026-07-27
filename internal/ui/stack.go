package ui

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/overlay"
	"ike/internal/theme"
)

// stackLayer pairs a shell with its lifetime: base layers (installed at
// construction) survive Close and are reopened by their owners, transient
// layers (Push) are removed from the stack as soon as they close.
type stackLayer struct {
	f         *Floating
	transient bool
}

// Stack is the z-ordered set of floating shells (#1237). The shell itself
// stays single-level and unaware of layering; the stack owns the order:
// layers composite bottom-to-top via internal/overlay, the topmost open layer
// owns key input, and dismissal pops one layer at a time. A stack of one
// behaves exactly like a bare Floating, so existing single-pane hosts keep
// working unchanged. Base layers passed to NewStack persist across open/close
// cycles (the host owns and reuses them); layers added via Push are transient
// and leave the stack when they close.
type Stack struct {
	layers []stackLayer

	width, height int
	pal           *theme.Palette
	sizes         *WinSizes
	maxWidth      int
}

// NewStack returns a stack seeded with the given persistent base layers,
// ordered bottom-to-top.
func NewStack(base ...*Floating) *Stack {
	s := &Stack{}
	for _, f := range base {
		s.layers = append(s.layers, stackLayer{f: f})
	}
	return s
}

// Push adds f as a new transient topmost layer, threads the stack's shared
// state (size, palette, size store, width cap) into it, and opens it. The
// layer is removed from the stack once it closes.
func (s *Stack) Push(f *Floating) {
	if f == nil {
		return
	}
	if s.pal != nil {
		f.SetPalette(s.pal)
	}
	if s.sizes != nil {
		f.SetSizeStore(s.sizes)
	}
	if s.maxWidth > 0 {
		f.SetMaxWidth(s.maxWidth)
	}
	if s.width > 0 && s.height > 0 {
		f.SetSize(s.width, s.height)
	}
	s.layers = append(s.layers, stackLayer{f: f, transient: true})
	f.Open()
}

// Pop closes the topmost open layer (transient layers also leave the stack).
// It reports whether a layer was closed.
func (s *Stack) Pop() bool {
	for i := len(s.layers) - 1; i >= 0; i-- {
		if s.layers[i].f.IsOpen() {
			s.layers[i].f.Close()
			s.prune()
			return true
		}
	}
	return false
}

// Top returns the topmost open shell — the input owner — or nil when no
// layer is open.
func (s *Stack) Top() *Floating {
	for i := len(s.layers) - 1; i >= 0; i-- {
		if s.layers[i].f.IsOpen() {
			return s.layers[i].f
		}
	}
	return nil
}

// IsOpen reports whether any layer is open.
func (s *Stack) IsOpen() bool { return s.Top() != nil }

// Depth is the number of currently open layers.
func (s *Stack) Depth() int {
	n := 0
	for _, l := range s.layers {
		if l.f.IsOpen() {
			n++
		}
	}
	return n
}

// SetSize records the terminal size and propagates it to every layer.
func (s *Stack) SetSize(width, height int) {
	s.width, s.height = width, height
	for _, l := range s.layers {
		l.f.SetSize(width, height)
	}
}

// SetPalette threads the active theme palette into every layer and every
// future Push.
func (s *Stack) SetPalette(p *theme.Palette) {
	s.pal = p
	for _, l := range s.layers {
		l.f.SetPalette(p)
	}
}

// SetSizeStore installs the persisted per-window size deltas (#774) on every
// layer and every future Push.
func (s *Stack) SetSizeStore(st *WinSizes) {
	s.sizes = st
	for _, l := range s.layers {
		l.f.SetSizeStore(st)
	}
}

// SetMaxWidth caps the outer width (#932) of every layer and every future
// Push.
func (s *Stack) SetMaxWidth(max int) {
	s.maxWidth = max
	for _, l := range s.layers {
		l.f.SetMaxWidth(max)
	}
}

// Update routes one message: a window size resizes every layer, everything
// else goes to the topmost open layer only (key-swallow as before). A dismiss
// key handled by that layer closes just it — one layer per keypress — and a
// closed transient layer leaves the stack. It reports whether the message was
// consumed.
func (s *Stack) Update(msg tea.Msg) bool {
	if sz, ok := msg.(tea.WindowSizeMsg); ok {
		s.SetSize(sz.Width, sz.Height)
		return s.IsOpen()
	}
	top := s.Top()
	if top == nil {
		return false
	}
	consumed := top.Update(msg)
	if !top.IsOpen() {
		s.prune()
	}
	return consumed
}

// Composite draws every open layer bottom-to-top centered over base, a canvas
// w columns by h rows, so the topmost layer is drawn last and fully readable.
func (s *Stack) Composite(base string, w, h int) string {
	for _, l := range s.layers {
		if l.f.IsOpen() {
			base = overlay.Center(base, l.f.View(), w, h)
		}
	}
	return base
}

// prune drops closed transient layers; base layers stay for reuse.
func (s *Stack) prune() {
	kept := s.layers[:0]
	for _, l := range s.layers {
		if l.transient && !l.f.IsOpen() {
			continue
		}
		kept = append(kept, l)
	}
	s.layers = kept
}
