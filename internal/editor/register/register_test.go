package register

import (
	"errors"
	"testing"
)

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

// errClip fails every operation — the broken-bridge case behind #1255.
type errClip struct{ err error }

func (c errClip) Read() (string, error) { return "", c.err }
func (c errClip) Write(string) error    { return c.err }

// TestClipboardErrorRecorded guards #1255 at the seam: a failed write is
// recorded for the editor to report, not discarded, and taking it clears it.
func TestClipboardErrorRecorded(t *testing.T) {
	want := errors.New("pbcopy missing")
	s := New()
	s.SetClipboard(errClip{err: want})

	s.Yank('+', Entry{Text: "copied"})
	if got := s.TakeClipboardError(); !errors.Is(got, want) {
		t.Fatalf("yank error = %v, want %v", got, want)
	}
	if got := s.TakeClipboardError(); got != nil {
		t.Fatalf("error = %v after taking it, want nil", got)
	}

	// Deletes into "+ and reads through it record too.
	s.Delete('+', Entry{Text: "cut"})
	if got := s.TakeClipboardError(); !errors.Is(got, want) {
		t.Fatalf("delete error = %v, want %v", got, want)
	}
	s.Get('+')
	if got := s.TakeClipboardError(); !errors.Is(got, want) {
		t.Fatalf("read error = %v, want %v", got, want)
	}
}

// TestClipboardReadFallsBackOnError: a failing read still yields the unnamed
// register, so a paste degrades instead of dying (#1255).
func TestClipboardReadFallsBackOnError(t *testing.T) {
	s := New()
	s.SetClipboard(errClip{err: errors.New("pbpaste died")})
	s.Yank(0, Entry{Text: "internal"})
	if got := s.Get('+').Text; got != "internal" {
		t.Fatalf(`Get('+') = %q, want the unnamed fallback`, got)
	}
}

// TestClipboardSyncScope guards #1256's boundaries at the store: unnamed
// yanks mirror, named yanks and deletes never do, and the sync is off by
// default so the package stays inert standalone.
func TestClipboardSyncScope(t *testing.T) {
	s := New()
	clip := &fakeClip{}
	s.SetClipboard(clip)

	s.Yank(0, Entry{Text: "no sync yet"})
	if clip.buf != "" {
		t.Fatalf("clipboard = %q, want the sync off by default", clip.buf)
	}

	s.SetClipboardSync(true)
	if !s.ClipboardSync() {
		t.Fatal("ClipboardSync() = false after enabling it")
	}
	s.Yank(0, Entry{Text: "yanked"})
	if clip.buf != "yanked" {
		t.Fatalf("clipboard = %q, want the unnamed yank mirrored", clip.buf)
	}

	s.Yank('a', Entry{Text: "named"})
	if clip.buf != "yanked" {
		t.Fatalf("clipboard = %q, want a named yank to leave it alone", clip.buf)
	}

	s.Delete(0, Entry{Text: "deleted"})
	if clip.buf != "yanked" {
		t.Fatalf("clipboard = %q, want a delete to leave it alone", clip.buf)
	}
}
