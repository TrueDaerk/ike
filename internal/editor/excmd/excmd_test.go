package excmd

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want Command
	}{
		{"w", Command{Kind: Write}},
		{"write", Command{Kind: Write}},
		{"w out.txt", Command{Kind: Write, Arg: "out.txt"}},
		{"q", Command{Kind: Quit}},
		{"q!", Command{Kind: Quit, Force: true}},
		{"wq", Command{Kind: WriteQuit}},
		{"x", Command{Kind: WriteQuit}},
		{"wq file", Command{Kind: WriteQuit, Arg: "file"}},
		{"e main.go", Command{Kind: Edit, Arg: "main.go"}},
		{"42", Command{Kind: Goto, Line: 42}},
		{"  w  ", Command{Kind: Write}},
		{"nonsense", Command{Kind: Unknown}},
		{"", Command{Kind: Unknown}},
	}
	for _, c := range cases {
		if got := Parse(c.in); got != c.want {
			t.Errorf("Parse(%q)=%+v want %+v", c.in, got, c.want)
		}
	}
}
