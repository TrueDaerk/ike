package editor

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"ike/internal/editor/buffer"
)

// perEditResult applies edits the way applyTextEdits did before the one-pass
// apply: sorted bottom-up, one buffer.Apply per edit.
func perEditResult(orig string, edits []TextEdit) string {
	sorted := append([]TextEdit(nil), edits...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].StartLine != sorted[j].StartLine {
			return sorted[i].StartLine > sorted[j].StartLine
		}
		return sorted[i].StartCol > sorted[j].StartCol
	})
	b := buffer.FromString(orig)
	for _, e := range sorted {
		b.Apply(buffer.Edit{
			Range: buffer.Range{
				Start: buffer.Position{Line: e.StartLine, Col: e.StartCol},
				End:   buffer.Position{Line: e.EndLine, Col: e.EndCol},
			},
			Text: e.Text,
		})
	}
	return b.String()
}

func TestApplyTextEditsMatchesPerEditAndUndoes(t *testing.T) {
	orig := "{\"a\":1,\"b\":[2,3]}\nsecond line\nthird"
	cases := map[string][]TextEdit{
		"edits in one line": {
			{StartLine: 0, StartCol: 1, EndLine: 0, EndCol: 1, Text: "\n  "},
			{StartLine: 0, StartCol: 7, EndLine: 0, EndCol: 7, Text: "\n  "},
			{StartLine: 0, StartCol: 16, EndLine: 0, EndCol: 16, Text: "\n"},
		},
		"edits spanning lines": {
			{StartLine: 0, StartCol: 16, EndLine: 1, EndCol: 6, Text: "};"},
			{StartLine: 1, StartCol: 7, EndLine: 2, EndCol: 0, Text: "LINE "},
		},
		"inserts deletes replacements": {
			{StartLine: 2, StartCol: 0, EndLine: 2, EndCol: 0, Text: "> "},
			{StartLine: 1, StartCol: 0, EndLine: 1, EndCol: 7, Text: ""},
			{StartLine: 0, StartCol: 2, EndLine: 0, EndCol: 3, Text: "A"},
		},
		"adjacent": {
			{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 1, Text: "["},
			{StartLine: 0, StartCol: 1, EndLine: 0, EndCol: 4, Text: "'a'"},
			{StartLine: 0, StartCol: 4, EndLine: 0, EndCol: 5, Text: "="},
		},
		"buffer end": {
			{StartLine: 2, StartCol: 5, EndLine: 2, EndCol: 5, Text: "\n"},
			{StartLine: 2, StartCol: 0, EndLine: 2, EndCol: 5, Text: "THIRD"},
		},
		"unsorted": {
			{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 1, Text: "("},
			{StartLine: 2, StartCol: 0, EndLine: 2, EndCol: 1, Text: "T"},
			{StartLine: 1, StartCol: 0, EndLine: 1, EndCol: 1, Text: "S"},
		},
		"malformed clamped": {
			{StartLine: 1, StartCol: 3, EndLine: 1, EndCol: 99, Text: "!"},
			{StartLine: 5, StartCol: 0, EndLine: 9, EndCol: 0, Text: "tail"},
		},
	}
	for name, edits := range cases {
		t.Run(name, func(t *testing.T) {
			m, _ := loaded(t, orig)
			if n := m.ApplyTextEdits(edits); n != len(edits) {
				t.Fatalf("applied = %d, want %d", n, len(edits))
			}
			if want := perEditResult(orig, edits); m.Text() != want {
				t.Fatalf("text = %q, want %q", m.Text(), want)
			}
			m = send(m, key('u'))
			if m.Text() != orig {
				t.Fatalf("one undo must restore the original, got %q", m.Text())
			}
		})
	}
}

func TestApplyTextEditsAmendMergesBatch(t *testing.T) {
	orig := "b\na"
	m, _ := loaded(t, orig)
	m.ApplyTextEdits([]TextEdit{
		{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 1, Text: "a"},
		{StartLine: 1, StartCol: 0, EndLine: 1, EndCol: 1, Text: "b"},
	})
	m.ApplyTextEditsAmend([]TextEdit{
		{StartLine: 0, StartCol: 1, EndLine: 0, EndCol: 1, Text: ";"},
		{StartLine: 1, StartCol: 1, EndLine: 1, EndCol: 1, Text: ";"},
	})
	if m.Text() != "a;\nb;" {
		t.Fatalf("text = %q", m.Text())
	}
	m = send(m, key('u'))
	if m.Text() != orig {
		t.Fatalf("one undo must revert both batches, got %q", m.Text())
	}
}

// singleLineJSON is a minified JSON document of at least size bytes on one
// line, plus the formatter-style edits breaking it after every comma.
func singleLineJSON(size, maxEdits int) (string, []TextEdit) {
	var sb strings.Builder
	var edits []TextEdit
	sb.WriteByte('{')
	for i := 0; sb.Len() < size; i++ {
		if i > 0 {
			sb.WriteByte(',')
			if len(edits) < maxEdits {
				edits = append(edits, TextEdit{StartLine: 0, StartCol: sb.Len(), EndLine: 0, EndCol: sb.Len(), Text: "\n  "})
			}
		}
		fmt.Fprintf(&sb, "\"key%06d\":\"value\"", i)
	}
	sb.WriteByte('}')
	return sb.String(), edits
}

func TestApplyTextEditsSingleLineJSON(t *testing.T) {
	src, edits := singleLineJSON(64*1024, 1<<30)
	m, _ := loaded(t, src)
	m.ApplyTextEdits(edits)
	if want := perEditResult(src, edits); m.Text() != want {
		t.Fatal("one-pass result differs from the per-edit result")
	}
	if m.buf.LineCount() != len(edits)+1 {
		t.Fatalf("line count = %d, want %d", m.buf.LineCount(), len(edits)+1)
	}
	m = send(m, key('u'))
	if m.Text() != src {
		t.Fatal("undo must restore the minified document byte-for-byte")
	}
}

// BenchmarkApplyTextEditsSingleLine applies 40,000 edits inside one line of a
// 1 MB buffer — the minified-JSON format result that used to cost minutes.
func BenchmarkApplyTextEditsSingleLine(b *testing.B) {
	src, edits := singleLineJSON(1<<20, 40_000)
	if len(edits) < 40_000 {
		b.Fatalf("fixture has only %d edits", len(edits))
	}
	benchApplyTextEdits(b, src, edits)
}

// BenchmarkApplyTextEditsManyLines applies 10,000 edits spread across a
// 100k-line buffer, half of them changing the line count.
func BenchmarkApplyTextEditsManyLines(b *testing.B) {
	src, edits := manyLinesFixture()
	benchApplyTextEdits(b, src, edits)
}

func manyLinesFixture() (string, []TextEdit) {
	src := strings.Repeat("\tfield := value // comment\n", 100_000)
	var edits []TextEdit
	for l := 0; l < 100_000; l += 10 {
		text := " "
		if l%20 == 0 {
			text = "\n\t"
		}
		edits = append(edits, TextEdit{StartLine: l, StartCol: 6, EndLine: l, EndCol: 7, Text: text})
	}
	return src, edits
}

func benchApplyTextEdits(b *testing.B, src string, edits []TextEdit) {
	m := benchEditor(b, src)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		m.RestoreText(src)
		b.StartTimer()
		m.ApplyTextEdits(edits)
	}
}
