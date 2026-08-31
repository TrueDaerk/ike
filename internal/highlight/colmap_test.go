package highlight

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// naiveRuneCol is the pre-#2353 conversion: rescan the line prefix per call.
// It is the semantic reference the cached mapper must agree with.
func naiveRuneCol(s string, byteOff int) int {
	if byteOff >= len(s) {
		return len([]rune(s))
	}
	return len([]rune(s[:byteOff]))
}

// The cached mapper answers exactly like the per-call rescan, on ASCII lines,
// mixed lines and multi-byte-heavy lines, in any query order.
func TestRuneColMatchesNaive(t *testing.T) {
	lines := []string{
		"plain ascii only",
		"präfix äöü mitte ende",
		"日本語のテキスト",
		"",
		"a " + strings.Repeat("é", 5) + "z",
	}
	c := newColMapper(lines)
	for line, s := range lines {
		// Every rune boundary (Tree-sitter columns are never mid-rune), plus
		// past-the-end — and deliberately not in ascending order: the cache
		// must not depend on query order.
		offs := []int{len(s), 0, len(s) + 7}
		for i := range s {
			offs = append(offs, i)
		}
		for j, k := 3, len(offs)-1; j < k; j, k = j+1, k-1 {
			offs[j], offs[k] = offs[k], offs[j]
		}
		for _, off := range offs {
			if got, want := c.runeCol(line, off), naiveRuneCol(s, off); got != want {
				t.Errorf("runeCol(%d, %d) on %q = %d, want %d", line, off, s, got, want)
			}
		}
	}
	if got := c.runeCol(-1, 3); got != 0 {
		t.Errorf("runeCol out of range = %d, want 0", got)
	}
	if got := c.runeCol(len(lines), 3); got != 0 {
		t.Errorf("runeCol out of range = %d, want 0", got)
	}
}

// byteCol stays runeCol's inverse at every rune boundary.
func TestByteColInverse(t *testing.T) {
	lines := []string{"abc", "aä b日x"}
	c := newColMapper(lines)
	for line, s := range lines {
		runes := []rune(s)
		byteOff := 0
		for i, r := range runes {
			if got := c.byteCol(line, i); got != byteOff {
				t.Errorf("byteCol(%d, %d) = %d, want %d", line, i, got, byteOff)
			}
			if got := c.runeCol(line, byteOff); got != i {
				t.Errorf("runeCol(%d, %d) = %d, want %d", line, byteOff, got, i)
			}
			byteOff += len(string(r))
		}
		if got := c.byteCol(line, len(runes)+5); got != len(s) {
			t.Errorf("byteCol past end = %d, want %d", got, len(s))
		}
	}
}

// nonASCIILine builds a minified-JSON-shaped single line of roughly n bytes
// with non-ASCII content near the front, so the ASCII fast path never covers
// the queried offsets — the #2353 worst case.
func nonASCIILine(n int) string {
	var b strings.Builder
	b.Grow(n + 64)
	b.WriteString(`{"täxt":"ä "`)
	for b.Len() < n {
		b.WriteString(`,"kü":"überlange Zeile mit Ümläuten"`)
	}
	b.WriteString("}")
	return b.String()
}

// Converting one span's worth of columns must not rescan the line prefix: a
// full sweep over a multi-megabyte non-ASCII single line — tens of thousands
// of spans, like the Elasticsearch response of #2353 — finishes in well under
// a second when the conversion is linear, and took minutes when it was
// quadratic. The generous bound only fails on a quadratic regression.
func TestRuneColLongLineNotQuadratic(t *testing.T) {
	line := nonASCIILine(4 << 20)
	c := newColMapper([]string{line})
	start := time.Now()
	step := 64
	got := 0
	for off := 0; off < len(line); off += step {
		got += c.runeCol(0, off)
	}
	if got == 0 {
		t.Fatal("conversion produced no columns")
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("sweeping a %d-byte line took %v — the conversion is superlinear again", len(line), d)
	}
}

// BenchmarkRuneColLongLine measures a span-like sweep over one long non-ASCII
// line at doubling lengths. Linear behaviour shows as roughly constant ns/op
// per size doubling relative to the doubled work; the quadratic pre-#2353 code
// grows each step by ~4x.
func BenchmarkRuneColLongLine(b *testing.B) {
	for _, size := range []int{256 << 10, 512 << 10, 1 << 20, 2 << 20} {
		line := nonASCIILine(size)
		b.Run(fmt.Sprintf("%dKiB", size>>10), func(b *testing.B) {
			b.SetBytes(int64(size))
			for i := 0; i < b.N; i++ {
				c := newColMapper([]string{line})
				for off := 0; off < len(line); off += 64 {
					_ = c.runeCol(0, off)
					_ = c.runeCol(0, off+32)
				}
			}
		})
	}
}
