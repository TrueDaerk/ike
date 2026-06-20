package register

import "testing"

func TestYankFillsUnnamedAndZero(t *testing.T) {
	s := New()
	s.Yank(0, Entry{Text: "hello"})
	if s.Get(0).Text != "hello" {
		t.Fatalf("unnamed=%q", s.Get(0).Text)
	}
	if s.Get('0').Text != "hello" {
		t.Fatalf("yank reg 0=%q", s.Get('0').Text)
	}
}

func TestDeleteSmallFillsDash(t *testing.T) {
	s := New()
	s.Delete(0, Entry{Text: "x"})
	if s.Get('-').Text != "x" {
		t.Fatalf("small-delete=%q", s.Get('-').Text)
	}
	// yank register 0 is untouched by a delete.
	if s.Get('0').Text != "" {
		t.Fatalf("yank reg should be empty, got %q", s.Get('0').Text)
	}
}

func TestDeleteLinewiseShiftsNumbered(t *testing.T) {
	s := New()
	s.Delete(0, Entry{Text: "one\n", Linewise: true})
	s.Delete(0, Entry{Text: "two\n", Linewise: true})
	if s.Get('1').Text != "two\n" {
		t.Fatalf(`"1=%q want two`, s.Get('1').Text)
	}
	if s.Get('2').Text != "one\n" {
		t.Fatalf(`"2=%q want one`, s.Get('2').Text)
	}
}

func TestNamedRegisterAndUppercaseAppend(t *testing.T) {
	s := New()
	s.Yank('a', Entry{Text: "foo"})
	s.Yank('A', Entry{Text: "bar"})
	if s.Get('a').Text != "foobar" {
		t.Fatalf("append=%q want foobar", s.Get('a').Text)
	}
}

// fakeClip is an in-memory clipboard for the seam test.
type fakeClip struct{ buf string }

func (c *fakeClip) Read() (string, error) { return c.buf, nil }
func (c *fakeClip) Write(s string) error  { c.buf = s; return nil }

func TestClipboardSeam(t *testing.T) {
	s := New()
	clip := &fakeClip{}
	s.SetClipboard(clip)
	s.Yank('+', Entry{Text: "copied"})
	if clip.buf != "copied" {
		t.Fatalf("clipboard=%q", clip.buf)
	}
	if s.Get('+').Text != "copied" {
		t.Fatalf("read back=%q", s.Get('+').Text)
	}
}
