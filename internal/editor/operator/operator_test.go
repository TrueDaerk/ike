package operator

import (
	"testing"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
	"ike/internal/editor/motion"
	"ike/internal/editor/register"
)

func setup(content string) (*buffer.Buffer, *register.Store, *history.History) {
	return buffer.FromString(content), register.New(), history.New()
}

func rec(b *buffer.Buffer, at buffer.Position) *history.Recorder {
	return history.NewRecorder(b, at)
}

func TestDeleteCharwise(t *testing.T) {
	b, st, _ := setup("hello world")
	// d w from col 0 -> exclusive to col 6 ("hello ").
	target := motion.WordForward(b, buffer.Position{Line: 0, Col: 0}, 1)
	tg := Compose(b, buffer.Position{Line: 0, Col: 0}, target.Pos, target.Kind)
	r := rec(b, buffer.Position{Line: 0, Col: 0})
	Delete(b, r, st, 0, tg)
	if b.Line(0) != "world" {
		t.Fatalf("dw=%q want world", b.Line(0))
	}
	if st.Get(0).Text != "hello " {
		t.Fatalf("register=%q want 'hello '", st.Get(0).Text)
	}
}

func TestDeleteInclusiveMotion(t *testing.T) {
	b, st, _ := setup("hello")
	// d $ inclusive -> whole line content.
	target := motion.LineEnd(b, buffer.Position{Line: 0, Col: 0}, 1)
	tg := Compose(b, buffer.Position{Line: 0, Col: 0}, target.Pos, target.Kind)
	Delete(b, rec(b, buffer.Position{Line: 0, Col: 0}), st, 0, tg)
	if b.Line(0) != "" {
		t.Fatalf("d$=%q want empty", b.Line(0))
	}
}

func TestDeleteLinewise(t *testing.T) {
	b, st, _ := setup("one\ntwo\nthree")
	tg := LineTarget(0, 1) // dd over first two lines
	Delete(b, rec(b, buffer.Position{Line: 0, Col: 0}), st, 0, tg)
	if b.LineCount() != 1 || b.Line(0) != "three" {
		t.Fatalf("2dd=%q", b.Lines())
	}
	if st.Get(0).Text != "one\ntwo\n" || !st.Get(0).Linewise {
		t.Fatalf("linewise reg=%q linewise=%v", st.Get(0).Text, st.Get(0).Linewise)
	}
}

func TestDeleteLastLine(t *testing.T) {
	b, st, _ := setup("one\ntwo")
	Delete(b, rec(b, buffer.Position{Line: 1, Col: 0}), st, 0, LineTarget(1, 1))
	if b.LineCount() != 1 || b.Line(0) != "one" {
		t.Fatalf("delete last line=%q", b.Lines())
	}
}

func TestYankDoesNotMutate(t *testing.T) {
	b, st, _ := setup("abc")
	Yank(b, st, 0, CharTarget(buffer.NewRange(buffer.Position{Line: 0, Col: 0}, buffer.Position{Line: 0, Col: 2})))
	if b.Line(0) != "abc" {
		t.Fatalf("yank mutated buffer: %q", b.Line(0))
	}
	if st.Get(0).Text != "ab" {
		t.Fatalf("yank reg=%q want ab", st.Get(0).Text)
	}
}

func TestChangeLinewiseKeepsLine(t *testing.T) {
	b, st, _ := setup("  foo\nbar")
	at := Change(b, rec(b, buffer.Position{Line: 0, Col: 0}), st, 0, LineTarget(0, 0))
	if b.LineCount() != 2 || b.Line(0) != "  " {
		t.Fatalf("cc kept lines=%q", b.Lines())
	}
	if at != (buffer.Position{Line: 0, Col: 2}) {
		t.Fatalf("cc cursor=%v want {0 2} (after indent)", at)
	}
}

func TestPasteCharAfter(t *testing.T) {
	b, _, _ := setup("ac")
	e := register.Entry{Text: "b"}
	cur := Paste(b, rec(b, buffer.Position{Line: 0, Col: 0}), e, buffer.Position{Line: 0, Col: 0}, true, 1, false)
	if b.Line(0) != "abc" {
		t.Fatalf("p=%q want abc", b.Line(0))
	}
	if cur != (buffer.Position{Line: 0, Col: 1}) {
		t.Fatalf("paste cursor=%v want {0 1}", cur)
	}
}

func TestPasteCharCount(t *testing.T) {
	b, _, _ := setup("X")
	e := register.Entry{Text: "ab"}
	Paste(b, rec(b, buffer.Position{Line: 0, Col: 0}), e, buffer.Position{Line: 0, Col: 0}, true, 3, false)
	if b.Line(0) != "Xababab" {
		t.Fatalf("3p=%q", b.Line(0))
	}
}

func TestPasteLinewiseAfter(t *testing.T) {
	b, _, _ := setup("one\ntwo")
	e := register.Entry{Text: "NEW\n", Linewise: true}
	cur := Paste(b, rec(b, buffer.Position{Line: 0, Col: 0}), e, buffer.Position{Line: 0, Col: 0}, true, 1, false)
	if b.LineCount() != 3 || b.Line(1) != "NEW" {
		t.Fatalf("linewise p=%q", b.Lines())
	}
	if cur.Line != 1 {
		t.Fatalf("paste line cursor line=%d want 1", cur.Line)
	}
}

func TestPasteLinewiseBefore(t *testing.T) {
	b, _, _ := setup("one\ntwo")
	e := register.Entry{Text: "NEW\n", Linewise: true}
	Paste(b, rec(b, buffer.Position{Line: 1, Col: 0}), e, buffer.Position{Line: 1, Col: 0}, false, 1, false)
	if b.Line(1) != "NEW" || b.Line(2) != "two" {
		t.Fatalf("linewise P=%q", b.Lines())
	}
}

func TestDeleteThenUndoViaHistory(t *testing.T) {
	b, st, h := setup("hello world")
	r := rec(b, buffer.Position{Line: 0, Col: 0})
	Delete(b, r, st, 0, CharTarget(buffer.NewRange(buffer.Position{Line: 0, Col: 0}, buffer.Position{Line: 0, Col: 6})))
	h.Push(r.Commit(buffer.Position{Line: 0, Col: 0}))
	if b.Line(0) != "world" {
		t.Fatalf("after delete=%q", b.Line(0))
	}
	h.Undo(b)
	if b.Line(0) != "hello world" {
		t.Fatalf("after undo=%q", b.Line(0))
	}
}
