package cli

import (
	"reflect"
	"testing"
)

func TestParseGrammar(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want Invocation
	}{
		{"zero args", nil, Invocation{}},
		{"plain path", []string{"file.go"}, Invocation{Targets: []Target{{Path: "file.go"}}}},
		{"path with line", []string{"file.go:42"}, Invocation{Targets: []Target{{Path: "file.go", Line: 42}}}},
		{"path with line and col", []string{"file.go:42:7"}, Invocation{Targets: []Target{{Path: "file.go", Line: 42, Col: 7}}}},
		{"colon inside path", []string{"weird:name.txt"}, Invocation{Targets: []Target{{Path: "weird:name.txt"}}}},
		{"colon path with line", []string{"a:b.txt:12"}, Invocation{Targets: []Target{{Path: "a:b.txt", Line: 12}}}},
		{"three numeric segments keep leftmost in path", []string{"a:12:5:7"}, Invocation{Targets: []Target{{Path: "a:12", Line: 5, Col: 7}}}},
		{"trailing colon is a plain path", []string{"file.go:"}, Invocation{Targets: []Target{{Path: "file.go:"}}}},
		{"trailing colon after number", []string{"file.go:42:"}, Invocation{Targets: []Target{{Path: "file.go:42:"}}}},
		{"zero line stays in path", []string{"file.go:0"}, Invocation{Targets: []Target{{Path: "file.go:0"}}}},
		{"signed suffix stays in path", []string{"file.go:+5"}, Invocation{Targets: []Target{{Path: "file.go:+5"}}}},
		{"leading colon stays in path", []string{":42"}, Invocation{Targets: []Target{{Path: ":42"}}}},
		{"vim-style +N", []string{"+42", "file.go"}, Invocation{Targets: []Target{{Path: "file.go", Line: 42}}}},
		{"suffix wins over +N", []string{"+42", "file.go:7"}, Invocation{Targets: []Target{{Path: "file.go", Line: 7}}}},
		{"+N skips only the next path", []string{"+42", "a.go", "b.go"}, Invocation{Targets: []Target{{Path: "a.go", Line: 42}, {Path: "b.go"}}}},
		{"stdin only", []string{"-"}, Invocation{Stdin: true}},
		{"stdin with files keeps order", []string{"a.go", "-", "b.go:3"}, Invocation{Stdin: true, Targets: []Target{{Path: "a.go"}, {Path: "b.go", Line: 3}}}},
		{"multiple targets preserve order", []string{"b.go", "a.go:1"}, Invocation{Targets: []Target{{Path: "b.go"}, {Path: "a.go", Line: 1}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Parse(c.args)
			if err != nil {
				t.Fatalf("Parse(%v) error: %v", c.args, err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Parse(%v) = %+v, want %+v", c.args, got, c.want)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"dangling +N", []string{"file.go", "+42"}},
		{"bare +", []string{"+"}},
		{"non-numeric +x", []string{"+abc", "file.go"}},
		{"double sign", []string{"++42", "file.go"}},
		{"zero line flag", []string{"+0", "file.go"}},
		{"duplicate stdin", []string{"-", "-"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Parse(c.args); err == nil {
				t.Fatalf("Parse(%v) must error", c.args)
			}
		})
	}
}
