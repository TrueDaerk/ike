package linescan

import (
	"reflect"
	"testing"
)

func TestIsSpace(t *testing.T) {
	for _, r := range []rune{' ', '\t'} {
		if !IsSpace(r) {
			t.Errorf("IsSpace(%q) = false, want true", r)
		}
	}
	for _, r := range []rune{'a', '\n', '_'} {
		if IsSpace(r) {
			t.Errorf("IsSpace(%q) = true, want false", r)
		}
	}
}

func TestSkipSpace(t *testing.T) {
	cases := []struct {
		in   string
		from int
		want int
	}{
		{"", 0, 0},
		{"abc", 0, 0},
		{"  abc", 0, 2},
		{"\t\tabc", 0, 2},
		{"   ", 0, 3},
	}
	for _, c := range cases {
		if got := SkipSpace([]rune(c.in), c.from); got != c.want {
			t.Errorf("SkipSpace(%q, %d) = %d, want %d", c.in, c.from, got, c.want)
		}
	}
}

func TestWords(t *testing.T) {
	cases := []struct {
		in   string
		want [][2]int
	}{
		{"", nil},
		{"   ", nil},
		{"one", [][2]int{{0, 3}}},
		{"one two", [][2]int{{0, 3}, {4, 7}}},
		{"  one   two  ", [][2]int{{2, 5}, {8, 11}}},
	}
	for _, c := range cases {
		if got := Words([]rune(c.in), 0); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Words(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestCommentStart(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"mode: 0644", 10},
		{"mode: 0644 # a comment", 11},
		{`mode: "0644 # not a comment"`, 28},
		{"cron: '*/5 * * * *' # sched", 20},
	}
	for _, c := range cases {
		runes := []rune(c.in)
		if got := CommentStart(runes, 0); got != c.want {
			t.Errorf("CommentStart(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
