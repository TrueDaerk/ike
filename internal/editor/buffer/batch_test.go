package buffer

import (
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// applyEach is the per-edit reference ApplyAll must match.
func applyEach(b *Buffer, edits []Edit) []Edit {
	inv := make([]Edit, len(edits))
	for i, e := range edits {
		inv[i], _ = b.Apply(e)
	}
	return inv
}

func edit(sl, sc, el, ec int, text string) Edit {
	return Edit{Range: Range{Start: Position{sl, sc}, End: Position{el, ec}}, Text: text}
}

// checkApplyAll asserts ApplyAll yields the per-edit text and inverses, and
// that undoing the inverses restores the original byte-for-byte.
func checkApplyAll(t *testing.T, src string, edits []Edit) {
	t.Helper()
	want := FromString(src)
	wantInv := applyEach(want, edits)
	got := FromString(src)
	gotInv := got.ApplyAll(edits)
	if got.String() != want.String() {
		t.Fatalf("text = %q, want %q", got.String(), want.String())
	}
	if !reflect.DeepEqual(gotInv, wantInv) {
		t.Fatalf("inverses = %+v, want %+v", gotInv, wantInv)
	}
	for i := len(gotInv) - 1; i >= 0; i-- {
		got.Apply(gotInv[i])
	}
	if got.String() != FromString(src).String() {
		t.Fatalf("undo = %q, want %q", got.String(), src)
	}
}

func TestApplyAllMatchesPerEdit(t *testing.T) {
	src := "alpha beta\ngamma\n\ndelta épsilon 🌍\nzeta"
	cases := map[string][]Edit{
		"one line": {
			edit(0, 6, 0, 10, "BETA"),
			edit(0, 5, 0, 5, ","),
			edit(0, 0, 0, 1, "A"),
		},
		"spanning lines": {
			edit(3, 6, 4, 2, "X\nY\nZ"),
			edit(0, 3, 1, 2, ""),
		},
		"inserts": {
			edit(4, 0, 4, 0, "\t"),
			edit(2, 0, 2, 0, "new\n"),
			edit(0, 0, 0, 0, "// head\n"),
		},
		"deletes": {
			edit(3, 0, 4, 0, ""),
			edit(0, 5, 0, 10, ""),
			edit(0, 10, 1, 0, ""),
		},
		"replacements": {
			edit(3, 14, 3, 15, "🌕"),
			edit(3, 6, 3, 13, "ἔψιλον"),
			edit(1, 0, 1, 5, "GAMMA"),
		},
		"adjacent": {
			edit(0, 6, 0, 10, "b"),
			edit(0, 5, 0, 6, "_"),
			edit(0, 0, 0, 5, "a"),
			edit(0, 0, 0, 0, ">"),
		},
		"buffer end": {
			edit(4, 4, 4, 4, "\n"),
			edit(4, 0, 4, 4, "ZETA"),
		},
		"same position inserts": {
			edit(1, 2, 1, 2, "b"),
			edit(1, 2, 1, 2, "a"),
		},
		"unsorted falls back": {
			edit(0, 0, 0, 1, "A"),
			edit(1, 0, 1, 1, "G"),
		},
		"overlapping falls back": {
			edit(0, 3, 0, 8, "x"),
			edit(0, 0, 0, 5, "y"),
		},
		"out of range falls back": {
			edit(9, 0, 9, 3, "tail"),
			edit(0, 2, 0, 99, "!"),
			edit(-1, -1, 0, 1, "<"),
		},
		"reversed range falls back": {
			edit(1, 3, 1, 1, "r"),
			edit(0, 0, 0, 0, "s"),
		},
	}
	for name, edits := range cases {
		t.Run(name, func(t *testing.T) { checkApplyAll(t, src, edits) })
	}
}

func TestApplyAllRandomBatches(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	texts := []string{"", "x", "ü", "\n", "a\nb", "🌍\n", "\n\n"}
	onePass := 0
	for iter := 0; iter < 3000; iter++ {
		var lines []string
		for i := rng.Intn(5) + 1; i > 0; i-- {
			lines = append(lines, strings.Repeat("aé", rng.Intn(4)))
		}
		src := strings.Join(lines, "\n")
		b := FromString(src)
		var edits []Edit
		if iter%2 == 0 {
			// well-formed: sorted valid cut points paired into ranges, bottom-up
			var cuts []Position
			for i := rng.Intn(10) + 2; i > 0; i-- {
				l := rng.Intn(b.LineCount())
				cuts = append(cuts, Position{Line: l, Col: rng.Intn(b.RuneLen(l) + 1)})
			}
			sort.Slice(cuts, func(i, j int) bool { return cuts[i].Before(cuts[j]) })
			for i := 0; i+1 < len(cuts); i += 2 {
				s, e := cuts[i], cuts[i+1]
				if rng.Intn(3) == 0 {
					e = s
				}
				edits = append([]Edit{{Range: Range{Start: s, End: e}, Text: texts[rng.Intn(len(texts))]}}, edits...)
			}
		} else {
			for i := rng.Intn(6); i > 0; i-- {
				sl := rng.Intn(b.LineCount()+1) - rng.Intn(2)
				sc := rng.Intn(b.RuneLen(b.Clamp(Position{Line: sl}).Line)+2) - rng.Intn(2)
				el, ec := sl, sc
				if rng.Intn(2) == 0 {
					el = sl + rng.Intn(2)
					ec = rng.Intn(b.RuneLen(b.Clamp(Position{Line: el}).Line) + 2)
				}
				edits = append(edits, edit(sl, sc, el, ec, texts[rng.Intn(len(texts))]))
			}
		}
		if len(edits) > 1 {
			if _, ok := FromString(src).applyOnePass(edits); ok {
				onePass++
			}
		}
		checkApplyAll(t, src, edits)
	}
	if onePass < 1000 {
		t.Fatalf("only %d random batches took the one-pass path", onePass)
	}
}

// manyLinesBatch is 10,000 bottom-up edits spread across a 100k-line buffer,
// half of them changing the line count.
func manyLinesBatch() ([]string, []Edit) {
	lines := make([]string, 100_000)
	for i := range lines {
		lines[i] = "\tfield := value // comment"
	}
	var edits []Edit
	for l := 99_990; l >= 0; l -= 10 {
		text := " "
		if l%20 == 0 {
			text = "\n\t"
		}
		edits = append(edits, edit(l, 6, l, 7, text))
	}
	return lines, edits
}

// singleLineBatch is n bottom-up inserts spread evenly over one ~1 MB line.
func singleLineBatch(n int) ([]string, []Edit) {
	line := strings.Repeat("\"key\":\"value\",", 1<<20/14)
	stride := len(line) / (n + 1)
	edits := make([]Edit, n)
	for i := range edits {
		col := (n - i) * stride
		edits[i] = edit(0, col, 0, col, "\n  ")
	}
	return []string{line}, edits
}

func benchBatch(b *testing.B, lines []string, edits []Edit, apply func(*Buffer, []Edit) []Edit) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		buf := New(lines)
		b.StartTimer()
		apply(buf, edits)
	}
}

func BenchmarkApplyAllManyLines(b *testing.B) {
	lines, edits := manyLinesBatch()
	benchBatch(b, lines, edits, (*Buffer).ApplyAll)
}

// BenchmarkApplyEachManyLines is the per-edit reference for
// BenchmarkApplyAllManyLines.
func BenchmarkApplyEachManyLines(b *testing.B) {
	lines, edits := manyLinesBatch()
	benchBatch(b, lines, edits, applyEach)
}

func BenchmarkApplyAllSingleLine(b *testing.B) {
	lines, edits := singleLineBatch(40_000)
	benchBatch(b, lines, edits, (*Buffer).ApplyAll)
}

// BenchmarkApplyEachSingleLine is the per-edit reference for
// BenchmarkApplyAllSingleLine with 1% of its edits: the per-edit cost grows
// with the line length, so the full 40,000 would run for minutes.
func BenchmarkApplyEachSingleLine(b *testing.B) {
	lines, edits := singleLineBatch(400)
	benchBatch(b, lines, edits, applyEach)
}

func TestApplyAllOnePassUsed(t *testing.T) {
	b := FromString("abc\ndef")
	before := b.lines
	b.ApplyAll([]Edit{edit(1, 1, 1, 2, "E"), edit(0, 1, 0, 2, "B")})
	if &b.lines[0] == &before[0] {
		t.Fatal("a well-formed batch must install a fresh line slice in one pass")
	}
	if b.String() != "aBc\ndEf" {
		t.Fatalf("text = %q", b.String())
	}
}
