package search

// landing.go is the bounded landing scan (#2734): the search behind "/"
// previews, Enter, n/N, "*"/"#" and the match-step chord. Before it, Next
// collected every match in the buffer (AllMatches) and picked one — a full
// pass over the document per keystroke, which on a multi-megabyte file kept
// the update loop busy for seconds and could not be interrupted.
//
// A landing now walks the buffer *from the departure point in the requested
// direction* and stops at the count-th match it meets, wrapping once past the
// buffer end. A pattern with matches near the cursor costs a handful of lines
// no matter how big the file is. A pattern that matches nowhere still has to
// look at every line — so the walk is resumable: Step scans under a byte
// budget and hands back a Scan the caller continues later, on the event loop
// or on a goroutine, until it reports Done. The editor runs the first budget
// synchronously and moves the rest off the loop with generation-tagged
// cancellation (editor/searchscan.go).

import (
	"unicode/utf8"

	"ike/internal/editor/buffer"
)

// Landing budgets (#2734), in bytes of line text scanned.
const (
	// SyncScanBytes is what one landing spends on the event loop before the
	// rest moves to a background scan: 256 KiB, a few milliseconds even for a
	// case-folded regex.
	SyncScanBytes = 256 << 10
	// AsyncScanBytes is the slice a background scan works through between
	// two cancellation checks.
	AsyncScanBytes = 1 << 20
)

// Scan is the resumable state of a landing scan. The zero value is not
// usable; Begin builds one.
type Scan struct {
	dir   Direction
	count int             // matches still to step over
	from  buffer.Position // the position the current step departs from
	line  int             // the next line to scan
	// turned marks the second leg: past the buffer end the walk came around
	// and now runs from the far end back toward the departure line, where it
	// ends. On the first leg the departure line only offers the matches on
	// the far side of the departure column.
	turned bool
}

// Landing is the outcome of one Step.
type Landing struct {
	// Pos is the match the scan landed on, valid when Found.
	Pos   buffer.Position
	Found bool
	// Done reports that the scan is over: with Found unset the pattern
	// matches nowhere in the buffer.
	Done bool
	// Scan is where to resume while !Done.
	Scan Scan
}

// Begin starts a landing scan for the count-th match from `from` in dir.
func (q Query) Begin(from buffer.Position, dir Direction, count int) Scan {
	if count < 1 {
		count = 1
	}
	return Scan{dir: dir, count: count, from: from, line: from.Line}
}

// Step continues a scan, spending at most maxBytes of line text (at least
// one line; maxBytes <= 0 means unbounded — run to the end). The scan may
// finish early with Found, finish with Done and no match, or hand back its
// resumable state.
func (q Query) Step(b *buffer.Buffer, s Scan, maxBytes int) Landing {
	if q.Empty() || b.LineCount() == 0 {
		return Landing{Done: true}
	}
	if q.multi {
		return q.multiStep(b, s, maxBytes)
	}
	n := b.LineCount()
	spent := 0
	for {
		if s.line < 0 || s.line >= n {
			if s.turned {
				return Landing{Done: true}
			}
			s.turned = true
			s.line = 0
			if s.dir == Backward {
				s.line = n - 1
			}
		}
		if s.turned && s.pastDeparture() {
			return Landing{Done: true}
		}
		spent += len(b.Line(s.line)) + 1
		if m, ok := q.lineLanding(b, s); ok {
			s.count--
			if s.count == 0 {
				return Landing{Pos: m, Found: true, Done: true}
			}
			// The next step departs from the match it just found, so a
			// count of 3 walks three matches, cycling past the end exactly
			// like the modulo arithmetic over the full list used to.
			s.from, s.line, s.turned = m, m.Line, false
			continue
		}
		s.line += s.stride()
		if maxBytes > 0 && spent >= maxBytes {
			return Landing{Scan: s}
		}
	}
}

// pastDeparture reports whether the wrapped leg walked past the departure
// line, i.e. the whole buffer has been covered.
func (s Scan) pastDeparture() bool {
	if s.dir == Forward {
		return s.line > s.from.Line
	}
	return s.line < s.from.Line
}

func (s Scan) stride() int {
	if s.dir == Forward {
		return 1
	}
	return -1
}

// lineLanding picks the landing on the scan's current line. A line of
// ordinary length is matched whole and filtered (pick); a line longer than
// LongLineBytes is cut at the departure column instead (#2734), so a
// forward landing costs the distance to the match rather than the line —
// the whole-line regex over a 600 KB minified line is ~20 ms, well over a
// frame, and it was paid on every n.
func (q Query) lineLanding(b *buffer.Buffer, s Scan) (buffer.Position, bool) {
	line := b.Line(s.line)
	if q.jq != nil || len(line) <= LongLineBytes {
		return s.pick(q.LineMatches(b, s.line))
	}
	onDeparture := !s.turned && s.line == s.from.Line
	if s.dir == Forward {
		base, off := 0, 0
		if onDeparture {
			base = s.from.Col + 1
			off = byteOffset(line, base)
		}
		bs, _, ok := q.firstIn(line[off:])
		if !ok {
			return buffer.Position{}, false
		}
		return buffer.Position{Line: s.line, Col: base + utf8.RuneCountInString(line[off:off+bs])}, true
	}
	// Backward: the prefix up to the departure column, plus a margin past it
	// so a match that starts before the column and reaches beyond it is
	// still seen whole (pick keeps only the starts before the column).
	limit := len(line)
	if onDeparture {
		limit = min(len(line), byteOffset(line, s.from.Col)+LongLineBytes)
	}
	return s.pick(q.scanText(line[:limit], s.line, 0))
}

// accepts reports whether a match starting at p is a landing for the scan:
// any match, except that on the first leg's departure line only those
// strictly past the departure column in the walking direction count.
func (s Scan) accepts(p buffer.Position) bool {
	if s.turned || p.Line != s.from.Line {
		return true
	}
	if s.dir == Forward {
		return p.Col > s.from.Col
	}
	return p.Col < s.from.Col
}

// pick chooses the landing among one line's matches (in reading order):
// forward the first acceptable one, backward the last.
func (s Scan) pick(spans []Span) (buffer.Position, bool) {
	if s.dir == Forward {
		for _, sp := range spans {
			if p := (buffer.Position{Line: sp.Line, Col: sp.Start}); s.accepts(p) {
				return p, true
			}
		}
		return buffer.Position{}, false
	}
	for i := len(spans) - 1; i >= 0; i-- {
		if p := (buffer.Position{Line: spans[i].Line, Col: spans[i].Start}); s.accepts(p) {
			return p, true
		}
	}
	return buffer.Position{}, false
}

// multiStep is Step for a pattern spanning lines (#2600): the walk goes
// window by window instead of line by line, each window's text joined and
// matched as multiline.go does. The window is widened by the pattern's break
// count on both sides, so a match reaching into it from outside is seen —
// and, as it starts outside, left to the window it starts in.
func (q Query) multiStep(b *buffer.Buffer, s Scan, maxBytes int) Landing {
	n := b.LineCount()
	back := q.breaks
	if back < 1 {
		back = 1
	}
	spent := 0
	for {
		if s.line < 0 || s.line >= n {
			if s.turned {
				return Landing{Done: true}
			}
			s.turned = true
			s.line = 0
			if s.dir == Backward {
				s.line = n - 1
			}
		}
		if s.turned && s.pastDeparture() {
			return Landing{Done: true}
		}
		lo, hi, bytes := s.window(b, maxBytes)
		spent += bytes
		if p, ok := s.pickRanges(b, q.scanLines(b, lo-back, hi+back), lo, hi); ok {
			s.count--
			if s.count == 0 {
				return Landing{Pos: p, Found: true, Done: true}
			}
			s.from, s.line, s.turned = p, p.Line, false
			continue
		}
		if s.dir == Forward {
			s.line = hi + 1
		} else {
			s.line = lo - 1
		}
		if maxBytes > 0 && spent >= maxBytes {
			return Landing{Scan: s}
		}
	}
}

// window extends from s.line in the walking direction for as many lines as
// the byte budget allows (at least one), never past the buffer end or — on
// the wrapped leg — past the departure line. It returns the inclusive line
// range and the bytes it holds.
func (s Scan) window(b *buffer.Buffer, maxBytes int) (lo, hi, bytes int) {
	n := b.LineCount()
	lo, hi = s.line, s.line
	bytes = len(b.Line(s.line)) + 1
	for {
		next := hi + 1
		if s.dir == Backward {
			next = lo - 1
		}
		if next < 0 || next >= n || s.turned && s.legEndsBefore(next) {
			return lo, hi, bytes
		}
		if maxBytes > 0 && bytes >= maxBytes {
			return lo, hi, bytes
		}
		bytes += len(b.Line(next)) + 1
		if s.dir == Forward {
			hi = next
		} else {
			lo = next
		}
	}
}

// legEndsBefore reports whether line lies past the wrapped leg's end (the
// departure line) in the walking direction.
func (s Scan) legEndsBefore(line int) bool {
	if s.dir == Forward {
		return line > s.from.Line
	}
	return line < s.from.Line
}

// pickRanges chooses the landing among a window's multi-line matches: only
// those starting inside [lo, hi] belong to this window, and among them the
// first acceptable head forward, the last backward.
func (s Scan) pickRanges(b *buffer.Buffer, ms []mrange, lo, hi int) (buffer.Position, bool) {
	inWindow := func(mr mrange) bool { return mr.start.Line >= lo && mr.start.Line <= hi }
	if s.dir == Forward {
		for _, mr := range ms {
			if inWindow(mr) && s.accepts(mr.start) {
				return mr.start, true
			}
		}
		return buffer.Position{}, false
	}
	for i := len(ms) - 1; i >= 0; i-- {
		if mr := ms[i]; inWindow(mr) && s.accepts(mr.start) {
			return mr.start, true
		}
	}
	return buffer.Position{}, false
}
