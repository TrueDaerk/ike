package highlight

// colMapper converts byte offsets to rune columns per line. ASCII-only lines
// (the common case) take a fast path where byte == rune column.
//
// The conversion used to rescan the line prefix on every call, which made a
// parse quadratic in the line length — O(spans × line bytes) on a minified
// megabyte body with non-ASCII content, the http.run freeze of #2353. Both
// answers are cached per line instead: the offset of the line's first
// non-ASCII byte (one scan per line, answering every query left of it in
// O(1)) and, once a query lands right of it, a prefix table of rune counts
// (one scan per line, O(1) per query). The caches live in shared backing
// arrays, so the mapper still passes by value like before.
type colMapper struct {
	lines []string
	// asciiTo[line] is 1 + the byte offset of the line's first non-ASCII
	// byte (1 + len(line) when the line is pure ASCII); 0 = not yet scanned.
	asciiTo []int
	// runes[line][k] is the number of runes starting in line[na : na+k],
	// where na is the line's first non-ASCII byte offset; nil until a query
	// crosses that offset.
	runes [][]int32
}

func newColMapper(lines []string) colMapper {
	return colMapper{
		lines:   lines,
		asciiTo: make([]int, len(lines)),
		runes:   make([][]int32, len(lines)),
	}
}

func (c colMapper) lineBytes(line int) int {
	if line < 0 || line >= len(c.lines) {
		return 0
	}
	return len(c.lines[line])
}

// firstNonASCII is the byte offset of the line's first non-ASCII byte,
// len(line) for a pure-ASCII line; scanned once and cached.
func (c colMapper) firstNonASCII(line int) int {
	if v := c.asciiTo[line]; v > 0 {
		return v - 1
	}
	s := c.lines[line]
	na := len(s)
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			na = i
			break
		}
	}
	c.asciiTo[line] = na + 1
	return na
}

// runeTable is the line's cached rune-count prefix table over its non-ASCII
// suffix: runeTable(line)[k] runes start in line[na : na+k]. Built once.
func (c colMapper) runeTable(line, na int) []int32 {
	if t := c.runes[line]; t != nil {
		return t
	}
	s := c.lines[line]
	t := make([]int32, len(s)-na+1)
	var n int32
	for k := 1; k < len(t); k++ {
		// A rune starts at every byte that is not a UTF-8 continuation byte.
		if b := s[na+k-1]; b < 0x80 || b >= 0xC0 {
			n++
		}
		t[k] = n
	}
	c.runes[line] = t
	return t
}

// byteCol is runeCol's inverse: it maps a rune column within line to a byte
// offset, clamping a column past the end to the line's byte length.
func (c colMapper) byteCol(line, runeOff int) int {
	if line < 0 || line >= len(c.lines) || runeOff <= 0 {
		return 0
	}
	s := c.lines[line]
	na := c.firstNonASCII(line)
	if runeOff <= na {
		return runeOff
	}
	n := na
	for i := na; i < len(s); i++ {
		if b := s[i]; b < 0x80 || b >= 0xC0 {
			if n == runeOff {
				return i
			}
			n++
		}
	}
	return len(s)
}

func (c colMapper) runeCol(line, byteOff int) int {
	if line < 0 || line >= len(c.lines) {
		return 0
	}
	s := c.lines[line]
	if byteOff > len(s) {
		byteOff = len(s)
	}
	na := c.firstNonASCII(line)
	if byteOff <= na {
		return byteOff
	}
	return na + int(c.runeTable(line, na)[byteOff-na])
}
