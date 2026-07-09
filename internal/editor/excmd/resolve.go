package excmd

// Buffer is the minimal buffer view the range resolver needs.
type Buffer interface {
	LineCount() int
}

// Resolver carries the editor state an address may consult: the cursor line, the
// visual-selection bounds ('<' / '>'), and a line-search hook for pattern
// addresses. All line numbers are 0-based. VisualStart/VisualEnd are -1 when no
// visual selection is remembered; Search may be nil when pattern addresses are
// not supported by the caller.
type Resolver struct {
	Buf         Buffer
	Current     int
	VisualStart int
	VisualEnd   int
	// Search returns the 0-based line of the next match of pattern, searching
	// from the current line in the given direction (wrapping is the caller's
	// choice); ok is false when nothing matches.
	Search func(pattern string, from int, forward bool) (line int, ok bool)
}

// Resolve turns the range into a 0-based inclusive [start, end] line span. An
// empty range (Count 0) falls back to def for both ends. Resolved lines are
// clamped to the buffer, and a reversed two-address span is swapped so start <=
// end. A non-empty error string reports an address that could not be resolved
// (missing selection, pattern not found).
func (r Range) Resolve(rv Resolver, def int) (start, end int, err string) {
	switch r.Count {
	case 0:
		l := rv.clamp(def)
		return l, l, ""
	case 1:
		l, e := rv.resolveAddr(r.Start)
		if e != "" {
			return 0, 0, e
		}
		l = rv.clamp(l)
		return l, l, ""
	default:
		s, e := rv.resolveAddr(r.Start)
		if e != "" {
			return 0, 0, e
		}
		en, e := rv.resolveAddr(r.End)
		if e != "" {
			return 0, 0, e
		}
		s, en = rv.clamp(s), rv.clamp(en)
		if s > en {
			s, en = en, s
		}
		return s, en, ""
	}
}

// resolveAddr maps a single address to a 0-based (unclamped) line.
func (rv Resolver) resolveAddr(a Address) (int, string) {
	var base int
	switch a.Kind {
	case AddrNone, AddrCurrent:
		base = rv.Current
	case AddrLine:
		base = a.Line - 1
	case AddrLast:
		base = rv.Buf.LineCount() - 1
	case AddrVisualStart:
		if rv.VisualStart < 0 {
			return 0, "no visual selection"
		}
		base = rv.VisualStart
	case AddrVisualEnd:
		if rv.VisualEnd < 0 {
			return 0, "no visual selection"
		}
		base = rv.VisualEnd
	case AddrPatternNext, AddrPatternPrev:
		if rv.Search == nil {
			return 0, "pattern search unavailable"
		}
		line, ok := rv.Search(a.Pattern, rv.Current, a.Kind == AddrPatternNext)
		if !ok {
			return 0, "pattern not found: " + a.Pattern
		}
		base = line
	}
	return base + a.Offset, ""
}

// clamp keeps a line within [0, LineCount-1].
func (rv Resolver) clamp(l int) int {
	if l < 0 {
		return 0
	}
	if hi := rv.Buf.LineCount() - 1; l > hi {
		return hi
	}
	return l
}
