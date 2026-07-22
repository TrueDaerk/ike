package excmd

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want Command
	}{
		// Bare verbs and arguments (backward compatibility with :w :q :wq :e).
		{"w", Command{Name: "w"}},
		{"write", Command{Name: "write"}},
		{"w out.txt", Command{Name: "w", Args: "out.txt"}},
		{"q", Command{Name: "q"}},
		{"q!", Command{Name: "q", Bang: true}},
		{"wq", Command{Name: "wq"}},
		{"x", Command{Name: "x"}},
		{"wq file", Command{Name: "wq", Args: "file"}},
		{"e main.go", Command{Name: "e", Args: "main.go"}},
		{"  w  ", Command{Name: "w"}},
		{"", Command{}},

		// Bare ranges (line jumps): no name.
		{"42", Command{Range: Range{Start: Address{Kind: AddrLine, Line: 42}, Count: 1}}},
		{"$", Command{Range: Range{Start: Address{Kind: AddrLast}, Count: 1}}},
		{".", Command{Range: Range{Start: Address{Kind: AddrCurrent}, Count: 1}}},

		// Ranges with a command.
		{"1,5d", Command{Range: Range{Start: Address{Kind: AddrLine, Line: 1}, End: Address{Kind: AddrLine, Line: 5}, Count: 2}, Name: "d"}},
		{"%d", Command{Range: Range{Start: Address{Kind: AddrLine, Line: 1}, End: Address{Kind: AddrLast}, Count: 2}, Name: "d"}},
		{"%s/a/b/g", Command{Range: Range{Start: Address{Kind: AddrLine, Line: 1}, End: Address{Kind: AddrLast}, Count: 2}, Name: "s", Args: "/a/b/g"}},
		{"'<,'>s/a/b/", Command{Range: Range{Start: Address{Kind: AddrVisualStart}, End: Address{Kind: AddrVisualEnd}, Count: 2}, Name: "s", Args: "/a/b/"}},
		{".,$y", Command{Range: Range{Start: Address{Kind: AddrCurrent}, End: Address{Kind: AddrLast}, Count: 2}, Name: "y"}},

		// Offsets.
		{".+2", Command{Range: Range{Start: Address{Kind: AddrCurrent, Offset: 2}, Count: 1}}},
		{"$-1", Command{Range: Range{Start: Address{Kind: AddrLast, Offset: -1}, Count: 1}}},
		{"+3", Command{Range: Range{Start: Address{Kind: AddrCurrent, Offset: 3}, Count: 1}}},
		{".-2,.+2d", Command{Range: Range{Start: Address{Kind: AddrCurrent, Offset: -2}, End: Address{Kind: AddrCurrent, Offset: 2}, Count: 2}, Name: "d"}},

		// Pattern addresses, including alternate delimiter escaping.
		{"/foo/d", Command{Range: Range{Start: Address{Kind: AddrPatternNext, Pattern: "foo"}, Count: 1}, Name: "d"}},
		{"?bar?", Command{Range: Range{Start: Address{Kind: AddrPatternPrev, Pattern: "bar"}, Count: 1}}},
		{`/a\/b/d`, Command{Range: Range{Start: Address{Kind: AddrPatternNext, Pattern: "a/b"}, Count: 1}, Name: "d"}},
		{"/x/,/y/d", Command{Range: Range{Start: Address{Kind: AddrPatternNext, Pattern: "x"}, End: Address{Kind: AddrPatternNext, Pattern: "y"}, Count: 2}, Name: "d"}},

		// Non-letter command names (range companions, #164).
		{">", Command{Name: ">"}},
		{"1,3>", Command{Range: Range{Start: Address{Kind: AddrLine, Line: 1}, End: Address{Kind: AddrLine, Line: 3}, Count: 2}, Name: ">"}},

		// Reserved global commands parse (execution reports "not implemented").
		{"g/re/d", Command{Name: "g", Args: "/re/d"}},
		{"v/re/d", Command{Name: "v", Args: "/re/d"}},
	}
	for _, c := range cases {
		if got := Parse(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Parse(%q)\n got %+v\nwant %+v", c.in, got, c.want)
		}
	}
}

// fakeBuf is a minimal Buffer for resolver tests.
type fakeBuf int

func (b fakeBuf) LineCount() int { return int(b) }

func TestRangeResolve(t *testing.T) {
	// A 10-line buffer; cursor on line index 4; visual selection 2..6.
	rv := Resolver{
		Buf:         fakeBuf(10),
		Current:     4,
		VisualStart: 2,
		VisualEnd:   6,
		Search: func(pat string, from int, forward bool) (int, bool) {
			if pat == "miss" {
				return 0, false
			}
			if forward {
				return 8, true
			}
			return 1, true
		},
	}
	cases := []struct {
		name         string
		in           string
		wantS, wantE int
		wantErr      bool
		def          int
	}{
		{"empty falls back to def", "d", 4, 4, false, 4},
		{"whole file", "%d", 0, 9, false, 4},
		{"absolute", "2,5d", 1, 4, false, 4},
		{"absolute clamped", "5,999d", 4, 9, false, 4},
		{"current and last", ".,$d", 4, 9, false, 4},
		{"offset from last", "$-2d", 7, 7, false, 4},
		{"reversed swapped", "6,2d", 1, 5, false, 4},
		{"visual bounds", "'<,'>d", 2, 6, false, 4},
		{"pattern forward", "/x/d", 8, 8, false, 4},
		{"pattern backward", "?x?d", 1, 1, false, 4},
		{"pattern not found", "/miss/d", 0, 0, true, 4},
	}
	for _, c := range cases {
		cmd := Parse(c.in)
		s, e, err := cmd.Range.Resolve(rv, c.def)
		if (err != "") != c.wantErr {
			t.Errorf("%s: Resolve err=%q wantErr=%v", c.name, err, c.wantErr)
			continue
		}
		if c.wantErr {
			continue
		}
		if s != c.wantS || e != c.wantE {
			t.Errorf("%s: Resolve = (%d,%d) want (%d,%d)", c.name, s, e, c.wantS, c.wantE)
		}
	}
}

// TestResolveMissingSelection reports an error when '< / '> are unset.
func TestResolveMissingSelection(t *testing.T) {
	rv := Resolver{Buf: fakeBuf(5), Current: 0, VisualStart: -1, VisualEnd: -1}
	cmd := Parse("'<,'>d")
	if _, _, err := cmd.Range.Resolve(rv, 0); err == "" {
		t.Fatal("expected error for missing visual selection")
	}
}
